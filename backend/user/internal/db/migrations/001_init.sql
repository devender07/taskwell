-- The whole schema as of 043_status_types, squashed from migrations 001-043 (pg_dump of a fresh database).
-- New changes go in 002_*.sql and on.
set local check_function_bodies = false;

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE FUNCTION public.admin_audit_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  raise exception 'admin_audit is append-only';
end $$;

CREATE FUNCTION public.can_see_project(project uuid, viewer uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  select not p.private
      or exists (select 1 from project_members m where m.project_id = p.id and m.user_id = viewer)
      or exists (select 1 from workspace_members wm join workspace_roles r on r.id = wm.role_id
                 where wm.workspace_id = p.workspace_id and wm.user_id = viewer and r.builtin = 'admin')
    from projects p where p.id = project
$$;

CREATE FUNCTION public.canonical_key(ws uuid, k text) RETURNS text
    LANGUAGE sql STABLE
    AS $$
  select coalesce(
    (select p.key from projects p where p.workspace_id = ws and p.key = k),
    (select p.key from project_key_aliases a join projects p on p.id = a.project_id where a.workspace_id = ws and a.key = k),
    k)
$$;

CREATE TABLE public.chats (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    kind text NOT NULL,
    name text,
    pair text,
    created_by uuid,
    last_message_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chats_kind_check CHECK ((kind = ANY (ARRAY['workspace'::text, 'self'::text, 'direct'::text, 'group'::text])))
);

CREATE FUNCTION public.chat_name(c public.chats, viewer uuid) RETURNS text
    LANGUAGE sql STABLE
    AS $$
  select case c.kind
    when 'workspace' then (select name from workspaces where id = c.workspace_id)
    when 'self' then 'Notes'
    when 'group' then c.name
    else coalesce((select u.name from chat_members o join users u on u.id = o.user_id
                   where o.chat_id = c.id and o.user_id <> viewer order by o.joined_at limit 1), 'Someone')
  end
$$;

CREATE FUNCTION public.project_key_alias() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  delete from project_key_aliases where workspace_id = new.workspace_id and key = new.key;
  if tg_op = 'UPDATE' and old.key <> new.key then
    insert into project_key_aliases (workspace_id, key, project_id) values (old.workspace_id, old.key, old.id)
    on conflict (workspace_id, key) do update set project_id = excluded.project_id;
  end if;
  return new;
end
$$;

CREATE FUNCTION public.seed_statuses() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  insert into statuses (workspace_id, name, category, position) values
    (new.id, 'Backlog', 'backlog', 0), (new.id, 'Todo', 'unstarted', 1), (new.id, 'In progress', 'started', 2),
    (new.id, 'Done', 'completed', 3), (new.id, 'Cancelled', 'canceled', 4);
  return null;
end $$;

CREATE TABLE public.statuses (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    "position" integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    type_id uuid,
    CONSTRAINT statuses_category_check CHECK ((category = ANY (ARRAY['backlog'::text, 'unstarted'::text, 'started'::text, 'completed'::text, 'canceled'::text])))
);

CREATE FUNCTION public.status_json(s public.statuses) RETURNS json
    LANGUAGE sql STABLE
    AS $$
  select json_build_object('id', s.id, 'name', s.name, 'category', s.category, 'position', s.position,
                           'icon', t.icon, 'color', t.color)
  from (select) x left join status_types t on t.id = s.type_id
$$;

CREATE TABLE public.admin_audit (
    id uuid DEFAULT uuidv7() NOT NULL,
    at timestamp with time zone DEFAULT now() NOT NULL,
    actor_id uuid,
    actor_email text DEFAULT ''::text NOT NULL,
    action text NOT NULL,
    target text DEFAULT ''::text NOT NULL,
    ip text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.admin_mfa (
    user_id uuid NOT NULL,
    secret text NOT NULL,
    enabled_at timestamp with time zone,
    last_step bigint DEFAULT 0 NOT NULL,
    recovery_codes text[] DEFAULT '{}'::text[] NOT NULL
);

CREATE TABLE public.admin_sessions (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    mfa_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL
);

CREATE TABLE public.ai_usage (
    workspace_id uuid NOT NULL,
    run_id uuid,
    cost_micros bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.api_tokens (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    token_hash bytea NOT NULL,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT api_tokens_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 60)))
);

