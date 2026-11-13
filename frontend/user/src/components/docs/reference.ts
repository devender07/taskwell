export type Endpoint = { method: 'GET' | 'POST' | 'DELETE'; path: string; about: string; fields?: [string, string][]; returns: string }
export type Group = { id: string; title: string; intro?: string; endpoints: Endpoint[] }

const ws = '/workspaces/{slug}'
const issue = `${ws}/issues/{identifier}`
const project = `${ws}/projects/{key}`
const cycle = `${project}/cycles/{number}`

const ISSUE_FIELDS: [string, string][] = [
  ['title', 'Up to 200 characters. Required on create.'],
  ['description', 'Rich text JSON, up to 50,000 characters. Empty clears it.'],
  ['status_id', 'A status id from GET /statuses. Defaults to the first status.'],
  ['assignee_id', 'A member who can see the project. Empty unassigns.'],
  ['priority', '0 no priority, 1 urgent, 2 high, 3 medium, 4 low.'],
  ['estimate', 'Story points: 1, 2, 3, 5 or 8. Empty clears it.'],
  ['due_date', 'YYYY-MM-DD. Empty clears it.'],
  ['label_ids', 'Repeated, one per label. Sending it replaces all labels.'],
  ['parent', 'The parent issue’s identifier, e.g. ENG-3. Empty makes it top-level.'],
  ['cycle', 'A cycle id in the issue’s project. Empty removes it from its cycle.'],
]

const PAGE_FIELDS: [string, string][] = [
  ['limit', '1–100. Turns on paging: the response is {"total", "groups": [{"key", "count", "issues", "has_more"}]}. Without it you get every issue as an array and the filters below are ignored.'],
  ['group', 'status (default), assignee, priority or none.'],
  ['sort', 'priority (default), updated, created or due.'],
  ['in', 'A group key: answers that group only, as {"issues", "has_more"}.'],
  ['offset', 'With in: how many of the group’s issues to skip.'],
  ['status', 'Comma-separated status ids.'],
  ['assignee', 'Comma-separated user ids, me, or none.'],
  ['label', 'Comma-separated label ids.'],
  ['priority', 'Comma-separated priorities, 0–4.'],
  ['cycle', 'Comma-separated cycle ids, or none.'],
]

