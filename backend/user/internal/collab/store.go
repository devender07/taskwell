package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Store names where a kind of document keeps its live state. The names come from code, never from input.
type Store struct {
	Updates string // the stored Yjs updates: id, <Key>, data
	Key     string // the updates' column holding the document's id
	Docs    string // the documents, with a seed_claimed_at column
	Channel string // the Postgres channel instances relay frames on
}

type docUpdate struct {
	id   int64
	data []byte
}

// Reset drops the document's live state in tx, after a REST write replaced it: every instance resets its editors so
// they reload it, and the next one seeds it from the new document (a recent seed's claim would hold that off for 10s).
func (s Store) Reset(ctx context.Context, tx pgx.Tx, docID string) error {
	if _, err := tx.Exec(ctx, `delete from `+s.Updates+` where `+s.Key+` = $1`, docID); err != nil {
		return fmt.Errorf("reset live document: %w", err)
	}
	return s.Rejoin(ctx, tx, docID)
}

// Rejoin tells every instance, once tx commits, to reset the document's editors so they join again, keeping its live
// state: after someone's access to it changed.
// ponytail: every editor reloads, not just the one whose access changed; target the user if that gets disruptive.
func (s Store) Rejoin(ctx context.Context, tx pgx.Tx, docID string) error {
	// The reset sessions' frames are ignored, so a pending seed never lands: free the claim for the next editor.
	if _, err := tx.Exec(ctx, `update `+s.Docs+` set seed_claimed_at = null where id = $1`, docID); err != nil {
		return fmt.Errorf("reset live document: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_notify($1, $2)`, s.Channel, notePrefix(docID, noteReset)); err != nil {
		return fmt.Errorf("reset live document: %w", err)
	}
	return nil
}

// docUpdates returns the document's stored Yjs updates, oldest first.
func (h *Hub) docUpdates(ctx context.Context, docID string) ([]docUpdate, error) {
	rows, err := h.db.Query(ctx, `select id, data from `+h.store.Updates+` where `+h.store.Key+` = $1 order by id`, docID)
	if err != nil {
		return nil, fmt.Errorf("list doc updates: %w", err)
	}
	us, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (docUpdate, error) {
		var u docUpdate
		err := row.Scan(&u.id, &u.data)
		return u, err
	})
	if err != nil {
		return nil, fmt.Errorf("list doc updates: %w", err)
	}
	return us, nil
}

// addDocUpdate stores an update and returns its id; ok is false when the document's stored updates would pass
// maxStored.
func (h *Hub) addDocUpdate(ctx context.Context, docID string, data []byte) (id int64, ok bool, err error) {
	t, k := h.store.Updates, h.store.Key
	err = h.db.QueryRow(ctx,
		`insert into `+t+` (`+k+`, data)
		 select $1, $2 where (select coalesce(sum(octet_length(data)), 0) from `+t+` where `+k+` = $1)
		                     + octet_length($2::bytea) <= $3
		 returning id`, docID, data, maxStored).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("add doc update: %w", err)
	}
	return id, true, nil
}

// docUpdate returns one stored update's data; nil when it's gone (compacted).
func (h *Hub) docUpdate(ctx context.Context, id int64) ([]byte, error) {
	var data []byte
	err := h.db.QueryRow(ctx, `select data from `+h.store.Updates+` where id = $1`, id).Scan(&data)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load doc update: %w", err)
	}
	return data, nil
}

// notify sends payload to every API instance listening on the store's channel.
func (h *Hub) notify(ctx context.Context, payload string) error {
	if _, err := h.db.Exec(ctx, `select pg_notify($1, $2)`, h.store.Channel, payload); err != nil {
		return fmt.Errorf("notify collab: %w", err)
	}
	return nil
}

// claimSeed claims seeding the document's live state when nothing is stored yet and no fresh claim exists.
// A concurrent claim waits on the row lock, then sees the new seed_claimed_at and fails.
func (h *Hub) claimSeed(ctx context.Context, docID string) (bool, error) {
	tag, err := h.db.Exec(ctx,
		`update `+h.store.Docs+` set seed_claimed_at = now()
		 where id = $1 and (seed_claimed_at is null or seed_claimed_at < now() - interval '10 seconds')
		   and not exists (select 1 from `+h.store.Updates+` where `+h.store.Key+` = $1)`, docID)
	if err != nil {
		return false, fmt.Errorf("claim seed: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// renewSeed extends a seed claim that's still held.
func (h *Hub) renewSeed(ctx context.Context, docID string) error {
	_, err := h.db.Exec(ctx, `update `+h.store.Docs+` set seed_claimed_at = now() where id = $1 and seed_claimed_at is not null`, docID)
	if err != nil {
		return fmt.Errorf("renew seed claim: %w", err)
	}
	return nil
}

// setSeedClaim refreshes the seed claim (handed to another editor here) or clears it (free for any instance).
func (h *Hub) setSeedClaim(ctx context.Context, docID string, claimed bool) error {
	_, err := h.db.Exec(ctx, `update `+h.store.Docs+` set seed_claimed_at = case when $2 then now() end where id = $1`, docID, claimed)
	if err != nil {
		return fmt.Errorf("set seed claim: %w", err)
	}
	return nil
}

// compactDoc replaces the document's updates with the ids, the ones the client loaded, with data, their merged state.
// Deleting by id rather than "up to" keeps an update that committed after the client's load.
func (h *Hub) compactDoc(ctx context.Context, docID string, ids []int64, data []byte) error {
	t, k := h.store.Updates, h.store.Key
	err := pgx.BeginFunc(ctx, h.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from `+t+` where `+k+` = $1 and id = any($2)`, docID, ids); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into `+t+` (`+k+`, data) values ($1, $2)`, docID, data)
		return err
	})
	if err != nil {
		return fmt.Errorf("compact doc: %w", err)
	}
	return nil
}
