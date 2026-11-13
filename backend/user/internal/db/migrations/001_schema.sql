-- The whole schema and seed data as of 048_feedback_url, squashed from migrations 001_init-048 (pg_dump of a fresh
-- database). Databases that already ran 048 record this as applied (db.go); new changes go in 002_*.sql and on.
set local check_function_bodies = false;

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE FUNCTION public.admin_audit_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  raise exception 'admin_audit is append-only';
end $$;

CREATE FUNCTION public.can_create_workspace(uid uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  select not exists (select 1 from workspace_members where user_id = uid)
      or exists (select 1 from workspace_members m join workspace_roles r on r.id = m.role_id
                 where m.user_id = uid and (r.builtin = 'admin' or 'workspaces.create' = any(r.permissions)))
$$;

CREATE FUNCTION public.can_see_project(project uuid, viewer uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  select case when is_guest(p.workspace_id, viewer)
           then exists (select 1 from project_members m where m.project_id = p.id and m.user_id = viewer)
           else not p.private
             or exists (select 1 from project_members m where m.project_id = p.id and m.user_id = viewer)
             or exists (select 1 from workspace_members wm join workspace_roles r on r.id = wm.role_id
                        where wm.workspace_id = p.workspace_id and wm.user_id = viewer and r.builtin = 'admin')
         end
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

CREATE TABLE public.users (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    password_hash text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    google_sub text,
    avatar_file_id uuid,
    suspended_at timestamp with time zone,
    email_verified_at timestamp with time zone,
    plan text DEFAULT 'free'::text NOT NULL,
    plan_comped boolean DEFAULT false NOT NULL,
    terms_accepted_at timestamp with time zone,
    plan_comped_until timestamp with time zone,
    plan_comp_reminded_at timestamp with time zone,
    CONSTRAINT users_plan_check CHECK ((plan = ANY (ARRAY['free'::text, 'starter'::text, 'pro'::text, 'advanced'::text]))),
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_google_sub_key UNIQUE (google_sub),
    CONSTRAINT users_pkey PRIMARY KEY (id)
);

CREATE TABLE public.workspaces (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    feedback_token text,
    onboarding_dismissed_at timestamp with time zone,
    require_mfa boolean DEFAULT false NOT NULL,
    CONSTRAINT workspaces_feedback_token_key UNIQUE (feedback_token),
    CONSTRAINT workspaces_pkey PRIMARY KEY (id),
    CONSTRAINT workspaces_slug_key UNIQUE (slug),
    CONSTRAINT workspaces_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL
);

CREATE TABLE public.chats (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    kind text NOT NULL,
    name text,
    pair text,
    created_by uuid,
    last_message_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chats_kind_check CHECK ((kind = ANY (ARRAY['workspace'::text, 'self'::text, 'direct'::text, 'group'::text]))),
    CONSTRAINT chats_pkey PRIMARY KEY (id),
    CONSTRAINT chats_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT chats_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
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
    CONSTRAINT status_types_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 30))),
    CONSTRAINT status_types_pkey PRIMARY KEY (id),
    CONSTRAINT status_types_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.statuses (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    "position" integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    type_id uuid,
    CONSTRAINT statuses_category_check CHECK ((category = ANY (ARRAY['backlog'::text, 'unstarted'::text, 'started'::text, 'completed'::text, 'canceled'::text]))),
    CONSTRAINT statuses_pkey PRIMARY KEY (id),
    CONSTRAINT statuses_type_id_fkey FOREIGN KEY (type_id) REFERENCES public.status_types(id) ON DELETE SET NULL,
    CONSTRAINT statuses_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.account_events (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    action text NOT NULL,
    ip inet,
    user_agent text DEFAULT ''::text NOT NULL,
    detail text DEFAULT ''::text NOT NULL,
    at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT account_events_action_check CHECK ((action = ANY (ARRAY['sign_in'::text, 'sign_in_failed'::text, 'sign_out'::text, 'password_changed'::text, 'password_added'::text, 'email_changed'::text, 'mfa_enabled'::text, 'mfa_disabled'::text, 'token_created'::text, 'token_deleted'::text, 'reauth'::text, 'reauth_failed'::text]))),
    CONSTRAINT account_events_pkey PRIMARY KEY (id),
    CONSTRAINT account_events_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.admin_audit (
    id uuid DEFAULT uuidv7() NOT NULL,
    at timestamp with time zone DEFAULT now() NOT NULL,
    actor_id uuid,
    actor_email text DEFAULT ''::text NOT NULL,
    action text NOT NULL,
    target text DEFAULT ''::text NOT NULL,
    ip text DEFAULT ''::text NOT NULL,
    CONSTRAINT admin_audit_pkey PRIMARY KEY (id)
);

CREATE TABLE public.admins (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT admins_email_key UNIQUE (email),
    CONSTRAINT admins_pkey PRIMARY KEY (id)
);

CREATE TABLE public.admin_mfa (
    admin_id uuid CONSTRAINT admin_mfa_user_id_not_null NOT NULL,
    secret text NOT NULL,
    enabled_at timestamp with time zone,
    last_step bigint DEFAULT 0 NOT NULL,
    recovery_codes text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT admin_mfa_pkey PRIMARY KEY (admin_id),
    CONSTRAINT admin_mfa_admin_id_fkey FOREIGN KEY (admin_id) REFERENCES public.admins(id) ON DELETE CASCADE
);

CREATE TABLE public.admin_sessions (
    id uuid DEFAULT uuidv7() NOT NULL,
    admin_id uuid CONSTRAINT admin_sessions_user_id_not_null NOT NULL,
    mfa_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT admin_sessions_pkey PRIMARY KEY (id),
    CONSTRAINT admin_sessions_admin_id_fkey FOREIGN KEY (admin_id) REFERENCES public.admins(id) ON DELETE CASCADE
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
    private boolean DEFAULT false NOT NULL,
    start_date date,
    target_date date,
    manual_order boolean DEFAULT false NOT NULL,
    CONSTRAINT projects_dates CHECK (((start_date IS NULL) OR (target_date IS NULL) OR (start_date <= target_date))),
    CONSTRAINT projects_pkey PRIMARY KEY (id),
    CONSTRAINT projects_workspace_id_key_key UNIQUE (workspace_id, key),
    CONSTRAINT projects_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT projects_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
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
    CONSTRAINT cycles_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 60))),
    CONSTRAINT cycles_pkey PRIMARY KEY (id),
    CONSTRAINT cycles_project_id_number_key UNIQUE (project_id, number),
    CONSTRAINT cycles_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT cycles_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT cycles_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.issues_all (
    id uuid DEFAULT uuidv7() CONSTRAINT issues_id_not_null NOT NULL,
    workspace_id uuid CONSTRAINT issues_workspace_id_not_null NOT NULL,
    project_id uuid CONSTRAINT issues_project_id_not_null NOT NULL,
    number integer CONSTRAINT issues_number_not_null NOT NULL,
    title text CONSTRAINT issues_title_not_null NOT NULL,
    status_id uuid CONSTRAINT issues_status_id_not_null NOT NULL,
    assignee_id uuid,
    priority smallint DEFAULT 0 CONSTRAINT issues_priority_not_null NOT NULL,
    due_date date,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() CONSTRAINT issues_created_at_not_null NOT NULL,
    updated_at timestamp with time zone DEFAULT now() CONSTRAINT issues_updated_at_not_null NOT NULL,
    parent_id uuid,
    description jsonb,
    seed_claimed_at timestamp with time zone,
    description_text text DEFAULT ''::text CONSTRAINT issues_description_text_not_null NOT NULL,
    description_generation integer DEFAULT 0 CONSTRAINT issues_description_generation_not_null NOT NULL,
    cycle_id uuid,
    rank text CONSTRAINT issues_rank_not_null NOT NULL COLLATE pg_catalog."C",
    deleted_at timestamp with time zone,
    deleted_by uuid,
    estimate smallint,
    CONSTRAINT issues_all_estimate_check CHECK ((estimate = ANY (ARRAY[1, 2, 3, 5, 8]))),
    CONSTRAINT issues_priority_check CHECK (((priority >= 0) AND (priority <= 4))),
    CONSTRAINT issues_pkey PRIMARY KEY (id),
    CONSTRAINT issues_project_id_number_key UNIQUE (project_id, number),
    CONSTRAINT issues_project_id_rank_key UNIQUE (project_id, rank) DEFERRABLE,
    CONSTRAINT issues_all_deleted_by_fkey FOREIGN KEY (deleted_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT issues_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT issues_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT issues_cycle_id_fkey FOREIGN KEY (cycle_id) REFERENCES public.cycles(id) ON DELETE SET NULL,
    CONSTRAINT issues_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.issues_all(id) ON DELETE SET NULL,
    CONSTRAINT issues_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT issues_status_id_fkey FOREIGN KEY (status_id) REFERENCES public.statuses(id),
    CONSTRAINT issues_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
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
    CONSTRAINT test_cases_source_check CHECK ((source = ANY (ARRAY['ai'::text, 'manual'::text]))),
    CONSTRAINT test_cases_pkey PRIMARY KEY (id),
    CONSTRAINT test_cases_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT test_cases_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE
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
    client text,
    CONSTRAINT test_runs_client_check CHECK (((char_length(client) >= 1) AND (char_length(client) <= 40))),
    CONSTRAINT test_runs_kind_check CHECK ((kind = ANY (ARRAY['ai'::text, 'manual'::text, 'replay'::text, 'local'::text]))),
    CONSTRAINT test_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'generating'::text, 'running'::text, 'passed'::text, 'failed'::text, 'error'::text, 'cancelled'::text, 'limit_reached'::text]))),
    CONSTRAINT test_runs_pkey PRIMARY KEY (id),
    CONSTRAINT test_runs_case_id_fkey FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE SET NULL,
    CONSTRAINT test_runs_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT test_runs_started_by_fkey FOREIGN KEY (started_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT test_runs_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.ai_usage (
    workspace_id uuid NOT NULL,
    run_id uuid,
    cost_micros bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_usage_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.test_runs(id) ON DELETE SET NULL,
    CONSTRAINT ai_usage_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.api_tokens (
    id uuid DEFAULT uuidv7() NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    token_hash bytea NOT NULL,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT api_tokens_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 60))),
    CONSTRAINT api_tokens_pkey PRIMARY KEY (id),
    CONSTRAINT api_tokens_token_hash_key UNIQUE (token_hash),
    CONSTRAINT api_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
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
    new_email text,
    CONSTRAINT auth_tokens_purpose_check CHECK ((purpose = ANY (ARRAY['signin_code'::text, 'password_reset'::text, 'email_verify'::text, 'password_set'::text, 'email_change_old'::text, 'email_change_new'::text, 'account_delete'::text, 'reauth'::text]))),
    CONSTRAINT auth_tokens_pkey PRIMARY KEY (id),
    CONSTRAINT auth_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.browser_sessions (
    id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    started_by uuid NOT NULL,
    tester_url text NOT NULL,
    token text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    closed_at timestamp with time zone,
    CONSTRAINT browser_sessions_pkey PRIMARY KEY (id),
    CONSTRAINT browser_sessions_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT browser_sessions_started_by_fkey FOREIGN KEY (started_by) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT browser_sessions_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.chat_members (
    chat_id uuid NOT NULL,
    user_id uuid NOT NULL,
    last_read_id uuid,
    joined_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chat_members_pkey PRIMARY KEY (chat_id, user_id),
    CONSTRAINT chat_members_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE,
    CONSTRAINT chat_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.comments (
    id uuid DEFAULT uuidv7() NOT NULL,
    issue_id uuid NOT NULL,
    author_id uuid,
    body jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    CONSTRAINT comments_pkey PRIMARY KEY (id),
    CONSTRAINT comments_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT comments_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE
);

CREATE TABLE public.cycle_points (
    cycle_id uuid NOT NULL,
    day date NOT NULL,
    scope integer NOT NULL,
    done integer NOT NULL,
    CONSTRAINT cycle_points_pkey PRIMARY KEY (cycle_id, day),
    CONSTRAINT cycle_points_cycle_id_fkey FOREIGN KEY (cycle_id) REFERENCES public.cycles(id) ON DELETE CASCADE
);

CREATE TABLE public.default_labels (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    color text NOT NULL,
    CONSTRAINT default_labels_color_check CHECK ((color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'::text)),
    CONSTRAINT default_labels_pkey PRIMARY KEY (id)
);

CREATE TABLE public.default_roles (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    builtin text,
    permissions text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT default_roles_builtin_check CHECK ((builtin = ANY (ARRAY['member'::text, 'guest'::text]))),
    CONSTRAINT default_roles_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 40))),
    CONSTRAINT default_roles_builtin_key UNIQUE (builtin),
    CONSTRAINT default_roles_pkey PRIMARY KEY (id)
);

CREATE TABLE public.default_status_types (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    icon text NOT NULL,
    color text,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT default_status_types_category_check CHECK ((category = ANY (ARRAY['backlog'::text, 'unstarted'::text, 'started'::text, 'completed'::text, 'canceled'::text]))),
    CONSTRAINT default_status_types_color_check CHECK ((color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'::text)),
    CONSTRAINT default_status_types_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 30))),
    CONSTRAINT default_status_types_pkey PRIMARY KEY (id)
);

