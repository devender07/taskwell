# Import plan

Scope: bring existing issues into a Taskwell project from a CSV export (Linear, Jira or any CSV with a title column) or straight from a GitHub repository. A dry run previews what will happen; the import itself is one transaction.

Out of scope:
- Linear and Jira API importers. Their CSV exports cover the same data.
- Keeping imports in sync.
- Skipping issues already imported. `ponytail:` importing twice creates duplicates. If that bites, store a source id on issues.
- Importing attachments.
- Keeping original creation dates.
- Sub-issues and links.

## API contract

Both endpoints need `issues.create` and a project the caller can see that isn't archived.

| Method & path | FormData | Success |
|---|---|---|
| `POST /api/workspaces/{slug}/import/csv` | `file` (one CSV, at most 10 MB and 5,000 data rows), `project` (key), `dry_run` (`1` = preview only) | `200 summary` on a dry run, `201 summary` once imported |
| `POST /api/workspaces/{slug}/import/github` | `repo` (`owner/name` or a github.com URL), `token` (optional, used once and never stored), `closed` (`1` = include closed issues), `project`, `dry_run` | same |

The summary:

```json
{
  "source": "linear" | "jira" | "csv" | "github",
  "total": 120,
  "statuses": [{"from": "In Progress", "to": {"id": "…", "name": "In progress"}, "count": 14}],
  "new_labels": ["bug", "ui"],
  "unmatched_assignees": ["sam@acme.com"],
  "comments": 37,
  "skipped": [{"row": 12, "error": "Title is required."}],
  "imported": 118,
  "first": "ABC-41"
}
```

`imported` and `first` (the first new identifier) are only present when it isn't a dry run. `row` is the 1-based CSV data row, or the GitHub issue number.

Failures:
- `400 {"errors": {…}}`, one message per field:

  | Field | Message |
  |---|---|
  | `file` | "Choose a CSV file." |
  | `file` | "The file is over 10 MB." |
  | `file` | "Over 5,000 rows. Split the file." |
  | `file` | "No title column found. Name one Title or Summary." |
  | `file` | "This file isn't a valid CSV." |
  | `project` | "Choose a project." |
  | `repo` | "Enter a repository like owner/name." |
  | `repo` | "Repository not found or not accessible." |
  | `token` | "This token isn't valid." |
- `400 {"error": "Nothing to import."}` when every row is skipped or the repo has no issues.
- `403` without `issues.create`.
- `404` for the workspace or project.
- `409` "This project is archived."
- `429 {"error": "GitHub's rate limit was hit. Add a token or try again later."}`
- `502 {"error": "GitHub didn't answer. Try again."}`

## Mapping

CSV headers are matched case-insensitively. Repeated columns, like Jira's "Labels" and "Comment", are all read.

| Field | Columns | Rule |
|---|---|---|
| title | Title, Summary, Name | Required; a row without one is skipped |
| description | Description, Body | Plain text → doc (`doc.FromText`, a port of mcp's `toDoc`) |
| status | Status | Same name (case-insensitive) as a workspace status; otherwise by keyword: done/closed/resolved/complete → first `completed`; cancel/won't/duplicate/invalid → first `canceled`; progress/review/doing → first `started`; backlog/icebox/triage → first `backlog`; else the default status |
| priority | Priority | urgent/highest/blocker/critical → 1, high → 2, medium/normal → 3, low/lowest/minor/trivial → 4, a digit 0–4 as is, else 0 |
| assignee | Assignee, Assignee email | A member with that email, else a member with that exact name (case-insensitive) who can see the project; else none, and the value is listed in `unmatched_assignees` |
| labels | Labels, Label | Split on commas; existing labels matched by name case-insensitively; missing ones created (default label color) |
| due date | Due date, Due | `YYYY-MM-DD`, RFC 3339, or Jira's `02/Jan/06 3:04 PM`; anything else is ignored |
| comments | Comment (Jira: `date;author;body`) | One comment per non-empty cell |
| original id | ID, Issue key | Appended to the description as "Imported from {Source} {id}." |

Source detection:
- `jira` when there are "Issue key" and "Summary" columns.
- `linear` when there are "ID", "Title" and "Team" columns.
- Otherwise `csv`.

GitHub uses the REST API: `GET /repos/{repo}/issues?state=open|all&per_page=100`, paged, skipping pull requests, with at most 2,000 issues.
- The body becomes the description.
- Status:
  - open → the default status
  - closed with `state_reason` `not_planned` → canceled
  - other closed → completed
- Labels → labels. Assignees are left empty (GitHub doesn't give emails), and logins aren't listed.
- Comments come from `GET /repos/{repo}/issues/{n}/comments`, but only for issues with `comments > 0`.
- The footer is "Imported from GitHub owner/name#12."
- Requests carry `Authorization: Bearer {token}` when a token is given, plus a 15s timeout.

Comments are created by the importer. The body is "**{author}** · {date}" on the first line, then the text.

## Backend (`backend/user/internal/issue/`)

- `import.go` holds the parsing with `encoding/csv` (`LazyQuotes`, `FieldsPerRecord = -1`), the mapping, and the GitHub fetch through the default `http.Client`. The host is fixed to api.github.com, so netguard isn't needed.
- `model.go` gets `importIssues`. In one transaction it:
  - takes a block of issue numbers from `issue_seq`
  - creates the missing labels
  - inserts issues ranked after the project's last issue
  - sets labels and inserts comments
  - adds one `created` event per issue
- Imports send no notifications and no webhooks. One realtime publish at the end covers the project's issue paths and activity.
- A dry run runs the same mapping without writing.
- Tests in `import_test.go` cover:
  - Linear and Jira header detection
  - status keyword mapping
  - priority mapping
  - Jira comment cells
  - a dry run writing nothing
  - an import landing in a project, with numbers continuing after the existing ones
- GitHub is tested with an `httptest` server: the base URL is a package variable the test overrides.

## Frontend (user app)

- New route `settings/import.tsx`, shown in the settings nav as "Import" (icon `UploadIcon`, added to icons.tsx if missing) when the user has `issues.create`.
- The page has two tabs, "CSV" and "GitHub".
  - **CSV**: a file input (accept `.csv`), a project `SmartAutocomplete` (non-archived projects), and the hint "Export from Linear (Settings → Import/export) or Jira (Filters → Export CSV), or any CSV with a Title column."
  - **GitHub**: a repository input, a token password input (hint "Needed for private repositories. Used once, never stored."), a checkbox "Include closed issues", and a project.
- "Preview" posts with `dry_run=1` and shows the summary:
  - "{total} issues from {Source}"
  - a status mapping table
  - "New labels: …"
  - "Not matched, left unassigned: …"
  - the comments count
  - skipped rows (up to 20, then "and N more")
- Then "Import {n} issues" posts for real. On 201 the page shows "Imported {n} issues" with a link to the project. Errors go through `toFormErrors` to fields or `FormError`.
- The project page's empty state gets a secondary "Import issues" link to the import page with `?project=KEY`. The import page reads that param and preselects the project.

Sentence case everywhere.

## Verification

- `go vet`, `go test`. Curl a Linear-style and a Jira-style CSV as a dry run and for real.
- `npm run build`.
- Integration: import a sample Linear CSV and a Jira CSV into a project, then check statuses, labels, assignees, comments and descriptions. Import a small public GitHub repo's issues. Check the errors: no title column, an archived project and a bad repo.
