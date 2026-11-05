package note

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// maxMembers caps the people a note is shared with, the owner not counted.
const maxMembers = 50

var (
	errTooMany  = errors.New("too many members")
	errNotOnWS  = errors.New("not a member of the workspace")
	errNotFound = errors.New("note not found")
)

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Member is someone the note is shared with; Role is "edit" or "view".
type Member struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Note is a note as one member sees it: Pinned, Archived and Labels are theirs.
type Note struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Body      json.RawMessage `json:"body"`
	Color     string          `json:"color"`
	Owner     Person          `json:"owner"`
	Members   []Member        `json:"members"` // everyone but the owner, by name
	Pinned    bool            `json:"pinned"`
	Archived  bool            `json:"archived"`
	Labels    []string        `json:"labels"`
	CanEdit   bool            `json:"can_edit"` // the owner or an edit member: may change the title, body and color
	CanDelete bool            `json:"can_delete"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	UpdatedBy *Person         `json:"updated_by"`
	bodyGen   int             // counts REST body writes, see collab.Doc
}

// noteQuery selects the notes the user ($1) has in the workspace ($2), all or the one with the id ($3): pinned
// first, then the latest changed.
// ponytail: no paging, every note the caller has; page it if anyone has more than a few hundred.
const noteQuery = `
	select n.id::text, n.title, n.body, n.color, o.id::text, o.name,
	  coalesce((select json_agg(json_build_object('id', u.id, 'name', u.name, 'role', x.role) order by lower(u.name), u.id)
	            from note_members x join users u on u.id = x.user_id where x.note_id = n.id and x.user_id <> n.owner_id), '[]'),
	  me.pinned, me.archived, me.labels, n.created_at, n.updated_at,
	  (select json_build_object('id', u.id, 'name', u.name) from users u where u.id = n.updated_by),
	  me.role = 'edit' or n.owner_id = $1, n.body_generation
	from notes n
	join note_members me on me.note_id = n.id and me.user_id = $1
	join users o on o.id = n.owner_id
	where n.workspace_id = $2 and ($3::uuid is null or n.id = $3)
	order by me.pinned desc, n.updated_at desc, n.id`

// model owns every query for notes.
type model struct{ db *pgxpool.Pool }

func (m *model) query(ctx context.Context, userID, wsID string, noteID *string) ([]Note, error) {
	rows, err := m.db.Query(ctx, noteQuery, userID, wsID, noteID)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	ns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Note, error) {
		var n Note
		err := row.Scan(&n.ID, &n.Title, &n.Body, &n.Color, &n.Owner.ID, &n.Owner.Name, &n.Members, &n.Pinned, &n.Archived,
			&n.Labels, &n.CreatedAt, &n.UpdatedAt, &n.UpdatedBy, &n.CanEdit, &n.bodyGen)
		n.CanDelete = n.Owner.ID == userID
		n.CreatedAt, n.UpdatedAt = n.CreatedAt.UTC(), n.UpdatedAt.UTC()
		return n, err
	})
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	return ns, nil
}

// list returns every note the user has in the workspace, archived too.
func (m *model) list(ctx context.Context, userID, wsID string) ([]Note, error) {
	return m.query(ctx, userID, wsID, nil)
}

// get returns the note as the user sees it; nil when they don't have it.
func (m *model) get(ctx context.Context, userID, wsID, noteID string) (*Note, error) {
	ns, err := m.query(ctx, userID, wsID, &noteID)
	if err != nil || len(ns) == 0 {
		return nil, err
	}
	return &ns[0], nil
}

// create adds the user's note, with them as its first member.
func (m *model) create(ctx context.Context, ws *workspace.Workspace, userID string, f fields) (*Note, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`insert into notes (workspace_id, owner_id, title, body, color, updated_by) values ($1, $2, coalesce($3, ''), $4, coalesce($5, ''), $2)
			 returning id::text`, ws.ID, userID, f.title, body(f), f.color).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into note_members (note_id, user_id) values ($1, $2)`, id, userID); err != nil {
			return err
		}
		if err := setMine(ctx, tx, id, userID, f); err != nil {
			return err
		}
		return publish(ctx, tx, ws, userID, []string{userID})
	})
	if err != nil {
		return nil, fmt.Errorf("create note: %w", err)
	}
	return m.get(ctx, userID, ws.ID, id)
}

// update changes the note's present fields: the shared ones for everyone (stamping updated_at and updated_by), the
// rest for the user alone. A body replaces the live document (see collab.Store.Reset).
// ponytail: last write wins on title and color; two people editing at once lose one change.
func (m *model) update(ctx context.Context, ws *workspace.Workspace, userID, noteID string, f fields) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if f.body != nil {
			if err := docs.Reset(ctx, tx, noteID); err != nil {
				return err
			}
		}
		if f.shared() {
			if _, err := tx.Exec(ctx,
				`update notes set title = coalesce($2, title), body = case when $3 then $4 else body end, color = coalesce($5, color),
				   body_generation = body_generation + case when $3 then 1 else 0 end, updated_at = now(), updated_by = $6
				 where id = $1`, noteID, f.title, f.body != nil, body(f), f.color, userID); err != nil {
				return err
			}
		}
		if err := setMine(ctx, tx, noteID, userID, f); err != nil {
			return err
		}
		ids, err := memberIDs(ctx, tx, noteID)
		if err != nil {
			return err
		}
		return publish(ctx, tx, ws, userID, ids)
	})
	if err != nil {
		return fmt.Errorf("update note: %w", err)
	}
	return nil
}