CREATE TABLE public.default_statuses (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    type_id uuid,
    "position" integer NOT NULL,
    CONSTRAINT default_statuses_category_check CHECK ((category = ANY (ARRAY['backlog'::text, 'unstarted'::text, 'started'::text, 'completed'::text, 'canceled'::text]))),
    CONSTRAINT default_statuses_pkey PRIMARY KEY (id),
    CONSTRAINT default_statuses_type_id_fkey FOREIGN KEY (type_id) REFERENCES public.default_status_types(id) ON DELETE SET NULL
);

CREATE TABLE public.default_templates (
    id uuid DEFAULT uuidv7() NOT NULL,
    name text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    description jsonb,
    status_id uuid,
    priority smallint DEFAULT 0 NOT NULL,
    label_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT default_templates_priority_check CHECK (((priority >= 0) AND (priority <= 4))),
    CONSTRAINT default_templates_pkey PRIMARY KEY (id),
    CONSTRAINT default_templates_status_id_fkey FOREIGN KEY (status_id) REFERENCES public.default_statuses(id) ON DELETE SET NULL
);

CREATE TABLE public.feedback (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    body text NOT NULL,
    email text,
    name text,
    source text NOT NULL,
    issue_id uuid,
    archived_at timestamp with time zone,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT feedback_body_check CHECK (((char_length(body) >= 1) AND (char_length(body) <= 5000))),
    CONSTRAINT feedback_email_check CHECK ((char_length(email) <= 254)),
    CONSTRAINT feedback_name_check CHECK ((char_length(name) <= 80)),
    CONSTRAINT feedback_source_check CHECK ((source = ANY (ARRAY['form'::text, 'member'::text]))),
    CONSTRAINT feedback_pkey PRIMARY KEY (id),
    CONSTRAINT feedback_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT feedback_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE SET NULL,
    CONSTRAINT feedback_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.file_folders (
    id uuid DEFAULT uuidv7() NOT NULL,
    project_id uuid NOT NULL,
    parent_id uuid,
    name text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT file_folders_name_check CHECK (((length(name) >= 1) AND (length(name) <= 100))),
    CONSTRAINT file_folders_pkey PRIMARY KEY (id),
    CONSTRAINT file_folders_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT file_folders_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.file_folders(id) ON DELETE CASCADE,
    CONSTRAINT file_folders_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE
);

CREATE TABLE public.files (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid,
    uploader_id uuid,
    name text NOT NULL,
    content_type text NOT NULL,
    size bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    project_id uuid,
    folder_id uuid,
    listed boolean DEFAULT false NOT NULL,
    issue_id uuid,
    CONSTRAINT files_pkey PRIMARY KEY (id),
    CONSTRAINT files_folder_id_fkey FOREIGN KEY (folder_id) REFERENCES public.file_folders(id) ON DELETE RESTRICT,
    CONSTRAINT files_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT files_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT files_uploader_id_fkey FOREIGN KEY (uploader_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT files_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
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
    CONSTRAINT git_hooks_provider_check CHECK ((provider = ANY (ARRAY['github'::text, 'gitlab'::text]))),
    CONSTRAINT git_hooks_pkey PRIMARY KEY (id),
    CONSTRAINT git_hooks_token_key UNIQUE (token),
    CONSTRAINT git_hooks_workspace_id_provider_key UNIQUE (workspace_id, provider),
    CONSTRAINT git_hooks_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT git_hooks_merge_status_id_fkey FOREIGN KEY (merge_status_id) REFERENCES public.statuses(id) ON DELETE SET NULL,
    CONSTRAINT git_hooks_open_status_id_fkey FOREIGN KEY (open_status_id) REFERENCES public.statuses(id) ON DELETE SET NULL,
    CONSTRAINT git_hooks_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.invite_sends (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT invite_sends_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT invite_sends_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_doc_updates (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    issue_id uuid NOT NULL,
    data bytea NOT NULL,
    CONSTRAINT issue_doc_updates_pkey PRIMARY KEY (id),
    CONSTRAINT issue_doc_updates_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_events (
    id uuid DEFAULT uuidv7() NOT NULL,
    issue_id uuid NOT NULL,
    actor_id uuid,
    kind text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_events_pkey PRIMARY KEY (id),
    CONSTRAINT issue_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT issue_events_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE
);

CREATE TABLE public.labels (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    color text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT labels_color_check CHECK ((color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'::text)),
    CONSTRAINT labels_pkey PRIMARY KEY (id),
    CONSTRAINT labels_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_labels (
    issue_id uuid NOT NULL,
    label_id uuid NOT NULL,
    CONSTRAINT issue_labels_pkey PRIMARY KEY (issue_id, label_id),
    CONSTRAINT issue_labels_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT issue_labels_label_id_fkey FOREIGN KEY (label_id) REFERENCES public.labels(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_links (
    blocker_id uuid NOT NULL,
    blocked_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_links_check CHECK ((blocker_id <> blocked_id)),
    CONSTRAINT issue_links_pkey PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT issue_links_blocked_id_fkey FOREIGN KEY (blocked_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT issue_links_blocker_id_fkey FOREIGN KEY (blocker_id) REFERENCES public.issues_all(id) ON DELETE CASCADE
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
    CONSTRAINT pull_requests_state_check CHECK ((state = ANY (ARRAY['draft'::text, 'open'::text, 'merged'::text, 'closed'::text]))),
    CONSTRAINT pull_requests_pkey PRIMARY KEY (id),
    CONSTRAINT pull_requests_workspace_id_provider_repo_number_key UNIQUE (workspace_id, provider, repo, number),
    CONSTRAINT pull_requests_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_pull_requests (
    issue_id uuid NOT NULL,
    pr_id uuid NOT NULL,
    closes boolean NOT NULL,
    CONSTRAINT issue_pull_requests_pkey PRIMARY KEY (issue_id, pr_id),
    CONSTRAINT issue_pull_requests_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT issue_pull_requests_pr_id_fkey FOREIGN KEY (pr_id) REFERENCES public.pull_requests(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_templates (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    description jsonb,
    project_id uuid,
    status_id uuid,
    assignee_id uuid,
    priority smallint DEFAULT 0 NOT NULL,
    label_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    repeat_every integer,
    repeat_unit text,
    next_on date,
    paused boolean DEFAULT false NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_templates_check CHECK ((((repeat_every IS NULL) = (repeat_unit IS NULL)) AND ((repeat_every IS NULL) = (next_on IS NULL)))),
    CONSTRAINT issue_templates_repeat_every_check CHECK (((repeat_every >= 1) AND (repeat_every <= 365))),
    CONSTRAINT issue_templates_repeat_unit_check CHECK ((repeat_unit = ANY (ARRAY['day'::text, 'week'::text, 'month'::text]))),
    CONSTRAINT issue_templates_pkey PRIMARY KEY (id),
    CONSTRAINT issue_templates_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT issue_templates_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT issue_templates_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT issue_templates_status_id_fkey FOREIGN KEY (status_id) REFERENCES public.statuses(id) ON DELETE SET NULL,
    CONSTRAINT issue_templates_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_viewers (
    conn_id text NOT NULL,
    issue_id uuid NOT NULL,
    user_id uuid NOT NULL,
    instance text NOT NULL,
    seen_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_viewers_pkey PRIMARY KEY (conn_id, issue_id),
    CONSTRAINT issue_viewers_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT issue_viewers_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.issue_watchers (
    issue_id uuid NOT NULL,
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_watchers_pkey PRIMARY KEY (issue_id, user_id),
    CONSTRAINT issue_watchers_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT issue_watchers_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
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
    last_reply_at timestamp with time zone,
    pinned_until timestamp with time zone,
    pinned_by uuid,
    CONSTRAINT messages_pkey PRIMARY KEY (id),
    CONSTRAINT messages_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT messages_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE,
    CONSTRAINT messages_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.messages(id) ON DELETE CASCADE,
    CONSTRAINT messages_pinned_by_fkey FOREIGN KEY (pinned_by) REFERENCES public.users(id) ON DELETE SET NULL
);

CREATE TABLE public.message_reactions (
    message_id uuid NOT NULL,
    user_id uuid NOT NULL,
    emoji text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT message_reactions_emoji_check CHECK (((char_length(emoji) >= 1) AND (char_length(emoji) <= 16))),
    CONSTRAINT message_reactions_pkey PRIMARY KEY (message_id, user_id, emoji),
    CONSTRAINT message_reactions_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.messages(id) ON DELETE CASCADE,
    CONSTRAINT message_reactions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.notes (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    body jsonb,
    color text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    seed_claimed_at timestamp with time zone,
    body_generation integer DEFAULT 0 NOT NULL,
    CONSTRAINT notes_color_check CHECK (((color = ''::text) OR (color ~ '^#[0-9a-f]{6}$'::text))),
    CONSTRAINT notes_pkey PRIMARY KEY (id),
    CONSTRAINT notes_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT notes_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT notes_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.note_doc_updates (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    note_id uuid NOT NULL,
    data bytea NOT NULL,
    CONSTRAINT note_doc_updates_pkey PRIMARY KEY (id),
    CONSTRAINT note_doc_updates_note_id_fkey FOREIGN KEY (note_id) REFERENCES public.notes(id) ON DELETE CASCADE
);

CREATE TABLE public.note_members (
    note_id uuid NOT NULL,
    user_id uuid NOT NULL,
    pinned boolean DEFAULT false NOT NULL,
    archived boolean DEFAULT false NOT NULL,
    labels text[] DEFAULT '{}'::text[] NOT NULL,
    added_at timestamp with time zone DEFAULT now() NOT NULL,
    role text DEFAULT 'edit'::text NOT NULL,
    CONSTRAINT note_members_role_check CHECK ((role = ANY (ARRAY['edit'::text, 'view'::text]))),
    CONSTRAINT note_members_pkey PRIMARY KEY (note_id, user_id),
    CONSTRAINT note_members_note_id_fkey FOREIGN KEY (note_id) REFERENCES public.notes(id) ON DELETE CASCADE,
    CONSTRAINT note_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
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
    weekly boolean DEFAULT true NOT NULL,
    last_weekly_at timestamp with time zone DEFAULT now(),
    due_dates boolean DEFAULT true NOT NULL,
    projects boolean DEFAULT true NOT NULL,
    CONSTRAINT notification_settings_email_check CHECK ((email = ANY (ARRAY['instant'::text, 'digest'::text, 'off'::text]))),
    CONSTRAINT notification_settings_pkey PRIMARY KEY (user_id),
    CONSTRAINT notification_settings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.workspace_roles (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    builtin text,
    permissions text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspace_roles_builtin_check CHECK ((builtin = ANY (ARRAY['admin'::text, 'member'::text, 'guest'::text]))),
    CONSTRAINT workspace_roles_pkey PRIMARY KEY (id),
    CONSTRAINT workspace_roles_workspace_id_builtin_key UNIQUE (workspace_id, builtin),
    CONSTRAINT workspace_roles_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.workspace_invites (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    email text NOT NULL,
    token_hash bytea NOT NULL,
    invited_by uuid,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    role_id uuid,
    project_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    dismissed_at timestamp with time zone,
    prev_token_hash bytea,
    CONSTRAINT workspace_invites_pkey PRIMARY KEY (id),
    CONSTRAINT workspace_invites_token_hash_key UNIQUE (token_hash),
    CONSTRAINT workspace_invites_workspace_id_email_key UNIQUE (workspace_id, email),
    CONSTRAINT workspace_invites_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT workspace_invites_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.workspace_roles(id) ON DELETE SET NULL,
    CONSTRAINT workspace_invites_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
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
    project_id uuid,
    invite_id uuid,
    CONSTRAINT notifications_kind_check CHECK ((kind = ANY (ARRAY['mention'::text, 'assigned'::text, 'status'::text, 'comment'::text, 'chat_mention'::text, 'message'::text, 'project_added'::text, 'invite'::text, 'due_soon'::text, 'overdue'::text]))),
    CONSTRAINT notifications_target_check CHECK ((num_nonnulls(issue_id, chat_id, project_id) =
CASE
    WHEN (kind = 'invite'::text) THEN 0
    ELSE 1
END)),
    CONSTRAINT notifications_pkey PRIMARY KEY (id),
    CONSTRAINT notifications_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT notifications_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE,
    CONSTRAINT notifications_invite_id_fkey FOREIGN KEY (invite_id) REFERENCES public.workspace_invites(id) ON DELETE SET NULL,
    CONSTRAINT notifications_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issues_all(id) ON DELETE CASCADE,
    CONSTRAINT notifications_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.messages(id) ON DELETE CASCADE,
    CONSTRAINT notifications_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT notifications_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT notifications_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.oauth_clients (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    redirect_uris text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT oauth_clients_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 100))),
    CONSTRAINT oauth_clients_redirect_uris_check CHECK (((cardinality(redirect_uris) >= 1) AND (cardinality(redirect_uris) <= 5))),
    CONSTRAINT oauth_clients_pkey PRIMARY KEY (id)
);

CREATE TABLE public.oauth_codes (
    code_hash bytea NOT NULL,
    client_id uuid NOT NULL,
    user_id uuid NOT NULL,
    redirect_uri text NOT NULL,
    code_challenge text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT oauth_codes_pkey PRIMARY KEY (code_hash),
    CONSTRAINT oauth_codes_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.oauth_clients(id) ON DELETE CASCADE,
    CONSTRAINT oauth_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
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
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    html text,
    CONSTRAINT outbound_mail_pkey PRIMARY KEY (id)
);

CREATE TABLE public.paddle_adjustments (
    adjustment_id text NOT NULL,
    transaction_id text,
    user_id uuid,
    action text NOT NULL,
    status text NOT NULL,
    currency_code text NOT NULL,
    total bigint NOT NULL,
    created_at timestamp with time zone NOT NULL,
    paddle_updated_at timestamp with time zone NOT NULL,
    CONSTRAINT paddle_adjustments_pkey PRIMARY KEY (adjustment_id),
    CONSTRAINT paddle_adjustments_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL
);

CREATE TABLE public.paddle_customers (
    customer_id text NOT NULL,
    user_id uuid,
    email text DEFAULT ''::text NOT NULL,
    paddle_updated_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT paddle_customers_pkey PRIMARY KEY (customer_id),
    CONSTRAINT paddle_customers_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL
);

CREATE TABLE public.paddle_subscriptions (
    subscription_id text NOT NULL,
    customer_id text NOT NULL,
    user_id uuid,
    status text NOT NULL,
    price_id text NOT NULL,
    product_id text NOT NULL,
    plan text NOT NULL,
    billing_interval text,
    currency_code text,
    next_billed_at timestamp with time zone,
    scheduled_change_action text,
    scheduled_change_at timestamp with time zone,
    paddle_updated_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    unit_amount bigint,
    trial_reminded_at timestamp with time zone,
    past_due_emailed_at timestamp with time zone,
    trial_ends_at timestamp with time zone,
    CONSTRAINT paddle_subscriptions_pkey PRIMARY KEY (subscription_id),
    CONSTRAINT paddle_subscriptions_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES public.paddle_customers(customer_id),
    CONSTRAINT paddle_subscriptions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL
);

CREATE TABLE public.paddle_transactions (
    transaction_id text NOT NULL,
    user_id uuid,
    subscription_id text,
    customer_id text,
    status text NOT NULL,
    currency_code text NOT NULL,
    subtotal bigint NOT NULL,
    tax bigint NOT NULL,
    total bigint NOT NULL,
    earnings bigint,
    billed_at timestamp with time zone NOT NULL,
    paddle_updated_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT paddle_transactions_pkey PRIMARY KEY (transaction_id),
    CONSTRAINT paddle_transactions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL
);

CREATE TABLE public.project_key_aliases (
    workspace_id uuid NOT NULL,
    key text NOT NULL,
    project_id uuid NOT NULL,
    CONSTRAINT project_key_aliases_pkey PRIMARY KEY (workspace_id, key),
    CONSTRAINT project_key_aliases_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT project_key_aliases_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.project_members (
    project_id uuid NOT NULL,
    user_id uuid NOT NULL,
    added_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_members_pkey PRIMARY KEY (project_id, user_id),
    CONSTRAINT project_members_added_by_fkey FOREIGN KEY (added_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT project_members_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE,
    CONSTRAINT project_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.project_milestones (
    id uuid DEFAULT uuidv7() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    date date NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT project_milestones_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 60))),
    CONSTRAINT project_milestones_pkey PRIMARY KEY (id),
    CONSTRAINT project_milestones_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT project_milestones_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE
);

CREATE TABLE public.project_updates (
    id uuid DEFAULT uuidv7() NOT NULL,
    project_id uuid NOT NULL,
    author_id uuid,
    health text NOT NULL,
    body jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    CONSTRAINT project_updates_health_check CHECK ((health = ANY (ARRAY['on_track'::text, 'at_risk'::text, 'off_track'::text]))),
    CONSTRAINT project_updates_pkey PRIMARY KEY (id),
    CONSTRAINT project_updates_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT project_updates_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE
);

CREATE UNLOGGED TABLE public.rate_limits (
    key text NOT NULL,
    tokens double precision NOT NULL,
    at timestamp with time zone NOT NULL,
    ok boolean NOT NULL,
    CONSTRAINT rate_limits_pkey PRIMARY KEY (key)
);

CREATE UNLOGGED TABLE public.realtime_conns (
    conn_id text NOT NULL,
    user_id uuid NOT NULL,
    instance text NOT NULL,
    seen_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT realtime_conns_pkey PRIMARY KEY (conn_id),
    CONSTRAINT realtime_conns_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.refresh_tokens (
    token_hash bytea CONSTRAINT sessions_token_hash_not_null NOT NULL,
    user_id uuid CONSTRAINT sessions_user_id_not_null NOT NULL,
    expires_at timestamp with time zone CONSTRAINT sessions_expires_at_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT now() CONSTRAINT sessions_created_at_not_null NOT NULL,
    family_id uuid NOT NULL,
    used_at timestamp with time zone,
    remember boolean DEFAULT true NOT NULL,
    CONSTRAINT sessions_pkey PRIMARY KEY (token_hash),
    CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.saved_views (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    path text NOT NULL,
    shared boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT saved_views_pkey PRIMARY KEY (id),
    CONSTRAINT saved_views_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT saved_views_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.system_config (
    key text NOT NULL,
    value text DEFAULT ''::text NOT NULL,
    secret boolean DEFAULT false NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT system_config_pkey PRIMARY KEY (key)
);

CREATE TABLE public.test_sites (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    base_url text NOT NULL,
    host text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT test_sites_pkey PRIMARY KEY (id),
    CONSTRAINT test_sites_workspace_id_host_key UNIQUE (workspace_id, host),
    CONSTRAINT test_sites_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.test_accounts (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    label text NOT NULL,
    username text NOT NULL,
    password text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    site_id uuid,
    CONSTRAINT test_accounts_pkey PRIMARY KEY (id),
    CONSTRAINT test_accounts_workspace_id_label_key UNIQUE (workspace_id, label),
    CONSTRAINT test_accounts_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.test_sites(id) ON DELETE CASCADE,
    CONSTRAINT test_accounts_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
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
    marked_at timestamp with time zone,
    video_id uuid,
    CONSTRAINT test_results_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'passed'::text, 'failed'::text, 'skipped'::text, 'error'::text]))),
    CONSTRAINT test_results_pkey PRIMARY KEY (id),
    CONSTRAINT test_results_run_id_case_id_key UNIQUE (run_id, case_id),
    CONSTRAINT test_results_case_id_fkey FOREIGN KEY (case_id) REFERENCES public.test_cases(id) ON DELETE CASCADE,
    CONSTRAINT test_results_marked_by_fkey FOREIGN KEY (marked_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT test_results_run_id_fkey FOREIGN KEY (run_id) REFERENCES public.test_runs(id) ON DELETE CASCADE,
    CONSTRAINT test_results_video_id_fkey FOREIGN KEY (video_id) REFERENCES public.files(id) ON DELETE SET NULL
);

CREATE TABLE public.test_steps (
    id uuid DEFAULT uuidv7() NOT NULL,
    result_id uuid NOT NULL,
    "position" integer NOT NULL,
    action text NOT NULL,
    url text,
    screenshot_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT test_steps_pkey PRIMARY KEY (id),
    CONSTRAINT test_steps_result_id_fkey FOREIGN KEY (result_id) REFERENCES public.test_results(id) ON DELETE CASCADE,
    CONSTRAINT test_steps_screenshot_id_fkey FOREIGN KEY (screenshot_id) REFERENCES public.files(id) ON DELETE SET NULL
);

CREATE TABLE public.uploads (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid,
    user_id uuid NOT NULL,
    name text NOT NULL,
    content_type text NOT NULL,
    size bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT uploads_pkey PRIMARY KEY (id),
    CONSTRAINT uploads_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT uploads_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.user_mfa (
    user_id uuid NOT NULL,
    secret text NOT NULL,
    last_step bigint DEFAULT 0 NOT NULL,
    recovery_codes text[] NOT NULL,
    enabled_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_mfa_pkey PRIMARY KEY (user_id),
    CONSTRAINT user_mfa_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE public.webhooks (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    url text NOT NULL,
    secret text NOT NULL,
    events text[] NOT NULL,
    active boolean DEFAULT true NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT webhooks_events_check CHECK ((cardinality(events) > 0)),
    CONSTRAINT webhooks_url_check CHECK ((char_length(url) <= 2000)),
    CONSTRAINT webhooks_pkey PRIMARY KEY (id),
    CONSTRAINT webhooks_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT webhooks_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.webhook_deliveries (
    id uuid DEFAULT uuidv7() NOT NULL,
    webhook_id uuid NOT NULL,
    event text NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    next_at timestamp with time zone DEFAULT now() NOT NULL,
    response_code integer,
    response_error text,
    delivered_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT webhook_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'delivered'::text, 'failed'::text]))),
    CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id),
    CONSTRAINT webhook_deliveries_webhook_id_fkey FOREIGN KEY (webhook_id) REFERENCES public.webhooks(id) ON DELETE CASCADE
);

CREATE TABLE public.workspace_audit (
    id uuid DEFAULT uuidv7() NOT NULL,
    workspace_id uuid NOT NULL,
    actor_id uuid,
    actor_name text DEFAULT ''::text NOT NULL,
    actor_email text DEFAULT ''::text NOT NULL,
    action text NOT NULL,
    target text DEFAULT ''::text NOT NULL,
    detail jsonb DEFAULT '{}'::jsonb NOT NULL,
    ip text DEFAULT ''::text NOT NULL,
    project_id uuid,
    at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workspace_audit_pkey PRIMARY KEY (id),
    CONSTRAINT workspace_audit_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL,
    CONSTRAINT workspace_audit_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

CREATE TABLE public.workspace_members (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    last_opened_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    role_id uuid NOT NULL,
    CONSTRAINT workspace_members_pkey PRIMARY KEY (workspace_id, user_id),
    CONSTRAINT workspace_members_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.workspace_roles(id),
    CONSTRAINT workspace_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT workspace_members_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE CASCADE
);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_avatar_file_id_fkey FOREIGN KEY (avatar_file_id) REFERENCES public.files(id) ON DELETE SET NULL;

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

CREATE FUNCTION public.is_guest(ws uuid, viewer uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  select exists (select 1 from workspace_members m join workspace_roles r on r.id = m.role_id
                 where m.workspace_id = ws and m.user_id = viewer and r.builtin = 'guest')
$$;

CREATE FUNCTION public.project_key_alias() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  if exists (select 1 from project_key_aliases where workspace_id = new.workspace_id and key = new.key and project_id <> new.id) then
    raise exception 'project key % was used by another project', new.key using errcode = 'unique_violation';
  end if;
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
declare
  t_map jsonb := (select coalesce(jsonb_object_agg(id, uuidv7()), '{}') from default_status_types);
  s_map jsonb := (select coalesce(jsonb_object_agg(id, uuidv7()), '{}') from default_statuses);
  l_map jsonb := (select coalesce(jsonb_object_agg(id, uuidv7()), '{}') from default_labels);
begin
  insert into status_types (id, workspace_id, name, category, icon, color)
    select (t_map ->> d.id::text)::uuid, new.id, d.name, d.category, d.icon, d.color from default_status_types d;
  if s_map = '{}' then
    insert into statuses (workspace_id, name, category, position) values
      (new.id, 'Backlog', 'backlog', 0), (new.id, 'Todo', 'unstarted', 1), (new.id, 'In progress', 'started', 2),
      (new.id, 'Done', 'completed', 3), (new.id, 'Cancelled', 'canceled', 4);
  else
    insert into statuses (id, workspace_id, name, category, type_id, position)
      select (s_map ->> d.id::text)::uuid, new.id, d.name, d.category, (t_map ->> d.type_id::text)::uuid, d.position
      from default_statuses d;
  end if;
  insert into labels (id, workspace_id, name, color)
    select (l_map ->> d.id::text)::uuid, new.id, d.name, d.color from default_labels d;
  insert into issue_templates (workspace_id, name, title, description, status_id, priority, label_ids)
    select new.id, d.name, d.title, d.description, (s_map ->> d.status_id::text)::uuid, d.priority,
           array(select (l_map ->> l::text)::uuid from unnest(d.label_ids) l where l_map ? l::text)
    from default_templates d;
  return null;
end $$;

CREATE FUNCTION public.status_json(s public.statuses) RETURNS json
    LANGUAGE sql STABLE
    AS $$
  select json_build_object('id', s.id, 'name', s.name, 'category', s.category, 'position', s.position,
                           'icon', t.icon, 'color', t.color)
  from (select) x left join status_types t on t.id = s.type_id
$$;

CREATE FUNCTION public.webhook_actor(id uuid) RETURNS jsonb
    LANGUAGE sql STABLE
    AS $$
  select jsonb_build_object('id', u.id, 'name', u.name) from users u where u.id = webhook_actor.id
$$;

CREATE FUNCTION public.webhook_comment() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
declare
  r record;
begin
  select * into r from webhook_issue_ref(new.issue_id);
  if r.ws is not null then
    perform webhook_enqueue(r.ws, 'comment.created', jsonb_build_object('issue', r.ref, 'actor', webhook_actor(new.author_id),
      'comment', jsonb_build_object('id', new.id)));
  end if;
  return null;
end $$;

CREATE FUNCTION public.webhook_enqueue(ws uuid, ev text, body jsonb, hook uuid DEFAULT NULL::uuid) RETURNS void
    LANGUAGE plpgsql
    AS $$
begin
  insert into webhook_deliveries (id, webhook_id, event, payload)
  select d.id, w.id, ev, body || jsonb_build_object('id', d.id, 'type', ev, 'workspace', s.slug,
    'created_at', to_char(now() at time zone 'utc', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
  from webhooks w join workspaces s on s.id = w.workspace_id, lateral (select uuidv7() as id) d
  where w.workspace_id = ws and (w.id = hook or (hook is null and w.active and ev = any(w.events)));
  if found then
    perform pg_notify('webhooks', '');
  end if;
end $$;

CREATE FUNCTION public.webhook_issue_deleted() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
declare
  pkey text;
begin
  select p.key into pkey from projects p
  where p.id = old.project_id and not p.private and exists (select 1 from webhooks w where w.workspace_id = old.workspace_id);
  if pkey is not null then
    perform webhook_enqueue(old.workspace_id, 'issue.deleted', jsonb_build_object(
      'actor', case when tg_op = 'UPDATE' then webhook_actor(new.deleted_by) end,
      'issue', jsonb_build_object('id', old.id, 'identifier', pkey || '-' || old.number, 'title', old.title)));
  end if;
  return null;
end $$;

CREATE FUNCTION public.webhook_issue_event() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
declare
  r record;
begin
  select * into r from webhook_issue_ref(new.issue_id);
  if r.ws is null then
    return null;
  end if;
  if new.kind = 'created' then
    perform webhook_enqueue(r.ws, 'issue.created', jsonb_build_object('issue', r.ref, 'actor', webhook_actor(new.actor_id)));
  else
    perform webhook_enqueue(r.ws, 'issue.updated', jsonb_build_object('issue', r.ref, 'actor', webhook_actor(new.actor_id),
      'change', jsonb_build_object('kind', new.kind, 'data', new.data)));
  end if;
  return null;
end $$;

CREATE FUNCTION public.webhook_issue_ref(issue uuid, OUT ws uuid, OUT ref jsonb) RETURNS record
    LANGUAGE sql STABLE
    AS $$
  select i.workspace_id, jsonb_build_object('id', i.id, 'identifier', p.key || '-' || i.number, 'title', i.title)
  from issues i join projects p on p.id = i.project_id
  where i.id = issue and not p.private and exists (select 1 from webhooks w where w.workspace_id = i.workspace_id)
$$;

CREATE FUNCTION public.workspace_audit_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  if tg_op = 'UPDATE' then
    if old.actor_id is not null and new.actor_id is null and (to_jsonb(new) - 'actor_id') = (to_jsonb(old) - 'actor_id') then
      return new;
    end if;
  elsif tg_op = 'DELETE' then
    if old.at < now() - interval '1 year' or not exists (select 1 from workspaces where id = old.workspace_id) then
      return old;
    end if;
  end if;
  raise exception 'workspace_audit is append-only';
end $$;

CREATE VIEW public.issues AS
 SELECT id,
    workspace_id,
    project_id,
    number,
    title,
    status_id,
    assignee_id,
    priority,
    due_date,
    created_by,
    created_at,
    updated_at,
    parent_id,
    description,
    seed_claimed_at,
    description_text,
    description_generation,
    cycle_id,
    rank,
    deleted_at,
    deleted_by,
    estimate
   FROM public.issues_all
  WHERE (deleted_at IS NULL);

INSERT INTO public.default_labels (id, name, color) VALUES ('01a1d384-d3ae-7a41-b489-4d8a8f5f93cc', 'Bug', '#eb5757');

INSERT INTO public.default_labels (id, name, color) VALUES ('01a1d384-d3ae-7f76-a202-ddaf992fb266', 'Feature', '#9b51e0');

INSERT INTO public.default_labels (id, name, color) VALUES ('01a1d384-d3ae-7ffb-9922-c99ee7e84194', 'Improvement', '#5e6ad2');

INSERT INTO public.default_labels (id, name, color) VALUES ('01a1d384-d3af-705a-898b-f842138f50ef', 'Docs', '#8a8f98');

INSERT INTO public.default_roles (id, name, builtin, permissions, created_at) VALUES ('01a1d384-d3aa-7065-a822-f4ef2aec2257', 'Member', 'member', '{issues.create,comments.create}', '2026-11-13 06:07:39.917772+00');

INSERT INTO public.default_roles (id, name, builtin, permissions, created_at) VALUES ('01a1d384-d3aa-72fe-8ace-fd1471973882', 'Guest', 'guest', '{issues.create,comments.create}', '2026-11-13 06:07:39.917772+00');

INSERT INTO public.default_statuses (id, name, category, type_id, "position") VALUES ('01a1d384-d3aa-73cf-8666-04d6e985dd51', 'Backlog', 'backlog', NULL, 0);

INSERT INTO public.default_statuses (id, name, category, type_id, "position") VALUES ('01a1d384-d3aa-7532-8fb2-263cd7128cc0', 'Todo', 'unstarted', NULL, 1);

INSERT INTO public.default_statuses (id, name, category, type_id, "position") VALUES ('01a1d384-d3aa-7550-b8ad-fac0f36d2eba', 'In progress', 'started', NULL, 2);

INSERT INTO public.default_statuses (id, name, category, type_id, "position") VALUES ('01a1d384-d3aa-755f-bdfa-e4177dd359c2', 'Done', 'completed', NULL, 4);

INSERT INTO public.default_statuses (id, name, category, type_id, "position") VALUES ('01a1d384-d3aa-756b-9be4-4a5eca35f090', 'Cancelled', 'canceled', NULL, 5);

INSERT INTO public.default_statuses (id, name, category, type_id, "position") VALUES ('01a1d384-d3af-7b3b-8a19-a2907af86d1b', 'In review', 'started', NULL, 3);

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_host', 'localhost', false, 'SMTP server host; empty disables email', '2026-11-02 16:11:47.947713+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_port', '1025', false, 'SMTP server port', '2026-11-02 16:11:47.947713+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_username', '', false, 'SMTP username; empty skips auth', '2026-11-02 16:11:47.947713+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_password', '', true, 'SMTP password', '2026-11-02 16:11:47.947713+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('smtp_from', 'Taskwell <no-reply@taskwell.local>', false, 'From header for outgoing email', '2026-11-02 16:11:47.947713+00');

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

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('digest_hour_utc', '8', false, 'Hour (0-23) daily notification digests are sent, in each user''s timezone', '2026-11-02 16:11:48.067809+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('ai_test_monthly_limit_usd', '50', false, 'AI test spend per workspace per month, in USD', '2026-11-02 16:11:48.314753+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('ai_test_workspace_concurrency', '2', false, 'AI test runs one workspace can have running at once', '2026-11-02 16:11:48.314753+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('ai_tester_heartbeat', '', false, 'The URL of the tester that last checked in, and when (updated_at; every 30s, AI runs need one within 90s)', '2026-11-02 16:11:48.314753+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('s3_endpoint', 'http://localhost:9000', false, 'S3-compatible endpoint (MinIO, R2); empty uses AWS. Browsers follow presigned links to it', '2026-11-13 06:07:39.617829+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('s3_region', 'us-east-1', false, 'Bucket region (auto for R2)', '2026-11-13 06:07:39.617829+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('s3_bucket', 'taskwell', false, 'Private bucket that holds uploads; empty makes uploads fail', '2026-11-13 06:07:39.617829+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('s3_access_key_id', 'taskwell', false, 'S3 access key ID', '2026-11-13 06:07:39.617829+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('s3_secret_access_key', 'taskwell-secret', true, 'S3 secret access key', '2026-11-13 06:07:39.617829+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('files_cdn_url', '', false, 'Where the file Worker serves uploads (https://files.taskwell.dev); empty redirects to presigned bucket links', '2026-11-13 06:07:39.768199+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('files_cdn_secret', '', true, 'Signs file links for the Worker; the same value as its FILES_SECRET', '2026-11-13 06:07:39.768199+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('paddle_environment', '', false, 'Paddle environment for billing: sandbox or production; anything else turns billing off', '2026-11-13 06:07:39.790869+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('paddle_api_key', '', true, 'Paddle server API key; empty turns billing off', '2026-11-13 06:07:39.790869+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('paddle_webhook_secret', '', true, 'Secret key of the Paddle notification destination at /api/billing/webhook', '2026-11-13 06:07:39.790869+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_chat', 'true', false, 'Chat', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_chat_plans', 'free,starter,pro,advanced', false, 'Plans with chat', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_notes', 'true', false, 'Notes', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_notes_plans', 'free,starter,pro,advanced', false, 'Plans with notes', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_files', 'true', false, 'Files', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_files_plans', 'free,starter,pro,advanced', false, 'Plans with files', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_roadmap', 'true', false, 'Roadmap', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_roadmap_plans', 'free,starter,pro,advanced', false, 'Plans with roadmap', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_feedback', 'true', false, 'Customer feedback', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_feedback_plans', 'free,starter,pro,advanced', false, 'Plans with customer feedback', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_team', 'true', false, 'Team analytics', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_team_plans', 'free,starter,pro,advanced', false, 'Plans with team analytics', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_cycles', 'true', false, 'Cycles', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_cycles_plans', 'free,starter,pro,advanced', false, 'Plans with cycles', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_ai_testing', 'true', false, 'AI testing', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_integrations', 'true', false, 'GitHub and GitLab', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_integrations_plans', 'free,starter,pro,advanced', false, 'Plans with GitHub and GitLab', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_webhooks', 'true', false, 'Webhooks', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_webhooks_plans', 'free,starter,pro,advanced', false, 'Plans with webhooks', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_templates', 'true', false, 'Templates', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_templates_plans', 'free,starter,pro,advanced', false, 'Plans with templates', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_api', 'true', false, 'API and MCP', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_api_plans', 'free,starter,pro,advanced', false, 'Plans with API and MCP', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_imports', 'true', false, 'Imports', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_imports_plans', 'free,starter,pro,advanced', false, 'Plans with imports', '2026-11-13 06:07:39.820101+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('feature_ai_testing_plans', 'starter,pro,advanced', false, 'Plans with AI testing', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_free_workspaces', '1', false, 'Free plan: workspaces an account can create (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_free_projects', '1', false, 'Free plan: projects per workspace, archived ones included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_free_members', '1', false, 'Free plan: members per workspace, pending invites included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_free_storage_gb', '2', false, 'Free plan: storage per workspace in GB (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_free_ai_usd', '0', false, 'Free plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_starter_workspaces', '1', false, 'Starter plan: workspaces an account can create (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_starter_projects', '5', false, 'Starter plan: projects per workspace, archived ones included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_starter_members', '10', false, 'Starter plan: members per workspace, pending invites included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_starter_storage_gb', '20', false, 'Starter plan: storage per workspace in GB (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_starter_ai_usd', '1', false, 'Starter plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_pro_workspaces', '3', false, 'Pro plan: workspaces an account can create (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_pro_projects', '0', false, 'Pro plan: projects per workspace, archived ones included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_pro_members', '25', false, 'Pro plan: members per workspace, pending invites included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_pro_storage_gb', '100', false, 'Pro plan: storage per workspace in GB (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_pro_ai_usd', '3', false, 'Pro plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_advanced_workspaces', '0', false, 'Advanced plan: workspaces an account can create (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_advanced_projects', '0', false, 'Advanced plan: projects per workspace, archived ones included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_advanced_members', '0', false, 'Advanced plan: members per workspace, pending invites included (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_advanced_storage_gb', '500', false, 'Advanced plan: storage per workspace in GB (0 is unlimited)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('plan_advanced_ai_usd', '15', false, 'Advanced plan: AI test spend a month in USD, across all the account''s workspaces (0 is none)', '2026-11-13 06:07:39.862901+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('files_cdn_zone_id', '', false, 'Cloudflare zone id of the file CDN''s domain (taskwell.dev), for purging deleted files from the edge cache; empty skips the purge', '2026-11-13 06:07:39.87077+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('files_cdn_api_token', '', true, 'Cloudflare API token with the Cache Purge permission on that zone', '2026-11-13 06:07:39.87077+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('ai_tests_server_runs', 'false', false, 'Run AI tests, recorded replays and browser sessions on Taskwell''s tester (spends the platform Anthropic key)', '2026-11-13 06:07:39.877712+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('beta_mode', 'false', false, 'Beta: hide pricing, billing and upgrade prompts, and show the beta notice instead of the privacy notice and terms (true or false)', '2026-11-13 06:07:39.903103+00');

INSERT INTO public.system_config (key, value, secret, description, updated_at) VALUES ('app_feedback_url', '', false, 'Link for "Report a bug" in the user app sidebar, e.g. your Taskwell feedback form (http or https); empty hides it', '2026-11-13 06:07:39.954534+00');

CREATE INDEX account_events_user_idx ON public.account_events USING btree (user_id, id DESC);

CREATE INDEX admin_sessions_expires_at_idx ON public.admin_sessions USING btree (expires_at);

CREATE INDEX admin_sessions_user_id_idx ON public.admin_sessions USING btree (admin_id);

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

CREATE UNIQUE INDEX default_labels_name_idx ON public.default_labels USING btree (lower(name));

CREATE UNIQUE INDEX default_roles_name_idx ON public.default_roles USING btree (lower(name));

CREATE UNIQUE INDEX default_status_types_name_idx ON public.default_status_types USING btree (lower(name));

CREATE UNIQUE INDEX default_statuses_name_idx ON public.default_statuses USING btree (lower(name));

CREATE INDEX feedback_issue_id_idx ON public.feedback USING btree (issue_id) WHERE (issue_id IS NOT NULL);

CREATE INDEX feedback_workspace_id_id_idx ON public.feedback USING btree (workspace_id, id DESC);

CREATE UNIQUE INDEX file_folders_name ON public.file_folders USING btree (project_id, parent_id, lower(name)) NULLS NOT DISTINCT;

CREATE INDEX files_folder_id_idx ON public.files USING btree (folder_id) WHERE (folder_id IS NOT NULL);

CREATE INDEX files_issue_id_idx ON public.files USING btree (issue_id) WHERE (issue_id IS NOT NULL);

CREATE INDEX files_project_id_folder_id_idx ON public.files USING btree (project_id, folder_id) WHERE (project_id IS NOT NULL);

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

CREATE INDEX issue_templates_due ON public.issue_templates USING btree (next_on) WHERE ((repeat_every IS NOT NULL) AND (NOT paused));

CREATE INDEX issue_templates_workspace ON public.issue_templates USING btree (workspace_id);

CREATE INDEX issue_viewers_instance_idx ON public.issue_viewers USING btree (instance);

CREATE INDEX issue_viewers_issue_id_idx ON public.issue_viewers USING btree (issue_id);

CREATE INDEX issue_viewers_user_id_idx ON public.issue_viewers USING btree (user_id);

CREATE INDEX issue_watchers_user_id_idx ON public.issue_watchers USING btree (user_id);

CREATE INDEX issues_all_trash ON public.issues_all USING btree (workspace_id, deleted_at) WHERE (deleted_at IS NOT NULL);

CREATE INDEX issues_created_at_idx ON public.issues_all USING btree (created_at);

CREATE INDEX issues_created_by_idx ON public.issues_all USING btree (created_by);

CREATE INDEX issues_cycle_id_idx ON public.issues_all USING btree (cycle_id);

CREATE INDEX issues_description_text_idx ON public.issues_all USING gin (description_text public.gin_trgm_ops);

CREATE INDEX issues_parent_id_idx ON public.issues_all USING btree (parent_id);

CREATE INDEX issues_status_id_idx ON public.issues_all USING btree (status_id);

CREATE INDEX issues_title_idx ON public.issues_all USING gin (title public.gin_trgm_ops);

CREATE INDEX issues_workspace_id_assignee_id_idx ON public.issues_all USING btree (workspace_id, assignee_id);

CREATE UNIQUE INDEX labels_workspace_id_lower_idx ON public.labels USING btree (workspace_id, lower(name));

CREATE INDEX message_reactions_user_id_idx ON public.message_reactions USING btree (user_id);

CREATE INDEX messages_author_id_idx ON public.messages USING btree (author_id);

CREATE INDEX messages_chat_id_id_idx ON public.messages USING btree (chat_id, id);

CREATE INDEX messages_parent_id_id_idx ON public.messages USING btree (parent_id, id) WHERE (parent_id IS NOT NULL);

CREATE INDEX messages_pinned_idx ON public.messages USING btree (chat_id) WHERE (pinned_until IS NOT NULL);

CREATE INDEX note_doc_updates_note_id_id_idx ON public.note_doc_updates USING btree (note_id, id);

CREATE INDEX note_members_user ON public.note_members USING btree (user_id);

CREATE INDEX notes_workspace ON public.notes USING btree (workspace_id);

CREATE INDEX notifications_actor_id_idx ON public.notifications USING btree (actor_id);

CREATE INDEX notifications_chat_id_idx ON public.notifications USING btree (chat_id);

CREATE INDEX notifications_created_at_idx ON public.notifications USING btree (created_at) WHERE (emailed_at IS NULL);

CREATE INDEX notifications_invite_id_idx ON public.notifications USING btree (invite_id) WHERE (invite_id IS NOT NULL);

CREATE INDEX notifications_issue_id_idx ON public.notifications USING btree (issue_id);

CREATE INDEX notifications_issue_id_kind_idx ON public.notifications USING btree (issue_id, kind) WHERE (kind = ANY (ARRAY['due_soon'::text, 'overdue'::text]));

CREATE INDEX notifications_message_id_idx ON public.notifications USING btree (message_id);

CREATE INDEX notifications_project_id_idx ON public.notifications USING btree (project_id) WHERE (project_id IS NOT NULL);

CREATE INDEX notifications_user_id_workspace_id_id_idx ON public.notifications USING btree (user_id, workspace_id, id DESC);

CREATE INDEX notifications_workspace_id_idx ON public.notifications USING btree (workspace_id);

CREATE INDEX oauth_codes_client_id ON public.oauth_codes USING btree (client_id);

CREATE INDEX outbound_mail_next_attempt_at_idx ON public.outbound_mail USING btree (next_attempt_at) WHERE (failed_at IS NULL);

CREATE INDEX paddle_adjustments_created_at_idx ON public.paddle_adjustments USING btree (created_at);

CREATE INDEX paddle_subscriptions_user_id_idx ON public.paddle_subscriptions USING btree (user_id);

CREATE INDEX paddle_transactions_billed_at_idx ON public.paddle_transactions USING btree (billed_at);

CREATE INDEX project_key_aliases_project_id_idx ON public.project_key_aliases USING btree (project_id);

CREATE INDEX project_members_user_id_idx ON public.project_members USING btree (user_id);

CREATE INDEX project_milestones_project_id_date_idx ON public.project_milestones USING btree (project_id, date);

CREATE INDEX project_updates_project ON public.project_updates USING btree (project_id, created_at DESC);

CREATE INDEX projects_created_by_idx ON public.projects USING btree (created_by);

CREATE INDEX projects_name_idx ON public.projects USING gin (name public.gin_trgm_ops);

CREATE INDEX rate_limits_at_idx ON public.rate_limits USING btree (at);

CREATE INDEX realtime_conns_instance_idx ON public.realtime_conns USING btree (instance);

CREATE INDEX realtime_conns_user_id_idx ON public.realtime_conns USING btree (user_id);

CREATE INDEX refresh_tokens_created_at_idx ON public.refresh_tokens USING btree (created_at);

CREATE INDEX refresh_tokens_family_id_idx ON public.refresh_tokens USING btree (family_id);

CREATE INDEX saved_views_workspace ON public.saved_views USING btree (workspace_id);

CREATE INDEX sessions_user_id_idx ON public.refresh_tokens USING btree (user_id);

CREATE UNIQUE INDEX status_types_workspace_id_lower_idx ON public.status_types USING btree (workspace_id, lower(name));

CREATE INDEX statuses_type_id_idx ON public.statuses USING btree (type_id);

CREATE UNIQUE INDEX statuses_workspace_id_lower_idx ON public.statuses USING btree (workspace_id, lower(name));

CREATE INDEX test_accounts_site_id_idx ON public.test_accounts USING btree (site_id);

CREATE INDEX test_cases_created_by_idx ON public.test_cases USING btree (created_by);

CREATE INDEX test_cases_issue_id_position_idx ON public.test_cases USING btree (issue_id, "position") WHERE (deleted_at IS NULL);

CREATE INDEX test_results_case_id_marked_at_idx ON public.test_results USING btree (case_id, marked_at DESC);

CREATE INDEX test_results_marked_by_idx ON public.test_results USING btree (marked_by);

CREATE INDEX test_results_video_id_idx ON public.test_results USING btree (video_id) WHERE (video_id IS NOT NULL);

CREATE INDEX test_runs_issue_id_created_at_idx ON public.test_runs USING btree (issue_id, created_at DESC);

CREATE INDEX test_runs_started_by_idx ON public.test_runs USING btree (started_by);

CREATE INDEX test_runs_status_created_at_idx ON public.test_runs USING btree (status, created_at) WHERE (status = 'queued'::text);

CREATE INDEX test_runs_workspace_id_idx ON public.test_runs USING btree (workspace_id) WHERE (status = ANY (ARRAY['generating'::text, 'running'::text]));

CREATE INDEX test_steps_result_id_position_idx ON public.test_steps USING btree (result_id, "position");

CREATE INDEX test_steps_screenshot_id_idx ON public.test_steps USING btree (screenshot_id);

CREATE INDEX uploads_created_at_idx ON public.uploads USING btree (created_at);

CREATE INDEX users_avatar_file_id_idx ON public.users USING btree (avatar_file_id);

CREATE INDEX users_created_at_idx ON public.users USING btree (created_at);

CREATE INDEX users_plan_comped_until ON public.users USING btree (plan_comped_until) WHERE (plan_comped_until IS NOT NULL);

CREATE INDEX webhook_deliveries_next_at_idx ON public.webhook_deliveries USING btree (next_at) WHERE (status = 'pending'::text);

CREATE INDEX webhook_deliveries_webhook_id_id_idx ON public.webhook_deliveries USING btree (webhook_id, id DESC);

CREATE INDEX webhooks_workspace_id_idx ON public.webhooks USING btree (workspace_id);

CREATE INDEX workspace_audit_workspace_idx ON public.workspace_audit USING btree (workspace_id, id DESC);

CREATE INDEX workspace_invites_invited_by_idx ON public.workspace_invites USING btree (invited_by);

CREATE INDEX workspace_invites_prev_token_hash_idx ON public.workspace_invites USING btree (prev_token_hash) WHERE (prev_token_hash IS NOT NULL);

CREATE INDEX workspace_invites_role_id_idx ON public.workspace_invites USING btree (role_id);

CREATE INDEX workspace_members_role_id_idx ON public.workspace_members USING btree (role_id);

CREATE INDEX workspace_members_user_id_last_opened_at_idx ON public.workspace_members USING btree (user_id, last_opened_at DESC);

CREATE UNIQUE INDEX workspace_roles_workspace_id_lower_idx ON public.workspace_roles USING btree (workspace_id, lower(name));

CREATE INDEX workspaces_created_by_idx ON public.workspaces USING btree (created_by);

CREATE TRIGGER admin_audit_no_change BEFORE DELETE OR UPDATE ON public.admin_audit FOR EACH ROW EXECUTE FUNCTION public.admin_audit_append_only();

CREATE TRIGGER admin_audit_no_truncate BEFORE TRUNCATE ON public.admin_audit FOR EACH STATEMENT EXECUTE FUNCTION public.admin_audit_append_only();

CREATE TRIGGER project_key_alias AFTER INSERT OR UPDATE OF key ON public.projects FOR EACH ROW EXECUTE FUNCTION public.project_key_alias();

CREATE TRIGGER seed_statuses AFTER INSERT ON public.workspaces FOR EACH ROW EXECUTE FUNCTION public.seed_statuses();

CREATE TRIGGER webhook_comment AFTER INSERT ON public.comments FOR EACH ROW EXECUTE FUNCTION public.webhook_comment();

CREATE TRIGGER webhook_issue_deleted AFTER DELETE ON public.issues_all FOR EACH ROW WHEN ((old.deleted_at IS NULL)) EXECUTE FUNCTION public.webhook_issue_deleted();

CREATE TRIGGER webhook_issue_event AFTER INSERT ON public.issue_events FOR EACH ROW EXECUTE FUNCTION public.webhook_issue_event();

CREATE TRIGGER webhook_issue_trashed AFTER UPDATE OF deleted_at ON public.issues_all FOR EACH ROW WHEN (((old.deleted_at IS NULL) AND (new.deleted_at IS NOT NULL))) EXECUTE FUNCTION public.webhook_issue_deleted();

CREATE TRIGGER workspace_audit_no_change BEFORE DELETE OR UPDATE ON public.workspace_audit FOR EACH ROW EXECUTE FUNCTION public.workspace_audit_append_only();

CREATE TRIGGER workspace_audit_no_truncate BEFORE TRUNCATE ON public.workspace_audit FOR EACH STATEMENT EXECUTE FUNCTION public.workspace_audit_append_only();
