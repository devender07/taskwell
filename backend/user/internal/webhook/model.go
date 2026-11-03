package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/realtime"
)

const maxWebhooks = 10

type Webhook struct {
	ID           string        `json:"id"`
	URL          string        `json:"url"`
	Events       []string      `json:"events"`
	Active       bool          `json:"active"`
	CreatedAt    time.Time     `json:"created_at"`
	LastDelivery *LastDelivery `json:"last_delivery"`
}

type LastDelivery struct {
	Status       string    `json:"status"`
	ResponseCode *int      `json:"response_code"`
	CreatedAt    time.Time `json:"created_at"`
}

type Delivery struct {
	ID            string          `json:"id"`
	Event         string          `json:"event"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	ResponseCode  *int            `json:"response_code"`
	ResponseError *string         `json:"response_error"`
	CreatedAt     time.Time       `json:"created_at"`
	DeliveredAt   *time.Time      `json:"delivered_at"`
	NextAt        time.Time       `json:"next_at"`
	Payload       json.RawMessage `json:"payload"`
}

type model struct{ db *pgxpool.Pool }

const webhookCols = `select w.id::text, w.url, w.events, w.active, w.created_at, d.status, d.response_code, d.created_at
	from webhooks w left join lateral (select status, response_code, created_at from webhook_deliveries
	  where webhook_id = w.id order by id desc limit 1) d on true`

func scanWebhook(row pgx.CollectableRow) (*Webhook, error) {
	var wh Webhook
	var status *string
	var ld LastDelivery
	var ldAt *time.Time
	if err := row.Scan(&wh.ID, &wh.URL, &wh.Events, &wh.Active, &wh.CreatedAt, &status, &ld.ResponseCode, &ldAt); err != nil {
		return nil, err
	}
	wh.CreatedAt = wh.CreatedAt.UTC()
	if status != nil {
		ld.Status, ld.CreatedAt = *status, ldAt.UTC()
		wh.LastDelivery = &ld
	}
	return &wh, nil
}

// list returns the workspace's webhooks, oldest first.
func (m *model) list(ctx context.Context, wsID string) ([]*Webhook, error) {
	rows, err := m.db.Query(ctx, webhookCols+` where w.workspace_id = $1 order by w.id`, wsID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	whs, err := pgx.CollectRows(rows, scanWebhook)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	return whs, nil
}

// get returns the workspace's webhook; nil when there's none.
func (m *model) get(ctx context.Context, wsID, id string) (*Webhook, error) {
	rows, err := m.db.Query(ctx, webhookCols+` where w.workspace_id = $1 and w.id = $2`, wsID, id)
	if err != nil {
		return nil, fmt.Errorf("read webhook: %w", err)
	}
	wh, err := pgx.CollectExactlyOneRow(rows, scanWebhook)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read webhook: %w", err)
	}
	return wh, nil
}

// newID returns an id for a webhook about to be created, so its secret can be sealed to it.
func (m *model) newID(ctx context.Context) (string, error) {
	var id string
	if err := m.db.QueryRow(ctx, `select uuidv7()::text`).Scan(&id); err != nil {
		return "", fmt.Errorf("new webhook id: %w", err)
	}
	return id, nil
}

// create adds the webhook; false when the workspace already has maxWebhooks.
func (m *model) create(ctx context.Context, e realtime.Event, wsID, id, url string, events []string, sealed, userID string) (bool, error) {
	ok := false
	err := publishing(ctx, m.db, e, func(tx pgx.Tx) error {
		// The workspace row's lock serializes creates, so two can't both pass the count.
		if _, err := tx.Exec(ctx, `select 1 from workspaces where id = $1 for no key update`, wsID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`insert into webhooks (id, workspace_id, url, secret, events, created_by)
			 select $1, $2, $3, $4, $5, $6 where (select count(*) from webhooks where workspace_id = $2) < $7`,
			id, wsID, url, sealed, events, userID, maxWebhooks)
		ok = tag.RowsAffected() > 0
		return err
	})
	return ok, err
}

// update changes the fields that aren't nil; false when the webhook isn't the workspace's.
func (m *model) update(ctx context.Context, e realtime.Event, wsID, id string, url *string, events []string, active *bool) (bool, error) {
	return m.change(ctx, e,
		`update webhooks set url = coalesce($3, url), events = coalesce($4, events), active = coalesce($5, active)
		 where workspace_id = $1 and id = $2`, wsID, id, url, events, active)
}

func (m *model) setSecret(ctx context.Context, e realtime.Event, wsID, id, sealed string) (bool, error) {
	return m.change(ctx, e, `update webhooks set secret = $3 where workspace_id = $1 and id = $2`, wsID, id, sealed)
}

func (m *model) delete(ctx context.Context, e realtime.Event, wsID, id string) (bool, error) {
	return m.change(ctx, e, `delete from webhooks where workspace_id = $1 and id = $2`, wsID, id)
}

// ping queues a ping from the user to the webhook, active or not.
func (m *model) ping(ctx context.Context, e realtime.Event, wsID, id, userID string) (bool, error) {
	return m.change(ctx, e, `select webhook_enqueue(workspace_id, 'ping', jsonb_build_object('actor', webhook_actor($3)), id)
		from webhooks where workspace_id = $1 and id = $2`, wsID, id, userID)
}

// redeliver queues a copy of the webhook's delivery with a new id.
func (m *model) redeliver(ctx context.Context, e realtime.Event, wsID, id, deliveryID string) (bool, error) {
	return m.change(ctx, e,
		`insert into webhook_deliveries (id, webhook_id, event, payload)
		 select n.id, d.webhook_id, d.event, jsonb_set(d.payload, '{id}', to_jsonb(n.id))
		 from webhook_deliveries d join webhooks w on w.id = d.webhook_id, lateral (select uuidv7() as id) n
		 where w.workspace_id = $1 and w.id = $2 and d.id = $3`, wsID, id, deliveryID)
}

// change runs sql, publishing e when it touched a row; false when it didn't.
func (m *model) change(ctx context.Context, e realtime.Event, sql string, args ...any) (bool, error) {
	found := false
	err := publishing(ctx, m.db, e, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		found = tag.RowsAffected() > 0
		return err
	})
	return found, err
}

// deliveries returns the webhook's newest 50 deliveries; found false when it isn't the workspace's.
func (m *model) deliveries(ctx context.Context, wsID, id string) (ds []Delivery, found bool, err error) {
	if err := m.db.QueryRow(ctx, `select exists(select 1 from webhooks where workspace_id = $1 and id = $2)`, wsID, id).Scan(&found); err != nil || !found {
		return nil, false, err
	}
	rows, err := m.db.Query(ctx,
		`select id::text, event, status, attempts, response_code, response_error, created_at, delivered_at, next_at, payload
		 from webhook_deliveries where webhook_id = $1 order by id desc limit 50`, id)
	if err != nil {
		return nil, true, fmt.Errorf("list deliveries: %w", err)
	}
	ds, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Delivery, error) {
		var d Delivery
		err := row.Scan(&d.ID, &d.Event, &d.Status, &d.Attempts, &d.ResponseCode, &d.ResponseError, &d.CreatedAt,
			&d.DeliveredAt, &d.NextAt, &d.Payload)
		d.CreatedAt, d.NextAt = d.CreatedAt.UTC(), d.NextAt.UTC()
		if d.DeliveredAt != nil {
			*d.DeliveredAt = d.DeliveredAt.UTC()
		}
		return d, err
	})
	if err != nil {
		return nil, true, fmt.Errorf("list deliveries: %w", err)
	}
	return ds, true, nil
}

// job is a claimed delivery.
type job struct {
	id, webhookID, wsID, slug, event, url, sealed string
	payload                                       []byte
	attempts                                      int
}

// claim takes the oldest due delivery of an active webhook, counting the attempt and pushing next_at past the send
// (so a crashed send is retried), and commits before the send; nil when nothing is due.
func (m *model) claim(ctx context.Context) (*job, error) {
	var j job
	err := m.db.QueryRow(ctx,
		`update webhook_deliveries d set attempts = d.attempts + 1, next_at = now() + interval '1 minute'
		 from webhooks w join workspaces s on s.id = w.workspace_id
		 where w.id = d.webhook_id and d.id = (
		   select d2.id from webhook_deliveries d2 join webhooks w2 on w2.id = d2.webhook_id
		   where d2.status = 'pending' and d2.next_at <= now() and w2.active
		   order by d2.next_at limit 1 for update of d2 skip locked)
		 returning d.id::text, w.id::text, w.workspace_id::text, s.slug, d.event, w.url, w.secret, d.payload::text, d.attempts`).
		Scan(&j.id, &j.webhookID, &j.wsID, &j.slug, &j.event, &j.url, &j.sealed, &j.payload, &j.attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim delivery: %w", err)
	}
	return &j, nil
}

// record stores a send's outcome: status delivered, failed, or pending again after retry.
func (m *model) record(ctx context.Context, j *job, status string, code *int, errMsg *string, retry time.Duration) error {
	e := realtime.Event{Kind: "webhooks.changed", Workspace: j.slug, Audience: realtime.ToWorkspace(j.wsID),
		Paths: []string{webhooksPath(j.slug), deliveriesPath(j.slug, j.webhookID)}}
	return publishing(ctx, m.db, e, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`update webhook_deliveries set status = $2, response_code = $3, response_error = $4,
			   delivered_at = case when $2 = 'delivered' then now() end,
			   next_at = case when $2 = 'pending' then now() + make_interval(secs => $5) else next_at end
			 where id = $1`, j.id, status, code, errMsg, retry.Seconds())
		return err
	})
}

// publishing runs fn in a transaction that publishes e when it commits.
func publishing(ctx context.Context, db *pgxpool.Pool, e realtime.Event, fn func(pgx.Tx) error) error {
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("change webhooks: %w", err)
	}
	return nil
}

func webhooksPath(slug string) string { return realtime.Workspace(slug) + "/webhooks" }

func deliveriesPath(slug, id string) string { return webhooksPath(slug) + "/" + id + "/deliveries" }
