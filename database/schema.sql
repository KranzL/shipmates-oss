CREATE TYPE "ClarificationStatus" AS ENUM ('pending', 'answered', 'expired');
CREATE TYPE "ExecutionMode" AS ENUM ('container', 'direct', 'review');
CREATE TYPE "LLMProvider" AS ENUM ('anthropic', 'openai', 'google', 'venice', 'groq', 'together', 'fireworks', 'custom');
CREATE TYPE "Platform" AS ENUM ('discord', 'slack');
CREATE TYPE "SessionStatus" AS ENUM ('pending', 'running', 'done', 'error', 'waiting_for_clarification', 'cancelling', 'cancelled', 'partial');
CREATE TYPE "UsageType" AS ENUM ('voice_minutes', 'container_seconds', 'task_count', 'llm_tokens');

CREATE TABLE users (
    id text PRIMARY KEY,
    email text NOT NULL,
    password_hash text NOT NULL,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE TABLE workspaces (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform "Platform" NOT NULL,
    platform_id text NOT NULL,
    connected_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    bot_token text,
    bot_user_id text,
    team_name text
);
CREATE UNIQUE INDEX workspaces_platform_platform_id_key ON workspaces (platform, platform_id);

CREATE TABLE api_keys (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider "LLMProvider" NOT NULL,
    api_key text NOT NULL,
    label text NOT NULL DEFAULT '',
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    base_url text,
    model_id text
);
CREATE UNIQUE INDEX api_keys_user_id_provider_key ON api_keys (user_id, provider);

CREATE TABLE agents (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name text NOT NULL,
    role text NOT NULL,
    personality text NOT NULL,
    voice_id text NOT NULL,
    scope_config jsonb NOT NULL,
    api_key_id text REFERENCES api_keys(id) ON DELETE SET NULL,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL,
    autonomy_level text NOT NULL DEFAULT 'standard',
    can_delegate_to text[],
    max_cost_per_task double precision NOT NULL DEFAULT 10.0,
    max_delegation_depth integer NOT NULL DEFAULT 0
);

CREATE TABLE github_connections (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    github_username text NOT NULL,
    github_token text NOT NULL,
    repos text[],
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    webhook_secret text,
    issue_label text DEFAULT 'shipmates'
);
CREATE UNIQUE INDEX github_connections_user_id_key ON github_connections (user_id);

CREATE TABLE agent_schedules (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id text NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name text NOT NULL,
    task text NOT NULL,
    cron_expression text NOT NULL,
    timezone text NOT NULL DEFAULT 'UTC',
    is_active boolean NOT NULL DEFAULT true,
    next_run_at timestamp(3) NOT NULL,
    last_run_at timestamp(3),
    last_status text,
    run_count integer NOT NULL DEFAULT 0,
    failure_count integer NOT NULL DEFAULT 0,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL
);
CREATE INDEX agent_schedules_agent_id_idx ON agent_schedules (agent_id);
CREATE INDEX agent_schedules_user_id_idx ON agent_schedules (user_id);
CREATE INDEX agent_schedules_is_active_next_run_at_idx ON agent_schedules (is_active, next_run_at);

CREATE TABLE sessions (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id text NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    task text NOT NULL,
    status "SessionStatus" NOT NULL DEFAULT 'pending',
    output text,
    branch_name text,
    pr_url text,
    parent_session_id text REFERENCES sessions(id) ON DELETE SET NULL,
    chain_id text,
    "position" integer NOT NULL DEFAULT 0,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL,
    execution_mode "ExecutionMode" NOT NULL DEFAULT 'container',
    finished_at timestamp(3),
    platform_thread_id text,
    platform_user_id text,
    repo text,
    schedule_id text REFERENCES agent_schedules(id) ON DELETE SET NULL,
    share_token text,
    share_token_expires_at timestamp(3),
    starred boolean NOT NULL DEFAULT false,
    started_at timestamp(3),
    coverage_data jsonb
);
CREATE UNIQUE INDEX sessions_share_token_key ON sessions (share_token);
CREATE INDEX sessions_user_id_created_at_idx ON sessions (user_id, created_at DESC);
CREATE INDEX sessions_user_id_status_idx ON sessions (user_id, status);

CREATE TABLE session_outputs (
    id text PRIMARY KEY,
    session_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    sequence integer NOT NULL,
    content text NOT NULL,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX session_outputs_session_id_sequence_key ON session_outputs (session_id, sequence);

CREATE TABLE clarifications (
    id text PRIMARY KEY,
    session_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    question text NOT NULL,
    answer text,
    status "ClarificationStatus" NOT NULL DEFAULT 'pending',
    asked_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    answered_at timestamp(3)
);
CREATE INDEX clarifications_session_id_status_idx ON clarifications (session_id, status);

CREATE TABLE conversation_history (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id text NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role text NOT NULL,
    content text NOT NULL,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE delegated_tasks (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_session_id text REFERENCES sessions(id) ON DELETE SET NULL,
    child_session_id text REFERENCES sessions(id) ON DELETE SET NULL,
    from_agent_id text NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    to_agent_id text NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    description text NOT NULL,
    priority integer NOT NULL DEFAULT 50,
    status text NOT NULL DEFAULT 'pending',
    error_message text,
    blocked_by text[],
    context jsonb NOT NULL,
    max_cost_usd double precision,
    actual_cost_usd double precision,
    claimed_at timestamp(3),
    completed_at timestamp(3),
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX delegated_tasks_child_session_id_key ON delegated_tasks (child_session_id);
CREATE UNIQUE INDEX delegated_tasks_parent_session_id_key ON delegated_tasks (parent_session_id);
CREATE INDEX delegated_tasks_child_session_id_idx ON delegated_tasks (child_session_id);
CREATE INDEX delegated_tasks_parent_session_id_idx ON delegated_tasks (parent_session_id);
CREATE INDEX delegated_tasks_status_idx ON delegated_tasks (status);
CREATE INDEX delegated_tasks_status_blocked_by_idx ON delegated_tasks (status, blocked_by);
CREATE INDEX delegated_tasks_to_agent_id_status_priority_idx ON delegated_tasks (to_agent_id, status, priority);
CREATE INDEX delegated_tasks_user_id_status_idx ON delegated_tasks (user_id, status);

CREATE TABLE delegation_events (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    from_session_id text NOT NULL,
    to_session_id text,
    from_agent_id text NOT NULL,
    to_agent_id text NOT NULL,
    action text NOT NULL,
    reason text,
    cost_usd double precision,
    "timestamp" timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX delegation_events_from_session_id_idx ON delegation_events (from_session_id);
CREATE INDEX delegation_events_user_id_timestamp_idx ON delegation_events (user_id, "timestamp");

CREATE TABLE engine_executions (
    id text PRIMARY KEY,
    session_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    provider "LLMProvider" NOT NULL,
    model_id text NOT NULL,
    strategy text NOT NULL,
    edit_format text NOT NULL,
    context_strategy text NOT NULL,
    input_tokens integer NOT NULL,
    output_tokens integer NOT NULL,
    cost_cents numeric(10,4) NOT NULL,
    latency_ms integer NOT NULL,
    retry_count integer NOT NULL DEFAULT 0,
    files_read integer NOT NULL,
    files_modified integer NOT NULL,
    task_type text,
    success boolean NOT NULL,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX engine_executions_session_id_idx ON engine_executions (session_id);

CREATE TABLE task_locks (
    id text PRIMARY KEY,
    user_id text NOT NULL,
    session_id text NOT NULL,
    agent_id text NOT NULL,
    file_path text NOT NULL,
    locked_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamp(3) NOT NULL
);
CREATE UNIQUE INDEX task_locks_user_id_file_path_key ON task_locks (user_id, file_path);
CREATE INDEX task_locks_expires_at_idx ON task_locks (expires_at);

CREATE TABLE usage_records (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type "UsageType" NOT NULL,
    quantity numeric(12,4) NOT NULL,
    metadata jsonb,
    session_id text REFERENCES sessions(id) ON DELETE SET NULL,
    recorded_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX usage_records_user_id_type_recorded_at_idx ON usage_records (user_id, type, recorded_at);

CREATE TABLE issue_pipelines (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id text NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    repo text NOT NULL,
    issue_number integer NOT NULL,
    issue_title text NOT NULL,
    issue_body text NOT NULL,
    issue_labels text[],
    status text NOT NULL DEFAULT 'pending',
    pr_number integer,
    pr_url text,
    branch_name text,
    error_message text,
    session_id text REFERENCES sessions(id) ON DELETE SET NULL,
    started_at timestamp(3),
    finished_at timestamp(3),
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL
);
CREATE INDEX issue_pipelines_repo_issue_number_idx ON issue_pipelines (repo, issue_number);
CREATE UNIQUE INDEX issue_pipelines_session_id_key ON issue_pipelines (session_id);
CREATE INDEX issue_pipelines_status_idx ON issue_pipelines (status);
CREATE INDEX issue_pipelines_user_id_idx ON issue_pipelines (user_id);

CREATE TABLE pr_reviews (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id text NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    repo text NOT NULL,
    pr_number integer NOT NULL,
    severity_counts jsonb,
    status text NOT NULL DEFAULT 'pending',
    input_tokens integer NOT NULL DEFAULT 0,
    output_tokens integer NOT NULL DEFAULT 0,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX pr_reviews_user_id_idx ON pr_reviews (user_id);

CREATE TABLE review_comments (
    id text PRIMARY KEY,
    review_id text NOT NULL REFERENCES pr_reviews(id) ON DELETE CASCADE,
    file_path text NOT NULL,
    line_number integer NOT NULL,
    severity text NOT NULL,
    category text NOT NULL,
    body text NOT NULL,
    github_comment_id bigint,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX review_comments_review_id_idx ON review_comments (review_id);

CREATE TABLE review_feedback (
    id text PRIMARY KEY,
    review_id text NOT NULL REFERENCES pr_reviews(id) ON DELETE CASCADE,
    comment_id text NOT NULL REFERENCES review_comments(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feedback_type text NOT NULL,
    reason text NOT NULL DEFAULT '',
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX review_feedback_review_id_idx ON review_feedback (review_id);

CREATE TABLE ci_diagnoses (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id text NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    repo text NOT NULL,
    pr_number integer,
    commit_sha text NOT NULL,
    workflow_name text NOT NULL,
    run_id text NOT NULL,
    error_category text,
    diagnosis text,
    root_cause text,
    confidence double precision,
    status text NOT NULL DEFAULT 'pending',
    input_tokens integer NOT NULL DEFAULT 0,
    output_tokens integer NOT NULL DEFAULT 0,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX ci_diagnoses_user_id_idx ON ci_diagnoses (user_id);

CREATE TABLE ci_auto_fixes (
    id text PRIMARY KEY,
    diagnosis_id text NOT NULL REFERENCES ci_diagnoses(id) ON DELETE CASCADE,
    repo text NOT NULL,
    branch_name text NOT NULL,
    pr_number integer,
    pr_url text,
    files_changed text[],
    fix_type text NOT NULL,
    outcome text NOT NULL DEFAULT 'pending',
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX ci_auto_fixes_diagnosis_id_idx ON ci_auto_fixes (diagnosis_id);

CREATE TABLE test_gens (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id text NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    repo text NOT NULL,
    task text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    iterations integer NOT NULL DEFAULT 0,
    baseline_coverage double precision NOT NULL DEFAULT 0,
    final_coverage double precision NOT NULL DEFAULT 0,
    tests_created text[],
    branch_name text,
    pr_url text,
    coverage_data jsonb,
    input_tokens integer NOT NULL DEFAULT 0,
    output_tokens integer NOT NULL DEFAULT 0,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX test_gens_user_id_idx ON test_gens (user_id);

CREATE TABLE test_gen_iterations (
    id text PRIMARY KEY,
    test_gen_id text NOT NULL REFERENCES test_gens(id) ON DELETE CASCADE,
    iteration integer NOT NULL,
    tests_generated text[],
    coverage_total double precision NOT NULL DEFAULT 0,
    coverage_delta double precision NOT NULL DEFAULT 0,
    input_tokens integer NOT NULL DEFAULT 0,
    output_tokens integer NOT NULL DEFAULT 0,
    created_at timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX test_gen_iterations_test_gen_id_idx ON test_gen_iterations (test_gen_id);