export const REFERENCE: Group[] = [
  {
    id: 'workspaces',
    title: 'Workspaces',
    intro: 'A workspace is {"id", "name", "slug", "role": {"id", "name"}, "permissions": [...]}. The slug goes in every path below.',
    endpoints: [
      { method: 'GET', path: '/workspaces', about: 'The workspaces you belong to.', returns: '200 [workspace]' },
      { method: 'GET', path: ws, about: 'One workspace, with your role and permissions.', returns: '200 workspace' },
      {
        method: 'GET',
        path: `${ws}/members`,
        about: 'Members, and pending invites when you can invite.',
        fields: [
          ['limit', '1–100. Pages the members and adds "total" and "has_more".'],
          ['offset', 'Members to skip.'],
          ['q', 'Filter by name or email.'],
        ],
        returns: '200 {"members": [{"id", "name", "email", "role"}], "invites": [...]}',
      },
    ],
  },
  {
    id: 'projects',
    title: 'Projects',
    intro: 'A project is {"id", "name", "key", "description", "archived", "private", "members", "created_at", "start_date", "target_date"}. Paths use its key, e.g. ENG.',
    endpoints: [
      { method: 'GET', path: `${ws}/projects`, about: 'Projects you can see.', fields: [['archived', 'true lists archived projects instead.']], returns: '200 [project]' },
      {
        method: 'POST',
        path: `${ws}/projects`,
        about: 'Create a project. Needs the manage projects permission.',
        fields: [
          ['name', 'Required.'],
          ['key', 'Required. 2–5 letters or digits, starting with a letter.'],
          ['description', 'Plain text, up to 10,000 characters.'],
          ['private', 'true to limit it to its members.'],
          ['member_ids', 'Repeated. Members of a private project.'],
          ['start_date, target_date', 'YYYY-MM-DD.'],
        ],
        returns: '201 project · 409 {"errors": {"key"}} when the key is taken',
      },
      { method: 'GET', path: project, about: 'One project.', returns: '200 project' },
      {
        method: 'POST',
        path: project,
        about: 'Update a project. Send name, key and description every time; private and the dates change only when sent.',
        fields: [
          ['name, key, description', 'As on create.'],
          ['private', 'true or false.'],
          ['start_date, target_date', 'YYYY-MM-DD, or empty to clear.'],
        ],
        returns: '200 project',
      },
      { method: 'POST', path: `${project}/archive`, about: 'Archive it. POST …/unarchive restores it.', returns: '200 project' },
      { method: 'GET', path: `${project}/people`, about: 'Everyone who can see the project: who you can assign and mention. q matches name or email; limit (1-100) caps the list.', returns: '200 [person]' },
    ],
  },
  {
    id: 'issues',
    title: 'Issues',
    intro: 'Issues are addressed by identifier, e.g. ENG-12 (case doesn’t matter). An issue carries its project, status, assignee, priority, estimate (story points or null), labels, cycle, parent, sub-issues, links and linked pull requests, plus can_edit and can_delete for you.',
    endpoints: [
      { method: 'GET', path: `${project}/issues`, about: 'The project’s issues.', fields: PAGE_FIELDS, returns: '200 [issue], or the paged shape with limit' },
      { method: 'GET', path: `${ws}/my-issues`, about: 'Issues assigned to you across active projects. Same paging and filters.', returns: '200 [issue], or the paged shape with limit' },
      { method: 'GET', path: `${ws}/watching`, about: 'Issues you watch across active projects. Same paging and filters.', returns: '200 [issue], or the paged shape with limit' },
      { method: 'GET', path: `${cycle}/issues`, about: 'A cycle’s issues. Same paging and filters.', returns: '200 [issue], or the paged shape with limit' },
      {
        method: 'GET',
        path: `${ws}/issues`,
        about: 'Search issues by title, description or identifier.',
        fields: [
          ['q', 'Search text, up to 200 characters.'],
          ['project', 'A project key to search within.'],
        ],
        returns: '200 [{"id", "identifier", "title", "status", "assignee"}]',
      },
      { method: 'POST', path: `${project}/issues`, about: 'Create an issue. Needs the create issues permission.', fields: ISSUE_FIELDS, returns: '201 issue · 409 when the project is archived' },
      { method: 'GET', path: issue, about: 'One issue with its relations.', returns: '200 issue' },
      { method: 'POST', path: issue, about: 'Update an issue. Only the fields you send change.', fields: ISSUE_FIELDS, returns: '200 issue' },
      { method: 'DELETE', path: issue, about: 'Delete an issue you created, or any with the edit any issue permission.', returns: '204' },
      {
        method: 'POST',
        path: `${issue}/links`,
        about: 'Link two issues.',
        fields: [
          ['type', 'blocks or blocked_by.'],
          ['issue', 'The other issue’s identifier.'],
        ],
        returns: '201 issue · 409 when already linked',
      },
      { method: 'DELETE', path: `${issue}/links/{other}`, about: 'Remove the link to another issue.', returns: '200 issue' },
      { method: 'POST', path: `${issue}/watch`, about: 'Watch the issue. DELETE stops watching.', returns: '200 {"watching"}' },
      {
        method: 'GET',
        path: `${issue}/timeline`,
        about: 'Comments and activity, one page at a time, newest page first.',
        fields: [['before', 'An item’s created_at (RFC 3339): the page before it.']],
        returns: '200 {"comments": [comment], "events": [event], "has_more"}',
      },
    ],
  },
  {
    id: 'comments',
    title: 'Comments',
    intro: 'A comment is {"id", "author", "body", "created_at", "edited_at", "can_edit", "can_delete"}.',
    endpoints: [
      { method: 'POST', path: `${issue}/comments`, about: 'Comment on an issue.', fields: [['body', 'Rich text JSON, up to 20,000 characters.']], returns: '201 comment' },
      { method: 'POST', path: `${ws}/comments/{id}`, about: 'Edit your comment.', fields: [['body', 'Rich text JSON.']], returns: '200 comment' },
      { method: 'DELETE', path: `${ws}/comments/{id}`, about: 'Delete your comment, or any with the manage comments permission.', returns: '204' },
    ],
  },
  {
    id: 'labels-statuses',
    title: 'Labels and statuses',
    intro: 'Creating, changing and deleting them needs the manage workflow permission.',
    endpoints: [
      { method: 'GET', path: `${ws}/labels`, about: 'The workspace’s labels.', returns: '200 [{"id", "name", "color"}]' },
      {
        method: 'POST',
        path: `${ws}/labels`,
        about: 'Create a label. POST …/labels/{id} updates one; DELETE removes it.',
        fields: [
          ['name', 'Up to 30 characters.'],
          ['color', '#rrggbb.'],
        ],
        returns: '201 label',
      },
      { method: 'GET', path: `${ws}/statuses`, about: 'The workflow’s statuses, in order.', returns: '200 [{"id", "name", "category", "position", "icon", "color"}]' },
      {
        method: 'POST',
        path: `${ws}/statuses`,
        about: 'Create a status. POST …/statuses/{id} updates one.',
        fields: [
          ['name', 'Up to 30 characters.'],
          ['category', 'backlog, unstarted, started, completed or canceled.'],
          ['type_id', 'Optional status type id.'],
        ],
        returns: '201 status',
      },
      { method: 'DELETE', path: `${ws}/statuses/{id}`, about: 'Delete a status.', fields: [['move_to', 'Query parameter: the status that takes its issues.']], returns: '204 · 409 when it still has issues' },
    ],
  },
  {
    id: 'cycles',
    title: 'Cycles',
    intro: 'Cycles belong to a project and are addressed by number. Changes need the manage cycles permission.',
    endpoints: [
      { method: 'GET', path: `${project}/cycles`, about: 'The project’s cycles with progress counts; progress.points sums the estimates (total, started, completed), canceled issues left out.', returns: '200 [cycle]' },
      {
        method: 'POST',
        path: `${project}/cycles`,
        about: 'Create a cycle.',
        fields: [
          ['name', 'Optional.'],
          ['starts_on, ends_on', 'YYYY-MM-DD. Required.'],
        ],
        returns: '201 cycle',
      },
      { method: 'GET', path: cycle, about: 'A cycle and its issues.', returns: '200 {"cycle", "issues"}' },
      { method: 'POST', path: cycle, about: 'Update a cycle. Only the fields you send change.', fields: [['name, starts_on, ends_on', 'As on create.']], returns: '200 cycle' },
      { method: 'DELETE', path: cycle, about: 'Delete a cycle. Its issues stay in the project.', returns: '204' },
      {
        method: 'GET',
        path: `${cycle}/burndown`,
        about: 'Points remaining per day: scope and done for each recorded day of the cycle, today live while it’s current. planned is the scope on the first recorded day.',
        returns: '200 {"planned", "days": [{"date", "scope", "done"}]}',
      },
      { method: 'POST', path: `${cycle}/rollover`, about: 'Move the cycle’s unfinished issues to another cycle.', fields: [['to', 'The target cycle’s number.']], returns: '200 {"moved", "cycle"}' },
    ],
  },
  {
    id: 'feedback',
    title: 'Feedback',
    intro: 'Customer feedback, from the public form or added by members.',
    endpoints: [
      {
        method: 'GET',
        path: `${ws}/feedback`,
        about: 'Feedback, newest first.',
        fields: [
          ['state', 'new, linked or archived.'],
          ['q', 'Search text.'],
          ['before', 'The last item’s id: the next page.'],
        ],
        returns: '200 {"items": [feedback], "has_more"}',
      },
      { method: 'GET', path: `${ws}/feedback/count`, about: 'How many new items there are.', returns: '200 {"new"}' },
      {
        method: 'POST',
        path: `${ws}/feedback`,
        about: 'Add feedback.',
        fields: [
          ['body', 'Up to 5,000 characters. Required.'],
          ['email', 'Optional.'],
          ['name', 'Optional, up to 80 characters.'],
        ],
        returns: '201 feedback',
      },
      {
        method: 'POST',
        path: `${ws}/feedback/{id}`,
        about: 'Link it to an issue, or archive it.',
        fields: [
          ['issue', 'An issue identifier. Empty unlinks.'],
          ['archived', 'true or false. Needs the manage feedback permission.'],
        ],
        returns: '200 feedback',
      },
      { method: 'DELETE', path: `${ws}/feedback/{id}`, about: 'Delete it. Needs the manage feedback permission.', returns: '204' },
    ],
  },
  {
    id: 'inbox',
    title: 'Inbox',
    intro: 'Your notifications in a workspace.',
    endpoints: [
      {
        method: 'GET',
        path: `${ws}/notifications`,
        about: 'Notifications, newest first.',
        fields: [
          ['before', 'The last notification’s id: the next page.'],
          ['exclude', 'chat leaves out chat notifications.'],
        ],
        returns: '200 {"notifications": [...], "has_more"}',
      },
      { method: 'GET', path: `${ws}/notifications/unread-count`, about: 'How many are unread.', returns: '200 {"count"}' },
      {
        method: 'POST',
        path: `${ws}/notifications/read`,
        about: 'Mark notifications read. POST …/notifications/unread with ids marks them unread.',
        fields: [
          ['ids', 'Repeated notification ids.'],
          ['all', 'true marks every notification read.'],
        ],
        returns: '204',
      },
    ],
  },
]
