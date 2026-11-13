# Keyboard-first plan (Phase 5)

Scope: a command palette (`⌘K` / `Ctrl+K`) to jump anywhere and run actions, quick-create (`C`) from anywhere, fast search across issues (identifier, title, description text) and projects, and single-key shortcuts on an issue page (`A` assign, `S` status, `P` priority, `L` labels). No new dependencies.

Out of scope: user-customisable shortcuts, `G then X` chords (only `?` help + the keys above), search across comments, search result pages.

## Search backend

Migration `backend/user/internal/db/migrations/016_search.sql`:

```sql
create extension if not exists pg_trgm;
alter table issues add column description_text text not null default '';
update issues set description_text = coalesce((select string_agg(t #>> '{}', ' ') from jsonb_path_query(description, 'strict $.**.text') t), '') where description is not null;
create index on issues using gin (title gin_trgm_ops);
create index on issues using gin (description_text gin_trgm_ops);
create index on projects using gin (name gin_trgm_ops);
```

Every place that saves a description (create, update, live `save`) also stores `description_text` (plain text from the doc: text nodes + mention labels, joined by spaces/newlines — add `plainText(doc)` next to `checkDoc` with a test row).

`GET /api/workspaces/{slug}/search?q=` (member):

```json
{"issues":[{"id","identifier","title","status":status,"assignee":person|null,"project":{"key","name"},"match":"title"|"description"|"identifier"}],
 "projects":[{"key","name","archived"}]}
```

- `q` trimmed; empty → `{"issues": newest 8 updated, "projects": all active, max 8}`.
- Issues (max 12): exact identifier first (`ENG-12`, any case, old keys via `canonical_key`), then `title ilike %q%` or `word_similarity(q, title) > 0.3`, then description matches (`description_text ilike %q%`), ordered by match kind then `updated_at desc`. Archived projects' issues included but after others.
- Projects (max 8): `key = upper(q)` first, then name ilike / trigram.
- The existing `GET …/issues?q=` (pickers) stays as is.
- Verify: `EXPLAIN` uses the trgm indexes on a few thousand generated rows (generate in a transaction and roll back); curl each match kind; 404 non-member.

## Frontend (user app)

### Shortcut handling

`components/app/shortcuts.tsx`: one `keydown` listener on `window` (mounted in the workspace layout) that ignores events when focus is in an input/textarea/contenteditable/select or a modifier other than ⌘/Ctrl+K is held, or a dialog is open (except the palette's own keys).

| Key | Where | Does |
|---|---|---|
| `⌘K` / `Ctrl+K` | anywhere (also inside inputs) | open the palette (toggle) |
| `C` | anywhere | open the New issue dialog |
| `A`, `S`, `P`, `L` | issue page, `can_edit` | open the palette in that issue's assignee / status / priority / labels mode |
| `/` | anywhere | open the palette (search) |
| `?` | anywhere | open the shortcuts help dialog |

Show shortcut hints: `⌘K` hint in the sidebar ("Search" item with a `kbd`), "C" in the New issue button `title`, keys in the help dialog. Use `⌘` on Apple platforms, `Ctrl` elsewhere.

### Command palette (`components/app/command-palette.tsx`)

- A `Dialog`-like modal (reuse shared `Dialog` if it fits, else native `<dialog>` with the same styles), top-aligned (15vh), width min(640px, 100% - 32px), input at top ("Search or type a command…"), grouped results list below (combobox/listbox ARIA, ↑/↓, Enter, Esc closes, mouse/touch tap). Max height 60vh, scrolls.
- **Root mode**, groups in order (empty groups hidden):
  1. *Issues* — from `GET …/search?q=` (debounced 120ms, abort stale; show when q non-empty, and "Recent" = the empty-q result when q is empty). Row: identifier (mono), title, status name hint. Enter → issue page.
  2. *Projects* — from the same response. Enter → project page.
  3. *Actions* — filtered client-side by `match.ts`: "Create issue" (C), "Go to My issues", "Go to Projects", "Go to Inbox" (only if the route exists), "Go to Settings → Profile / Workspace / Members / Roles / Workflow / Labels / Notifications (if exists)", and on an issue page with `can_edit`: "Change status…" (S), "Assign to…" (A), "Set priority…" (P), "Change labels…" (L), "Copy issue link", "Copy issue ID".
- **Issue modes** (status / assignee / priority / labels): breadcrumb chip "ENG-12 › Status" in the input row, Backspace on empty input goes back to root. Options from the issue route's loader data (statuses, members incl. "No assignee", priorities, labels). Status/assignee/priority: Enter saves via the existing partial update (`POST …/issues/{identifier}` with the one field), closes, `router.invalidate()`. Labels: Enter toggles the label (checkbox state shown), palette stays open; Esc closes. Errors show as a line under the input.
- The palette reads "current issue" context from a small React context set by the issue page (`IssueContext` provider with issue + statuses + labels + members + workspace), so the palette needs no extra fetch.

### Quick-create

`C` and the palette action open the existing `CreateIssueForm` in a `Dialog` with a **Project** `SmartAutocomplete` at the top (active projects; default = the project of the current page/issue, else the last used project from `localStorage` `taskwell:last-project:<workspace>`, else the first). Statuses/labels/members come from the workspace (fetch statuses/labels/members/projects on open, cached for the dialog's lifetime). The existing project-page "New issue" keeps working (it can reuse the same dialog with the project preset).

### Help dialog

`?` → Dialog "Keyboard shortcuts" listing the table above.

## Verification

- Backend as above; `npm run build`.
- Integration (headless Chrome): ⌘K opens, typing `ENG-3` → first result ENG-3, Enter navigates; typing a word that's only in a description finds it; a project by name; "Go to My issues" works; on an issue `S` → pick "In progress" → saved; `A`, `P`, `L` likewise; `C` from Home → create in the default project; shortcuts don't fire while typing in an input or the editor; Esc closes; `?` shows help. At iPad size the palette fits and is tappable (opened via the sidebar Search item).
