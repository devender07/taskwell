package export

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/file"
)

// writeZip writes the export to w as a zip: each file's bytes (streamed from storage, one at a time), then
// workspace.json, issues.csv, comments.csv and README.txt. A file whose object is gone is left out, its path null,
// and listed in README.txt.
func (h *Handler) writeZip(ctx context.Context, w io.Writer, e *Export) error {
	z := zip.NewWriter(w)
	var missing []string
	byIssue := map[string][]string{}
	for i := range e.Files {
		f := &e.Files[i]
		path := "files/" + f.ID + "-" + file.CleanName(f.Name)
		ok, err := h.copyFile(ctx, z, f.ID, path)
		if err != nil {
			return err
		}
		if !ok {
			missing = append(missing, f.Project+": "+f.Name)
			continue
		}
		f.Path = &path
		if f.Issue != nil {
			byIssue[*f.Issue] = append(byIssue[*f.Issue], path)
		}
	}
	for i := range e.Issues {
		if paths := byIssue[e.Issues[i].Identifier]; paths != nil {
			e.Issues[i].Files = paths
		}
	}

	out, err := z.CreateHeader(entry("workspace.json", e.ExportedAt))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(e); err != nil {
		return fmt.Errorf("write workspace.json: %w", err)
	}

	issues := [][]string{{"Identifier", "Project", "Title", "Description", "Status", "Priority", "Estimate", "Assignee", "Labels",
		"Due date", "Cycle", "Created"}}
	comments := [][]string{{"Issue", "Author", "Created", "Body"}}
	for _, i := range e.Issues {
		cycle := ""
		if i.Cycle != nil {
			cycle = strconv.Itoa(*i.Cycle)
		}
		issues = append(issues, []string{i.Identifier, i.Project, i.Title, i.DescriptionText, i.Status, i.Priority,
			estimate(i.Estimate), deref(i.Assignee), strings.Join(i.Labels, ", "), deref(i.DueDate), cycle, i.CreatedAt.Format(time.RFC3339)})
		for _, c := range i.Comments {
			comments = append(comments, []string{i.Identifier, deref(c.Author), c.CreatedAt.Format(time.RFC3339), c.BodyText})
		}
	}
	if err := writeCSV(z, "issues.csv", e.ExportedAt, issues); err != nil {
		return err
	}
	if err := writeCSV(z, "comments.csv", e.ExportedAt, comments); err != nil {
		return err
	}

	readme, err := z.CreateHeader(entry("README.txt", e.ExportedAt))
	if err != nil {
		return err
	}
	if _, err := io.WriteString(readme, readmeText(e, missing)); err != nil {
		return err
	}
	return z.Close()
}

// copyFile streams the object id into the zip at path; false when the object is gone.
func (h *Handler) copyFile(ctx context.Context, z *zip.Writer, id, path string) (bool, error) {
	body, err := h.files.Get(ctx, id)
	if err != nil || body == nil {
		return false, err
	}
	defer body.Close()
	// Store, not deflate: most uploads (images, PDFs, archives) are compressed already.
	out, err := z.CreateHeader(&zip.FileHeader{Name: path, Method: zip.Store, Modified: time.Now()})
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(out, body); err != nil {
		return false, fmt.Errorf("copy file %s: %w", id, err)
	}
	return true, nil
}

// entry is a deflated zip entry dated at (z.Create leaves the date empty, which unzip shows as 1979).
func entry(name string, at time.Time) *zip.FileHeader {
	return &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: at}
}

func writeCSV(z *zip.Writer, name string, at time.Time, rows [][]string) error {
	out, err := z.CreateHeader(entry(name, at))
	if err != nil {
		return err
	}
	cw := csv.NewWriter(out)
	if err := cw.WriteAll(rows); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func readmeText(e *Export, missing []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Taskwell export of %s (%s)\nExported %s by %s <%s>\n\n", e.Workspace.Name, e.Workspace.Slug,
		e.ExportedAt.Format(time.RFC3339), e.ExportedBy.Name, e.ExportedBy.Email)
	b.WriteString(`workspace.json  everything below, structured: members, roles, statuses, labels, projects (milestones,
                cycles, updates), issues (comments, attached files), feedback and files
issues.csv      one row per issue, in the columns the CSV import reads
comments.csv    one row per comment
files/          every project file and issue attachment, named <id>-<name>

Private projects the exporter isn't in are left out, with their issues, comments and files. Deleted issues are
left out. People are their email addresses; empty when the account is gone.
`)
	if len(missing) > 0 {
		b.WriteString("\nThese files' contents were missing from storage and are left out:\n")
		for _, m := range missing {
			b.WriteString("  " + m + "\n")
		}
	}
	return b.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// estimate is an issue's points as a CSV cell, "" when it has none.
func estimate(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}
