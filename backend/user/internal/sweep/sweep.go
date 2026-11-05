// Package sweep deletes what the app no longer needs: spent sign-in tokens, old read notifications, given-up email,
// stale presence, and uploads nothing references. One instance at a time runs it, under an advisory lock.
//
// sweep.go runs it and removes files from storage; model.go owns every query.
package sweep

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

// fileGrace is how long an upload may stay unreferenced (a draft not yet saved) before it's deleted.
const fileGrace = 24 * time.Hour

// sweepLock is the session advisory lock held while a sweep runs.
const sweepLock = 7292

type Sweeper struct {
	db    *pgxpool.Pool
	files *storage.Store
}

func New(db *pgxpool.Pool, files *storage.Store) *Sweeper { return &Sweeper{db: db, files: files} }

// Run sweeps a minute after start (sooner when every is shorter), then every every, until ctx ends.
// ponytail: each instance sweeps on its own schedule, so N instances sweep N times per interval (the lock only stops
// overlaps); harmless since a sweep after a sweep finds little. Record the last run in a table if that wastes much.
func (s *Sweeper) Run(ctx context.Context, every time.Duration) {
	t := time.NewTimer(min(every, time.Minute))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			slog.Error("sweep failed", "err", err)
		}
		t.Reset(every)
	}
}

// Sweep runs one sweep, unless another instance is running one.
func (s *Sweeper) Sweep(ctx context.Context) error {
	pc, err := s.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack() // the session lock and settings die with it, even if unlocking fails
	defer conn.Close(context.WithoutCancel(ctx))
	var locked bool
	if err := conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, sweepLock).Scan(&locked); err != nil {
		return fmt.Errorf("sweep lock: %w", err)
	}
	if !locked {
		slog.Info("sweep skipped: another instance is sweeping")
		return nil
	}
	if _, err := conn.Exec(ctx, `set statement_timeout = '5min'`); err != nil {
		return fmt.Errorf("sweep session: %w", err)
	}
	start := time.Now()
	m := model{conn}
	counts := []any{}
	for _, r := range retention {
		n, err := m.deleteAll(ctx, r.table, r.where)
		if err != nil {
			return fmt.Errorf("sweep %s: %w", r.table, err)
		}
		counts = append(counts, r.table, n)
	}
	big, err := m.bigDocs(ctx)
	if err != nil {
		return err
	}
	for _, d := range big {
		// ponytail: only a client can merge Yjs updates (no Yjs in Go); the next editor to open the issue compacts it.
		// Merge server-side with a Yjs port, or close and reseed the doc from its saved description, if this logs often.
		slog.Warn("issue has many live-editing updates", "issue", d.issueID, "updates", d.rows)
	}
	ids, err := m.deleteUnreferencedFiles(ctx, fileGrace)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.remove(ctx, id)
	}
	orphans, err := s.sweepBucket(ctx, m)
	if err != nil {
		return err
	}
	slog.Info("sweep done", append(counts, "files", len(ids), "orphans", orphans, "duration_ms", time.Since(start).Milliseconds())...)
	return nil
}

func (s *Sweeper) remove(ctx context.Context, id string) {
	if err := s.files.Delete(ctx, id); err != nil {
		slog.Warn("sweep remove file", "id", id, "err", err)
	}
}

// sweepBucket removes objects older than fileGrace that have no row: their workspace was deleted, or a crash came
// between deleting a row and its object.
// ponytail: lists the whole bucket each sweep; fine to ~1M objects, then use S3 inventory or lifecycle rules.
func (s *Sweeper) sweepBucket(ctx context.Context, m model) (int, error) {
	removed := 0
	err := s.files.Old(ctx, time.Now().Add(-fileGrace), func(keys []string) error {
		var ids []string
		for _, k := range keys {
			if web.IsUUID(k) {
				ids = append(ids, k)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		gone, err := m.missingFiles(ctx, ids)
		if err != nil {
			return err
		}
		for _, id := range gone {
			s.remove(ctx, id)
		}
		removed += len(gone)
		return nil
	})
	return removed, err
}