CREATE TABLE public.auth_failures (
    key text NOT NULL,
    at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.auth_tokens (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    purpose text NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    name text,
    password_hash text,
    workspace_name text,
    CONSTRAINT auth_tokens_purpose_check CHECK ((purpose = ANY (ARRAY['signin_code'::text, 'password_reset'::text, 'email_verify'::text, 'password_set'::text])))
);

CREATE TABLE public.browser_sessions (
    id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    started_by uuid NOT NULL,
    tester_url text NOT NULL,
    token text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    closed_at timestamp with time zone
);

CREATE TABLE public.chat_members (
    chat_id uuid NOT NULL,
    user_id uuid NOT NULL,
    last_read_id uuid,
    joined_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.comments (
    id uuid DEFAULT uuidv7() NOT NULL,
    issue_id uuid NOT NULL,
    author_id uuid,
    body jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone
);

CREATE TABLE public.cycles (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    project_id uuid NOT NULL,
    number integer NOT NULL,
    name text,
    starts_on date NOT NULL,
    ends_on date NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT cycles_check CHECK ((ends_on >= starts_on)),
    CONSTRAINT cycles_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 60)))
);

CREATE TABLE public.files (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid,
    uploader_id uuid,
    name text NOT NULL,
    content_type text NOT NULL,
    size bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.git_hooks (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    token text NOT NULL,
    secret text NOT NULL,
    open_status_id uuid,
    merge_status_id uuid,
    last_delivery_at timestamp with time zone,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT git_hooks_provider_check CHECK ((provider = ANY (ARRAY['github'::text, 'gitlab'::text])))
);

CREATE TABLE public.invite_sends (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.issue_doc_updates (
    id bigint NOT NULL,
    issue_id uuid NOT NULL,
    data bytea NOT NULL
);

ALTER TABLE public.issue_doc_updates ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.issue_doc_updates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE public.issue_events (
    id uuid DEFAULT uuidv7() NOT NULL,
    issue_id uuid NOT NULL,
    actor_id uuid,
    kind text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.issue_labels (
    issue_id uuid NOT NULL,
    label_id uuid NOT NULL
);

CREATE TABLE public.issue_links (
    blocker_id uuid NOT NULL,
    blocked_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_links_check CHECK ((blocker_id <> blocked_id))
);

CREATE TABLE public.issue_pull_requests (
    issue_id uuid NOT NULL,
    pr_id uuid NOT NULL,
    closes boolean NOT NULL
);

CREATE TABLE public.issue_viewers (
    conn_id text NOT NULL,
    issue_id uuid NOT NULL,
    user_id uuid NOT NULL,
    instance text NOT NULL,
    seen_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.issue_watchers (
    issue_id uuid NOT NULL,
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.issues (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    project_id uuid NOT NULL,
    number integer NOT NULL,
    title text NOT NULL,
    status_id uuid NOT NULL,
    assignee_id uuid,
    priority smallint DEFAULT 0 NOT NULL,
    due_date date,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    parent_id uuid,
    description jsonb,
    seed_claimed_at timestamp with time zone,
    description_text text DEFAULT ''::text NOT NULL,
    description_generation integer DEFAULT 0 NOT NULL,
    cycle_id uuid,
    CONSTRAINT issues_priority_check CHECK (((priority >= 0) AND (priority <= 4)))
);

CREATE TABLE public.labels (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    color text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT labels_color_check CHECK ((color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'::text))
);

CREATE TABLE public.message_reactions (
    message_id uuid NOT NULL,
    user_id uuid NOT NULL,
    emoji text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT message_reactions_emoji_check CHECK (((char_length(emoji) >= 1) AND (char_length(emoji) <= 16)))
);

CREATE TABLE public.messages (
    id uuid DEFAULT uuidv7() NOT NULL,
    chat_id uuid NOT NULL,
    parent_id uuid,
    author_id uuid,
    body jsonb,
    mentions uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    deleted_at timestamp with time zone,
    reply_count integer DEFAULT 0 NOT NULL,
    last_reply_at timestamp with time zone
);

CREATE TABLE public.notification_settings (
    user_id uuid NOT NULL,
    email text DEFAULT 'instant'::text NOT NULL,
    mentions boolean DEFAULT true NOT NULL,
    assignments boolean DEFAULT true NOT NULL,
    status boolean DEFAULT true NOT NULL,
    comments boolean DEFAULT true NOT NULL,
    last_digest_at timestamp with time zone,
    timezone text,
    chat boolean DEFAULT true NOT NULL,
    CONSTRAINT notification_settings_email_check CHECK ((email = ANY (ARRAY['instant'::text, 'digest'::text, 'off'::text])))
);

CREATE TABLE public.notifications (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    issue_id uuid,
    actor_id uuid,
    kind text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    read_at timestamp with time zone,
    emailed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    chat_id uuid,
    message_id uuid,
    CONSTRAINT notifications_kind_check CHECK ((kind = ANY (ARRAY['mention'::text, 'assigned'::text, 'status'::text, 'comment'::text, 'chat_mention'::text, 'message'::text]))),
    CONSTRAINT notifications_target_check CHECK (((issue_id IS NOT NULL) <> (chat_id IS NOT NULL)))
);

CREATE TABLE public.outbound_mail (
    id uuid DEFAULT uuidv7() NOT NULL,
    recipient text NOT NULL,
    subject text NOT NULL,
    body text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    last_error text,
    failed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.project_key_aliases (
    workspace_id uuid NOT NULL,
    key text NOT NULL,
    project_id uuid NOT NULL
);

CREATE TABLE public.project_members (
    project_id uuid NOT NULL,
    user_id uuid NOT NULL,
    added_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.projects (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    key text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    archived_at timestamp with time zone,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    issue_seq integer DEFAULT 0 NOT NULL,
    cycle_seq integer DEFAULT 0 NOT NULL,
    private boolean DEFAULT false NOT NULL
);

CREATE TABLE public.pull_requests (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    provider text NOT NULL,
    repo text NOT NULL,
    number integer NOT NULL,
    url text NOT NULL,
    title text NOT NULL,
    branch text NOT NULL,
    author text NOT NULL,
    state text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT pull_requests_state_check CHECK ((state = ANY (ARRAY['draft'::text, 'open'::text, 'merged'::text, 'closed'::text])))
);

CREATE UNLOGGED TABLE public.rate_limits (
    key text NOT NULL,
    tokens double precision NOT NULL,
    at timestamp with time zone NOT NULL,
    ok boolean NOT NULL
);

CREATE UNLOGGED TABLE public.realtime_conns (
    conn_id text NOT NULL,
    user_id uuid NOT NULL,
    instance text NOT NULL,
    seen_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.refresh_tokens (
    token_hash bytea CONSTRAINT sessions_token_hash_not_null NOT NULL,
    user_id uuid CONSTRAINT sessions_user_id_not_null NOT NULL,
    expires_at timestamp with time zone CONSTRAINT sessions_expires_at_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT now() CONSTRAINT sessions_created_at_not_null NOT NULL,
    family_id uuid NOT NULL,
    used_at timestamp with time zone,
    remember boolean DEFAULT true NOT NULL
);

CREATE TABLE public.status_types (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    icon text NOT NULL,
    color text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT status_types_category_check CHECK ((category = ANY (ARRAY['backlog'::text, 'unstarted'::text, 'started'::text, 'completed'::text, 'canceled'::text]))),
    CONSTRAINT status_types_color_check CHECK ((color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'::text)),
    CONSTRAINT status_types_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 30)))
);

CREATE TABLE public.system_config (
    key text NOT NULL,
    value text DEFAULT ''::text NOT NULL,
    secret boolean DEFAULT false NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.test_accounts (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    label text NOT NULL,
    username text NOT NULL,
    password text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    site_id uuid
);

CREATE TABLE public.test_cases (
    id uuid DEFAULT uuidv7() NOT NULL,
    issue_id uuid NOT NULL,
    "position" integer NOT NULL,
    title text NOT NULL,
    steps text[] NOT NULL,
    expected text NOT NULL,
    source text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    script jsonb,
    playwright text,
    CONSTRAINT test_cases_playwright_check CHECK ((char_length(playwright) <= 20000)),
    CONSTRAINT test_cases_source_check CHECK ((source = ANY (ARRAY['ai'::text, 'manual'::text])))
);

CREATE TABLE public.test_results (
    id uuid DEFAULT uuidv7() NOT NULL,
    run_id uuid NOT NULL,
    case_id uuid NOT NULL,
    "position" integer NOT NULL,
    title text NOT NULL,
    steps text[] NOT NULL,
    expected text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    summary text,
    reproduction text[],
    marked_by uuid,
    marked_at timestamp with time zone
);

CREATE TABLE public.test_runs (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    kind text NOT NULL,
    regenerate boolean DEFAULT false NOT NULL,
    started_by uuid,
    status text DEFAULT 'queued'::text NOT NULL,
    error text,
    cancel_requested boolean DEFAULT false NOT NULL,
    proxy_token text,
    claimed_at timestamp with time zone,
    finished_at timestamp with time zone,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    cache_read_tokens bigint DEFAULT 0 NOT NULL,
    cache_write_tokens bigint DEFAULT 0 NOT NULL,
    cost_micros bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    tester_url text,
    case_id uuid,
    CONSTRAINT test_runs_kind_check CHECK ((kind = ANY (ARRAY['ai'::text, 'manual'::text, 'replay'::text])))
);

CREATE TABLE public.test_sites (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    base_url text NOT NULL,
    host text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.test_steps (
    id uuid DEFAULT uuidv7() NOT NULL,
    result_id uuid NOT NULL,
    "position" integer NOT NULL,
    action text NOT NULL,
    url text,
    screenshot_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.users (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    password_hash text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    google_sub text,
    avatar_file_id uuid,
    platform_admin boolean DEFAULT false NOT NULL,
    suspended_at timestamp with time zone,
    email_verified_at timestamp with time zone
);

CREATE TABLE public.workspace_invites (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    email text NOT NULL,
    token_hash bytea NOT NULL,
    invited_by uuid,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    role_id uuid
);

CREATE TABLE public.workspace_members (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    last_opened_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    role_id uuid NOT NULL
);

CREATE TABLE public.workspace_roles (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    builtin text,
    permissions text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspace_roles_builtin_check CHECK ((builtin = ANY (ARRAY['admin'::text, 'member'::text])))
);

CREATE TABLE public.workspaces (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.admin_audit
    ADD CONSTRAINT admin_audit_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.admin_mfa
    ADD CONSTRAINT admin_mfa_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY public.admin_sessions
    ADD CONSTRAINT admin_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.api_tokens
    ADD CONSTRAINT api_tokens_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.api_tokens
    ADD CONSTRAINT api_tokens_token_hash_key UNIQUE (token_hash);

ALTER TABLE ONLY public.auth_tokens
    ADD CONSTRAINT auth_tokens_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.browser_sessions
    ADD CONSTRAINT browser_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.chat_members
    ADD CONSTRAINT chat_members_pkey PRIMARY KEY (chat_id, user_id);

ALTER TABLE ONLY public.chats
    ADD CONSTRAINT chats_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.comments
    ADD CONSTRAINT comments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.cycles
    ADD CONSTRAINT cycles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.cycles
    ADD CONSTRAINT cycles_project_id_number_key UNIQUE (project_id, number);

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.git_hooks
    ADD CONSTRAINT git_hooks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.git_hooks
    ADD CONSTRAINT git_hooks_token_key UNIQUE (token);

ALTER TABLE ONLY public.git_hooks
    ADD CONSTRAINT git_hooks_workspace_id_provider_key UNIQUE (workspace_id, provider);

ALTER TABLE ONLY public.issue_doc_updates
    ADD CONSTRAINT issue_doc_updates_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.issue_events
    ADD CONSTRAINT issue_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.issue_labels
    ADD CONSTRAINT issue_labels_pkey PRIMARY KEY (issue_id, label_id);

ALTER TABLE ONLY public.issue_links
    ADD CONSTRAINT issue_links_pkey PRIMARY KEY (blocker_id, blocked_id);

ALTER TABLE ONLY public.issue_pull_requests
    ADD CONSTRAINT issue_pull_requests_pkey PRIMARY KEY (issue_id, pr_id);

ALTER TABLE ONLY public.issue_viewers
    ADD CONSTRAINT issue_viewers_pkey PRIMARY KEY (conn_id, issue_id);

ALTER TABLE ONLY public.issue_watchers
    ADD CONSTRAINT issue_watchers_pkey PRIMARY KEY (issue_id, user_id);

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_project_id_number_key UNIQUE (project_id, number);

ALTER TABLE ONLY public.labels
    ADD CONSTRAINT labels_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.message_reactions
    ADD CONSTRAINT message_reactions_pkey PRIMARY KEY (message_id, user_id, emoji);

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.notification_settings
    ADD CONSTRAINT notification_settings_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.outbound_mail
    ADD CONSTRAINT outbound_mail_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.project_key_aliases
    ADD CONSTRAINT project_key_aliases_pkey PRIMARY KEY (workspace_id, key);

ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT project_members_pkey PRIMARY KEY (project_id, user_id);

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_workspace_id_key_key UNIQUE (workspace_id, key);

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_workspace_id_provider_repo_number_key UNIQUE (workspace_id, provider, repo, number);

ALTER TABLE ONLY public.rate_limits
    ADD CONSTRAINT rate_limits_pkey PRIMARY KEY (key);

ALTER TABLE ONLY public.realtime_conns
    ADD CONSTRAINT realtime_conns_pkey PRIMARY KEY (conn_id);

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (token_hash);

ALTER TABLE ONLY public.status_types
    ADD CONSTRAINT status_types_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.statuses
    ADD CONSTRAINT statuses_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.system_config
    ADD CONSTRAINT system_config_pkey PRIMARY KEY (key);

ALTER TABLE ONLY public.test_accounts
    ADD CONSTRAINT test_accounts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.test_accounts
    ADD CONSTRAINT test_accounts_workspace_id_label_key UNIQUE (workspace_id, label);

ALTER TABLE ONLY public.test_cases
    ADD CONSTRAINT test_cases_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.test_results
    ADD CONSTRAINT test_results_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.test_results
    ADD CONSTRAINT test_results_run_id_case_id_key UNIQUE (run_id, case_id);

ALTER TABLE ONLY public.test_runs
    ADD CONSTRAINT test_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.test_sites
    ADD CONSTRAINT test_sites_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.test_sites
    ADD CONSTRAINT test_sites_workspace_id_host_key UNIQUE (workspace_id, host);

ALTER TABLE ONLY public.test_steps
    ADD CONSTRAINT test_steps_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_google_sub_key UNIQUE (google_sub);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_token_hash_key UNIQUE (token_hash);

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_workspace_id_email_key UNIQUE (workspace_id, email);

ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT workspace_members_pkey PRIMARY KEY (workspace_id, user_id);

ALTER TABLE ONLY public.workspace_roles
    ADD CONSTRAINT workspace_roles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_roles
    ADD CONSTRAINT workspace_roles_workspace_id_builtin_key UNIQUE (workspace_id, builtin);

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_slug_key UNIQUE (slug);

CREATE INDEX admin_sessions_expires_at_idx ON public.admin_sessions USING btree (expires_at);

CREATE INDEX admin_sessions_user_id_idx ON public.admin_sessions USING btree (user_id);

CREATE INDEX ai_usage_run_id_idx ON public.ai_usage USING btree (run_id);

CREATE INDEX ai_usage_workspace_id_created_at_idx ON public.ai_usage USING btree (workspace_id, created_at);

CREATE INDEX api_tokens_user_id_idx ON public.api_tokens USING btree (user_id);

CREATE INDEX auth_failures_at_idx ON public.auth_failures USING btree (at);

CREATE INDEX auth_failures_key_at_idx ON public.auth_failures USING btree (key, at);

CREATE INDEX auth_tokens_token_hash_idx ON public.auth_tokens USING btree (token_hash);

CREATE INDEX auth_tokens_user_id_purpose_idx ON public.auth_tokens USING btree (user_id, purpose) WHERE (used_at IS NULL);

CREATE INDEX browser_sessions_started_by_idx ON public.browser_sessions USING btree (started_by) WHERE (closed_at IS NULL);

CREATE INDEX chat_members_user_id_idx ON public.chat_members USING btree (user_id);

CREATE INDEX chats_created_by_idx ON public.chats USING btree (created_by);

CREATE UNIQUE INDEX chats_workspace_id_idx ON public.chats USING btree (workspace_id) WHERE (kind = 'workspace'::text);

CREATE UNIQUE INDEX chats_workspace_id_pair_idx ON public.chats USING btree (workspace_id, pair) WHERE (kind = ANY (ARRAY['direct'::text, 'self'::text]));

CREATE INDEX comments_author_id_idx ON public.comments USING btree (author_id);

CREATE INDEX comments_created_at_idx ON public.comments USING btree (created_at);

CREATE INDEX comments_issue_id_id_idx ON public.comments USING btree (issue_id, id);

CREATE INDEX cycles_project_id_starts_on_idx ON public.cycles USING btree (project_id, starts_on);

CREATE INDEX files_uploader_id_idx ON public.files USING btree (uploader_id);

CREATE INDEX files_workspace_id_idx ON public.files USING btree (workspace_id);

CREATE INDEX invite_sends_at_idx ON public.invite_sends USING btree (at);

CREATE INDEX invite_sends_user_id_at_idx ON public.invite_sends USING btree (user_id, at);

CREATE INDEX invite_sends_workspace_id_at_idx ON public.invite_sends USING btree (workspace_id, at);

CREATE INDEX issue_doc_updates_issue_id_id_idx ON public.issue_doc_updates USING btree (issue_id, id);

CREATE INDEX issue_events_activity_idx ON public.issue_events USING btree (created_at) WHERE (kind <> 'description'::text);

CREATE INDEX issue_events_actor_id_idx ON public.issue_events USING btree (actor_id);

CREATE INDEX issue_events_issue_id_id_idx ON public.issue_events USING btree (issue_id, id);

CREATE INDEX issue_labels_label_id_idx ON public.issue_labels USING btree (label_id);

CREATE INDEX issue_links_blocked_id_idx ON public.issue_links USING btree (blocked_id);

CREATE INDEX issue_pull_requests_pr_id_idx ON public.issue_pull_requests USING btree (pr_id);

CREATE INDEX issue_viewers_instance_idx ON public.issue_viewers USING btree (instance);

CREATE INDEX issue_viewers_issue_id_idx ON public.issue_viewers USING btree (issue_id);

CREATE INDEX issue_viewers_user_id_idx ON public.issue_viewers USING btree (user_id);

CREATE INDEX issue_watchers_user_id_idx ON public.issue_watchers USING btree (user_id);

CREATE INDEX issues_created_at_idx ON public.issues USING btree (created_at);

CREATE INDEX issues_created_by_idx ON public.issues USING btree (created_by);

CREATE INDEX issues_cycle_id_idx ON public.issues USING btree (cycle_id);

CREATE INDEX issues_description_text_idx ON public.issues USING gin (description_text public.gin_trgm_ops);

CREATE INDEX issues_parent_id_idx ON public.issues USING btree (parent_id);

CREATE INDEX issues_status_id_idx ON public.issues USING btree (status_id);

CREATE INDEX issues_title_idx ON public.issues USING gin (title public.gin_trgm_ops);

CREATE INDEX issues_workspace_id_assignee_id_idx ON public.issues USING btree (workspace_id, assignee_id);

CREATE UNIQUE INDEX labels_workspace_id_lower_idx ON public.labels USING btree (workspace_id, lower(name));

CREATE INDEX message_reactions_user_id_idx ON public.message_reactions USING btree (user_id);

CREATE INDEX messages_author_id_idx ON public.messages USING btree (author_id);

CREATE INDEX messages_chat_id_id_idx ON public.messages USING btree (chat_id, id);

CREATE INDEX messages_parent_id_id_idx ON public.messages USING btree (parent_id, id) WHERE (parent_id IS NOT NULL);

CREATE INDEX notifications_actor_id_idx ON public.notifications USING btree (actor_id);

CREATE INDEX notifications_chat_id_idx ON public.notifications USING btree (chat_id);

CREATE INDEX notifications_created_at_idx ON public.notifications USING btree (created_at) WHERE (emailed_at IS NULL);

CREATE INDEX notifications_issue_id_idx ON public.notifications USING btree (issue_id);

CREATE INDEX notifications_message_id_idx ON public.notifications USING btree (message_id);

CREATE INDEX notifications_user_id_workspace_id_id_idx ON public.notifications USING btree (user_id, workspace_id, id DESC);

CREATE INDEX notifications_workspace_id_idx ON public.notifications USING btree (workspace_id);

CREATE INDEX outbound_mail_next_attempt_at_idx ON public.outbound_mail USING btree (next_attempt_at) WHERE (failed_at IS NULL);

CREATE INDEX project_key_aliases_project_id_idx ON public.project_key_aliases USING btree (project_id);

CREATE INDEX project_members_user_id_idx ON public.project_members USING btree (user_id);

CREATE INDEX projects_created_by_idx ON public.projects USING btree (created_by);

CREATE INDEX projects_name_idx ON public.projects USING gin (name public.gin_trgm_ops);

CREATE INDEX rate_limits_at_idx ON public.rate_limits USING btree (at);

CREATE INDEX realtime_conns_instance_idx ON public.realtime_conns USING btree (instance);

CREATE INDEX realtime_conns_user_id_idx ON public.realtime_conns USING btree (user_id);

CREATE INDEX refresh_tokens_created_at_idx ON public.refresh_tokens USING btree (created_at);

CREATE INDEX refresh_tokens_family_id_idx ON public.refresh_tokens USING btree (family_id);

CREATE INDEX sessions_user_id_idx ON public.refresh_tokens USING btree (user_id);

CREATE UNIQUE INDEX status_types_workspace_id_lower_idx ON public.status_types USING btree (workspace_id, lower(name));

CREATE INDEX statuses_type_id_idx ON public.statuses USING btree (type_id);

CREATE UNIQUE INDEX statuses_workspace_id_lower_idx ON public.statuses USING btree (workspace_id, lower(name));

CREATE INDEX test_accounts_site_id_idx ON public.test_accounts USING btree (site_id);

CREATE INDEX test_cases_created_by_idx ON public.test_cases USING btree (created_by);

CREATE INDEX test_cases_issue_id_position_idx ON public.test_cases USING btree (issue_id, "position") WHERE (deleted_at IS NULL);

CREATE INDEX test_results_case_id_marked_at_idx ON public.test_results USING btree (case_id, marked_at DESC);

CREATE INDEX test_results_marked_by_idx ON public.test_results USING btree (marked_by);

CREATE INDEX test_runs_issue_id_created_at_idx ON public.test_runs USING btree (issue_id, created_at DESC);

CREATE INDEX test_runs_started_by_idx ON public.test_runs USING btree (started_by);

CREATE INDEX test_runs_status_created_at_idx ON public.test_runs USING btree (status, created_at) WHERE (status = 'queued'::text);

CREATE INDEX test_runs_workspace_id_idx ON public.test_runs USING btree (workspace_id) WHERE (status = ANY (ARRAY['generating'::text, 'running'::text]));

CREATE INDEX test_steps_result_id_position_idx ON public.test_steps USING btree (result_id, "position");

CREATE INDEX test_steps_screenshot_id_idx ON public.test_steps USING btree (screenshot_id);

CREATE INDEX users_avatar_file_id_idx ON public.users USING btree (avatar_file_id);

CREATE INDEX users_created_at_idx ON public.users USING btree (created_at);

CREATE INDEX workspace_invites_invited_by_idx ON public.workspace_invites USING btree (invited_by);

CREATE INDEX workspace_invites_role_id_idx ON public.workspace_invites USING btree (role_id);

CREATE INDEX workspace_members_role_id_idx ON public.workspace_members USING btree (role_id);

CREATE INDEX workspace_members_user_id_last_opened_at_idx ON public.workspace_members USING btree (user_id, last_opened_at DESC);

CREATE UNIQUE INDEX workspace_roles_workspace_id_lower_idx ON public.workspace_roles USING btree (workspace_id, lower(name));

CREATE INDEX workspaces_created_by_idx ON public.workspaces USING btree (created_by);

CREATE TRIGGER admin_audit_no_change BEFORE DELETE OR UPDATE ON public.admin_audit FOR EACH ROW EXECUTE FUNCTION public.admin_audit_append_only();

CREATE TRIGGER admin_audit_no_truncate BEFORE TRUNCATE ON public.admin_audit FOR EACH STATEMENT EXECUTE FUNCTION public.admin_audit_append_only();

CREATE TRIGGER project_key_alias AFTER INSERT OR UPDATE OF key ON public.projects FOR EACH ROW EXECUTE FUNCTION public.project_key_alias();

CREATE TRIGGER seed_statuses AFTER INSERT ON public.workspaces FOR EACH ROW EXECUTE FUNCTION public.seed_statuses();

ALTER TABLE ONLY public.admin_mfa
    ADD CONSTRAINT admin_mfa_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.admin_sessions
    ADD CONSTRAINT admin_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.ai_usage
    ADD CONSTRAINT ai_usage_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.test_runs(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.ai_usage
    ADD CONSTRAINT ai_usage_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.api_tokens
    ADD CONSTRAINT api_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.auth_tokens
    ADD CONSTRAINT auth_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.browser_sessions
    ADD CONSTRAINT browser_sessions_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.browser_sessions
    ADD CONSTRAINT browser_sessions_started_by_fkey FOREIGN KEY (started_by) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.browser_sessions
    ADD CONSTRAINT browser_sessions_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.chat_members
    ADD CONSTRAINT chat_members_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.chat_members
    ADD CONSTRAINT chat_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.chats
    ADD CONSTRAINT chats_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.chats
    ADD CONSTRAINT chats_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.comments
    ADD CONSTRAINT comments_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.comments
    ADD CONSTRAINT comments_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.cycles
    ADD CONSTRAINT cycles_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.cycles
    ADD CONSTRAINT cycles_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.cycles
    ADD CONSTRAINT cycles_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_uploader_id_fkey FOREIGN KEY (uploader_id) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.git_hooks
    ADD CONSTRAINT git_hooks_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.git_hooks
    ADD CONSTRAINT git_hooks_merge_status_id_fkey FOREIGN KEY (merge_status_id) REFERENCES public.statuses(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.git_hooks
    ADD CONSTRAINT git_hooks_open_status_id_fkey FOREIGN KEY (open_status_id) REFERENCES public.statuses(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.git_hooks
    ADD CONSTRAINT git_hooks_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.invite_sends
    ADD CONSTRAINT invite_sends_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.invite_sends
    ADD CONSTRAINT invite_sends_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_doc_updates
    ADD CONSTRAINT issue_doc_updates_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_events
    ADD CONSTRAINT issue_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.issue_events
    ADD CONSTRAINT issue_events_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_labels
    ADD CONSTRAINT issue_labels_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_labels
    ADD CONSTRAINT issue_labels_label_id_fkey FOREIGN KEY (label_id) REFERENCES public.labels(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_links
    ADD CONSTRAINT issue_links_blocked_id_fkey FOREIGN KEY (blocked_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_links
    ADD CONSTRAINT issue_links_blocker_id_fkey FOREIGN KEY (blocker_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_pull_requests
    ADD CONSTRAINT issue_pull_requests_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_pull_requests
    ADD CONSTRAINT issue_pull_requests_pr_id_fkey FOREIGN KEY (pr_id) REFERENCES public.pull_requests(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_viewers
    ADD CONSTRAINT issue_viewers_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_viewers
    ADD CONSTRAINT issue_viewers_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_watchers
    ADD CONSTRAINT issue_watchers_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issue_watchers
    ADD CONSTRAINT issue_watchers_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_cycle_id_fkey FOREIGN KEY (cycle_id) REFERENCES public.cycles(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.issues(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_status_id_fkey FOREIGN KEY (status_id) REFERENCES public.statuses(id);

ALTER TABLE ONLY public.issues
    ADD CONSTRAINT issues_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.labels
    ADD CONSTRAINT labels_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.message_reactions
    ADD CONSTRAINT message_reactions_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.messages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.message_reactions
    ADD CONSTRAINT message_reactions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.messages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.notification_settings
    ADD CONSTRAINT notification_settings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.messages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.notifications
    ADD CONSTRAINT notifications_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.project_key_aliases
    ADD CONSTRAINT project_key_aliases_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.project_key_aliases
    ADD CONSTRAINT project_key_aliases_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT project_members_added_by_fkey FOREIGN KEY (added_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT project_members_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.project_members
    ADD CONSTRAINT project_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.pull_requests
    ADD CONSTRAINT pull_requests_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.realtime_conns
    ADD CONSTRAINT realtime_conns_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.status_types
    ADD CONSTRAINT status_types_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.statuses
    ADD CONSTRAINT statuses_type_id_fkey FOREIGN KEY (type_id) REFERENCES public.status_types(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.statuses
    ADD CONSTRAINT statuses_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_accounts
    ADD CONSTRAINT test_accounts_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.test_sites(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_accounts
    ADD CONSTRAINT test_accounts_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_cases
    ADD CONSTRAINT test_cases_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.test_cases
    ADD CONSTRAINT test_cases_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_results
    ADD CONSTRAINT test_results_case_id_fkey FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_results
    ADD CONSTRAINT test_results_marked_by_fkey FOREIGN KEY (marked_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.test_results
    ADD CONSTRAINT test_results_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.test_runs(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_runs
    ADD CONSTRAINT test_runs_case_id_fkey FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.test_runs
    ADD CONSTRAINT test_runs_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_runs
    ADD CONSTRAINT test_runs_started_by_fkey FOREIGN KEY (started_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.test_runs
    ADD CONSTRAINT test_runs_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_sites
    ADD CONSTRAINT test_sites_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_steps
    ADD CONSTRAINT test_steps_result_id_fkey FOREIGN KEY (result_id) REFERENCES public.test_results(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.test_steps
    ADD CONSTRAINT test_steps_screenshot_id_fkey FOREIGN KEY (screenshot_id) REFERENCES public.files(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_avatar_file_id_fkey FOREIGN KEY (avatar_file_id) REFERENCES public.files(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.workspace_roles(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.workspace_invites
    ADD CONSTRAINT workspace_invites_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT workspace_members_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.workspace_roles(id);

ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT workspace_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT workspace_members_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspace_roles
    ADD CONSTRAINT workspace_roles_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

-- Seed config
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_host', 'localhost', false, 'SMTP server host; empty disables email', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_port', '1025', false, 'SMTP server port', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_username', '', false, 'SMTP username; empty skips auth', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_password', '', true, 'SMTP password', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_from', 'Tether <no-reply@tether.local>', false, 'From header for outgoing email', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('app_base_url', 'http://localhost:5173', false, 'Public URL of the user app, used in email links', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('otp_ttl_minutes', '10', false, 'Sign-in code lifetime in minutes', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('reset_ttl_minutes', '30', false, 'Password reset link lifetime in minutes', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('access_ttl_minutes', '15', false, 'JWT access token lifetime in minutes', '2026-11-02 16:11:47.960409+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('jwt_secret', '', true, 'HMAC key that signs access tokens; generated on first start if empty, changing it signs everyone out of their access tokens', '2026-11-02 16:11:47.960409+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('refresh_ttl_hours', '720', false, 'Sign-in lifetime in hours with "Remember me", counted from sign-in', '2026-11-02 16:11:47.947713+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('short_refresh_ttl_hours', '24', false, 'Sign-in lifetime in hours without "Remember me"; the cookies also end when the browser closes', '2026-11-02 16:11:47.964194+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('google_client_id', '', false, 'Google OAuth client ID; empty disables "Continue with Google". Authorized redirect URI: {app_base_url}/api/auth/google/callback', '2026-11-02 16:11:47.967179+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('google_client_secret', '', true, 'Google OAuth client secret', '2026-11-02 16:11:47.967179+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('invite_ttl_days', '7', false, 'Workspace invite link lifetime in days', '2026-11-02 16:11:47.979948+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('upload_max_mb', '10', false, 'Largest attachment upload in MB', '2026-11-02 16:11:48.046044+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('invite_daily_user_limit', '50', false, 'Invite emails one person can send per day', '2026-11-02 16:11:48.110667+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('invite_daily_workspace_limit', '200', false, 'Invite emails one workspace can send per day', '2026-11-02 16:11:48.110667+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('upload_quota_mb', '5120', false, 'Total attachment storage per workspace in MB', '2026-11-02 16:11:48.110667+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('digest_hour_utc', '8', false, 'Hour (0-23) daily notification digests are sent, in each user''s timezone', '2026-11-02 16:11:48.067809+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('ai_test_monthly_limit_usd', '50', false, 'AI test spend per workspace per month, in USD', '2026-11-02 16:11:48.314753+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('ai_test_workspace_concurrency', '2', false, 'AI test runs one workspace can have running at once', '2026-11-02 16:11:48.314753+00');
INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('ai_tester_heartbeat', '', false, 'The URL of the tester that last checked in, and when (updated_at; every 30s, AI runs need one within 90s)', '2026-11-02 16:11:48.314753+00');
