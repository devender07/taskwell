package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/notify"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

// FromTemplate is an issue a recurring template creates. StatusID "" is the workspace's first status; AssigneeID ""
// is nobody. The caller has checked the assignee can see the project.
type FromTemplate struct {
	ProjectID, CreatorID, Title, StatusID, AssigneeID string
	Description                                       json.RawMessage
	Priority                                          int
	LabelIDs                                          []string // ids that aren't the workspace's labels are skipped
}

// CreateFromTemplate adds the issue in tx like create does (numbered and ranked last in its project, labelled, logged,
// the assignee notified, published) and returns its id. The caller checks the project isn't archived.
func CreateFromTemplate(ctx context.Context, tx pgx.Tx, t FromTemplate) (string, error) {
	var workspaceID, id string
	var number int
	if err := tx.QueryRow(ctx, `update projects set issue_seq = issue_seq + 1 where id = $1 returning workspace_id::text, issue_seq`,
		t.ProjectID).Scan(&workspaceID, &number); err != nil {
		return "", fmt.Errorf("number issue: %w", err)
	}
	rank, err := place(ctx, tx, t.ProjectID, "", "", "")
	if err != nil {
		return "", err
	}
	if err := tx.QueryRow(ctx,
		`insert into issues (workspace_id, project_id, number, title, description, description_text, status_id, assignee_id,
		   priority, created_by, rank)
		 values ($1, $2, $3, $4, $5, $6,
		         coalesce(nullif($7, '')::uuid, (select id from statuses where workspace_id = $1 order by position, id limit 1)),
		         nullif($8, '')::uuid, $9, $10, $11)
		 returning id::text`,
		workspaceID, t.ProjectID, number, t.Title, t.Description, doc.PlainText(t.Description), t.StatusID, t.AssigneeID,
		t.Priority, t.CreatorID, rank).Scan(&id); err != nil {
		return "", fmt.Errorf("insert issue: %w", err)
	}
	if err := setLabels(ctx, tx, workspaceID, id, t.LabelIDs); err != nil {
		return "", fmt.Errorf("label issue: %w", err)
	}
	if err := addEvents(ctx, tx, id, t.CreatorID, []event{{"created", struct{}{}}}); err != nil {
		return "", err
	}
	if err := notify.Record(ctx, tx, notify.Change{IssueID: id, ActorID: t.CreatorID, Watch: []string{t.CreatorID},
		Assignee: t.AssigneeID, Mentioned: doc.Mentions(t.Description)}); err != nil {
		return "", err
	}
	n, err := issueNames(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if err := realtime.Publish(ctx, tx, n.event("issue.created", t.CreatorID, slices.Concat(n.relatedPaths(),
		[]string{realtime.ProjectIssues(n.slug, n.key), realtime.Project(n.slug, n.key), realtime.MyIssues(n.slug),
			realtime.Watching(n.slug), realtime.Activity(n.slug), realtime.ProjectStats(n.slug), realtime.Roadmap(n.slug)})...)); err != nil {
		return "", err
	}
	return id, realtime.Publish(ctx, tx, realtime.AdminStats(t.CreatorID))
}