// setMine sets the user's own pinned, archived and labels where present; archiving unpins.
func setMine(ctx context.Context, tx pgx.Tx, noteID, userID string, f fields) error {
	if f.pinned == nil && f.archived == nil && f.labels == nil {
		return nil
	}
	_, err := tx.Exec(ctx,
		`update note_members set archived = coalesce($3, archived), pinned = case when $3 then false else coalesce($4, pinned) end,
		   labels = coalesce($5, labels)
		 where note_id = $1 and user_id = $2`, noteID, userID, f.archived, f.pinned, f.labels)
	return err
}

// delete deletes the note for everyone when the user owns it, else takes it off their notes.
func (m *model) delete(ctx context.Context, ws *workspace.Workspace, userID string, n *Note) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		ids, err := memberIDs(ctx, tx, n.ID)
		if err != nil {
			return err
		}
		if n.Owner.ID == userID {
			_, err = tx.Exec(ctx, `delete from notes where id = $1`, n.ID)
		} else {
			_, err = tx.Exec(ctx, `delete from note_members where note_id = $1 and user_id = $2`, n.ID, userID)
		}
		if err != nil {
			return err
		}
		return publish(ctx, tx, ws, userID, ids)
	})
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	return nil
}

// addMember shares the note with a non-guest member of its workspace who doesn't have it, with the role. Returns
// errNotOnWS when they aren't one (or have it already), errTooMany when the note has maxMembers, errNotFound when
// it's gone.
func (m *model) addMember(ctx context.Context, ws *workspace.Workspace, actorID, noteID, userID, role string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		// Locking the note makes concurrent adds count in turn.
		var owner string
		if err := tx.QueryRow(ctx, `select owner_id::text from notes where id = $1 for update`, noteID).Scan(&owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errNotFound
			}
			return err
		}
		ids, err := memberIDs(ctx, tx, noteID)
		if err != nil {
			return err
		}
		if slices.Contains(ids, userID) {
			return errNotOnWS
		}
		if len(ids)-1 >= maxMembers {
			return errTooMany
		}
		var ok bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from workspace_members where workspace_id = $1 and user_id = $2) and not is_guest($1, $2)`,
			ws.ID, userID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errNotOnWS
		}
		if _, err := tx.Exec(ctx, `insert into note_members (note_id, user_id, role) values ($1, $2, $3)`, noteID, userID, role); err != nil {
			return err
		}
		return publish(ctx, tx, ws, actorID, append(ids, userID))
	})
	if errors.Is(err, errNotOnWS) || errors.Is(err, errTooMany) || errors.Is(err, errNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("add note member: %w", err)
	}
	return nil
}

// removeMember takes the user off the note; they also get the change, so it leaves their page.
func (m *model) removeMember(ctx context.Context, ws *workspace.Workspace, actorID, noteID, userID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		ids, err := memberIDs(ctx, tx, noteID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from note_members where note_id = $1 and user_id = $2`, noteID, userID); err != nil {
			return err
		}
		if err := docs.Rejoin(ctx, tx, noteID); err != nil {
			return err
		}
		return publish(ctx, tx, ws, actorID, ids)
	})
	if err != nil {
		return fmt.Errorf("remove note member: %w", err)
	}
	return nil
}

// setRole changes a member's role; errNotFound when they aren't a member. Live sessions rejoin under the new role.
func (m *model) setRole(ctx context.Context, ws *workspace.Workspace, actorID, noteID, userID, role string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update note_members set role = $3 where note_id = $1 and user_id = $2`, noteID, userID, role)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound
		}
		if err := docs.Rejoin(ctx, tx, noteID); err != nil {
			return err
		}
		ids, err := memberIDs(ctx, tx, noteID)
		if err != nil {
			return err
		}
		return publish(ctx, tx, ws, actorID, ids)
	})
	if errors.Is(err, errNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("set note role: %w", err)
	}
	return nil
}

func memberIDs(ctx context.Context, tx pgx.Tx, noteID string) ([]string, error) {
	rows, err := tx.Query(ctx, `select user_id::text from note_members where note_id = $1`, noteID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// publish tells each of the users (the note's members before and after the change) their notes changed.
func publish(ctx context.Context, tx pgx.Tx, ws *workspace.Workspace, actorID string, userIDs []string) error {
	for _, id := range userIDs {
		if err := realtime.Publish(ctx, tx, realtime.Event{Kind: "notes.changed", Workspace: ws.Slug, Actor: actorID,
			Audience: realtime.ToUser(id), Paths: []string{realtime.Notes(ws.Slug)}}); err != nil {
			return err
		}
	}
	return nil
}

// body is the body to store: nil (SQL null) when absent or empty.
func body(f fields) any {
	if f.body == nil || *f.body == nil {
		return nil
	}
	return []byte(*f.body)
}
