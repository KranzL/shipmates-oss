--
-- PostgreSQL database dump
--

-- Dumped from database version 14.18 (Homebrew)
-- Dumped by pg_dump version 14.18 (Homebrew)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: ClarificationStatus; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public."ClarificationStatus" AS ENUM (
    'pending',
    'answered',
    'expired'
);


--
-- Name: ExecutionMode; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public."ExecutionMode" AS ENUM (
    'container',
    'direct',
    'review'
);


--
-- Name: LLMProvider; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public."LLMProvider" AS ENUM (
    'anthropic',
    'openai',
    'google',
    'venice',
    'groq',
    'together',
    'fireworks',
    'custom'
);


--
-- Name: Platform; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public."Platform" AS ENUM (
    'discord',
    'slack'
);


--
-- Name: SessionStatus; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public."SessionStatus" AS ENUM (
    'pending',
    'running',
    'done',
    'error',
    'waiting_for_clarification',
    'cancelling',
    'cancelled',
    'partial'
);


--
-- Name: UsageType; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public."UsageType" AS ENUM (
    'voice_minutes',
    'container_seconds',
    'task_count',
    'llm_tokens'
);


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: _prisma_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public._prisma_migrations (
    id character varying(36) NOT NULL,
    checksum character varying(64) NOT NULL,
    finished_at timestamp with time zone,
    migration_name character varying(255) NOT NULL,
    logs text,
    rolled_back_at timestamp with time zone,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    applied_steps_count integer DEFAULT 0 NOT NULL
);


--
-- Name: agent_schedules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_schedules (
    id text NOT NULL,
    user_id text NOT NULL,
    agent_id text NOT NULL,
    name text NOT NULL,
    task text NOT NULL,
    cron_expression text NOT NULL,
    timezone text DEFAULT 'UTC'::text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    next_run_at timestamp(3) without time zone NOT NULL,
    last_run_at timestamp(3) without time zone,
    last_status text,
    run_count integer DEFAULT 0 NOT NULL,
    failure_count integer DEFAULT 0 NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp(3) without time zone NOT NULL
);


--
-- Name: agent_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agent_templates (
    id text NOT NULL,
    name text NOT NULL,
    role text NOT NULL,
    personality text NOT NULL,
    voice_id text NOT NULL,
    scope_config jsonb NOT NULL,
    category text NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp(3) without time zone NOT NULL
);


--
-- Name: agents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.agents (
    id text NOT NULL,
    user_id text NOT NULL,
    name text NOT NULL,
    role text NOT NULL,
    personality text NOT NULL,
    voice_id text NOT NULL,
    scope_config jsonb NOT NULL,
    api_key_id text,
    template_id text,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp(3) without time zone NOT NULL,
    autonomy_level text DEFAULT 'standard'::text NOT NULL,
    can_delegate_to text[],
    max_cost_per_task double precision DEFAULT 10.0 NOT NULL,
    max_delegation_depth integer DEFAULT 0 NOT NULL
);


--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id text NOT NULL,
    user_id text NOT NULL,
    provider public."LLMProvider" NOT NULL,
    api_key text NOT NULL,
    label text DEFAULT ''::text NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    base_url text,
    model_id text
);


--
-- Name: clarifications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.clarifications (
    id text NOT NULL,
    session_id text NOT NULL,
    question text NOT NULL,
    answer text,
    status public."ClarificationStatus" DEFAULT 'pending'::public."ClarificationStatus" NOT NULL,
    asked_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    answered_at timestamp(3) without time zone
);


--
-- Name: conversation_history; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversation_history (
    id text NOT NULL,
    workspace_id text NOT NULL,
    agent_id text NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: delegated_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.delegated_tasks (
    id text NOT NULL,
    user_id text NOT NULL,
    parent_session_id text,
    child_session_id text,
    from_agent_id text NOT NULL,
    to_agent_id text NOT NULL,
    description text NOT NULL,
    priority integer DEFAULT 50 NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    error_message text,
    blocked_by text[],
    context jsonb NOT NULL,
    max_cost_usd double precision,
    actual_cost_usd double precision,
    claimed_at timestamp(3) without time zone,
    completed_at timestamp(3) without time zone,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: delegation_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.delegation_events (
    id text NOT NULL,
    user_id text NOT NULL,
    from_session_id text NOT NULL,
    to_session_id text,
    from_agent_id text NOT NULL,
    to_agent_id text NOT NULL,
    action text NOT NULL,
    reason text,
    cost_usd double precision,
    "timestamp" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: engine_executions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.engine_executions (
    id text NOT NULL,
    session_id text NOT NULL,
    provider public."LLMProvider" NOT NULL,
    model_id text NOT NULL,
    strategy text NOT NULL,
    edit_format text NOT NULL,
    context_strategy text NOT NULL,
    input_tokens integer NOT NULL,
    output_tokens integer NOT NULL,
    cost_cents numeric(10,4) NOT NULL,
    latency_ms integer NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    files_read integer NOT NULL,
    files_modified integer NOT NULL,
    task_type text,
    success boolean NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: github_connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.github_connections (
    id text NOT NULL,
    user_id text NOT NULL,
    github_username text NOT NULL,
    github_token text NOT NULL,
    repos text[],
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    webhook_secret text,
    issue_label text DEFAULT 'shipmates'::text
);


--
-- Name: issue_pipelines; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.issue_pipelines (
    id text NOT NULL,
    user_id text NOT NULL,
    agent_id text NOT NULL,
    repo text NOT NULL,
    issue_number integer NOT NULL,
    issue_title text NOT NULL,
    issue_body text NOT NULL,
    issue_labels text[],
    status text DEFAULT 'pending'::text NOT NULL,
    pr_number integer,
    pr_url text,
    branch_name text,
    error_message text,
    session_id text,
    started_at timestamp(3) without time zone,
    finished_at timestamp(3) without time zone,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp(3) without time zone NOT NULL
);


--
-- Name: session_feedback; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.session_feedback (
    id text NOT NULL,
    session_id text NOT NULL,
    rating text NOT NULL,
    comment text,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: session_outputs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.session_outputs (
    id text NOT NULL,
    session_id text NOT NULL,
    sequence integer NOT NULL,
    content text NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    id text NOT NULL,
    user_id text NOT NULL,
    agent_id text NOT NULL,
    task text NOT NULL,
    status public."SessionStatus" DEFAULT 'pending'::public."SessionStatus" NOT NULL,
    output text,
    branch_name text,
    pr_url text,
    parent_session_id text,
    chain_id text,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp(3) without time zone NOT NULL,
    execution_mode public."ExecutionMode" DEFAULT 'container'::public."ExecutionMode" NOT NULL,
    finished_at timestamp(3) without time zone,
    platform_thread_id text,
    platform_user_id text,
    repo text,
    schedule_id text,
    share_token text,
    share_token_expires_at timestamp(3) without time zone,
    starred boolean DEFAULT false NOT NULL,
    started_at timestamp(3) without time zone,
    coverage_data jsonb
);


--
-- Name: task_locks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_locks (
    id text NOT NULL,
    user_id text NOT NULL,
    session_id text NOT NULL,
    agent_id text NOT NULL,
    file_path text NOT NULL,
    locked_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    expires_at timestamp(3) without time zone NOT NULL
);


--
-- Name: usage_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usage_records (
    id text NOT NULL,
    user_id text NOT NULL,
    type public."UsageType" NOT NULL,
    quantity numeric(12,4) NOT NULL,
    metadata jsonb,
    session_id text,
    recorded_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    created_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp(3) without time zone NOT NULL
);


--
-- Name: workspaces; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workspaces (
    id text NOT NULL,
    user_id text NOT NULL,
    platform public."Platform" NOT NULL,
    platform_id text NOT NULL,
    connected_at timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    bot_token text,
    bot_user_id text,
    team_name text
);


--
-- Name: _prisma_migrations _prisma_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public._prisma_migrations
    ADD CONSTRAINT _prisma_migrations_pkey PRIMARY KEY (id);


--
-- Name: agent_schedules agent_schedules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_schedules
    ADD CONSTRAINT agent_schedules_pkey PRIMARY KEY (id);


--
-- Name: agent_templates agent_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_templates
    ADD CONSTRAINT agent_templates_pkey PRIMARY KEY (id);


--
-- Name: agents agents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_pkey PRIMARY KEY (id);


--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);


--
-- Name: clarifications clarifications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.clarifications
    ADD CONSTRAINT clarifications_pkey PRIMARY KEY (id);


--
-- Name: conversation_history conversation_history_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_history
    ADD CONSTRAINT conversation_history_pkey PRIMARY KEY (id);


--
-- Name: delegated_tasks delegated_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delegated_tasks
    ADD CONSTRAINT delegated_tasks_pkey PRIMARY KEY (id);


--
-- Name: delegation_events delegation_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delegation_events
    ADD CONSTRAINT delegation_events_pkey PRIMARY KEY (id);


--
-- Name: engine_executions engine_executions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.engine_executions
    ADD CONSTRAINT engine_executions_pkey PRIMARY KEY (id);


--
-- Name: github_connections github_connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.github_connections
    ADD CONSTRAINT github_connections_pkey PRIMARY KEY (id);


--
-- Name: issue_pipelines issue_pipelines_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issue_pipelines
    ADD CONSTRAINT issue_pipelines_pkey PRIMARY KEY (id);


--
-- Name: session_feedback session_feedback_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.session_feedback
    ADD CONSTRAINT session_feedback_pkey PRIMARY KEY (id);


--
-- Name: session_outputs session_outputs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.session_outputs
    ADD CONSTRAINT session_outputs_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);


--
-- Name: task_locks task_locks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_locks
    ADD CONSTRAINT task_locks_pkey PRIMARY KEY (id);


--
-- Name: usage_records usage_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_pkey PRIMARY KEY (id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: workspaces workspaces_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);


--
-- Name: agent_schedules_agent_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_schedules_agent_id_idx ON public.agent_schedules USING btree (agent_id);


--
-- Name: agent_schedules_is_active_next_run_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_schedules_is_active_next_run_at_idx ON public.agent_schedules USING btree (is_active, next_run_at);


--
-- Name: agent_schedules_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX agent_schedules_user_id_idx ON public.agent_schedules USING btree (user_id);


--
-- Name: api_keys_user_id_provider_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX api_keys_user_id_provider_key ON public.api_keys USING btree (user_id, provider);


--
-- Name: clarifications_session_id_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX clarifications_session_id_status_idx ON public.clarifications USING btree (session_id, status);


--
-- Name: delegated_tasks_child_session_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegated_tasks_child_session_id_idx ON public.delegated_tasks USING btree (child_session_id);


--
-- Name: delegated_tasks_child_session_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX delegated_tasks_child_session_id_key ON public.delegated_tasks USING btree (child_session_id);


--
-- Name: delegated_tasks_parent_session_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegated_tasks_parent_session_id_idx ON public.delegated_tasks USING btree (parent_session_id);


--
-- Name: delegated_tasks_parent_session_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX delegated_tasks_parent_session_id_key ON public.delegated_tasks USING btree (parent_session_id);


--
-- Name: delegated_tasks_status_blocked_by_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegated_tasks_status_blocked_by_idx ON public.delegated_tasks USING btree (status, blocked_by);


--
-- Name: delegated_tasks_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegated_tasks_status_idx ON public.delegated_tasks USING btree (status);


--
-- Name: delegated_tasks_to_agent_id_status_priority_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegated_tasks_to_agent_id_status_priority_idx ON public.delegated_tasks USING btree (to_agent_id, status, priority);


--
-- Name: delegated_tasks_user_id_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegated_tasks_user_id_status_idx ON public.delegated_tasks USING btree (user_id, status);


--
-- Name: delegation_events_from_session_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegation_events_from_session_id_idx ON public.delegation_events USING btree (from_session_id);


--
-- Name: delegation_events_user_id_timestamp_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX delegation_events_user_id_timestamp_idx ON public.delegation_events USING btree (user_id, "timestamp");


--
-- Name: engine_executions_session_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX engine_executions_session_id_idx ON public.engine_executions USING btree (session_id);


--
-- Name: github_connections_user_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX github_connections_user_id_key ON public.github_connections USING btree (user_id);


--
-- Name: issue_pipelines_repo_issue_number_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX issue_pipelines_repo_issue_number_idx ON public.issue_pipelines USING btree (repo, issue_number);


--
-- Name: issue_pipelines_session_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX issue_pipelines_session_id_key ON public.issue_pipelines USING btree (session_id);


--
-- Name: issue_pipelines_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX issue_pipelines_status_idx ON public.issue_pipelines USING btree (status);


--
-- Name: issue_pipelines_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX issue_pipelines_user_id_idx ON public.issue_pipelines USING btree (user_id);


--
-- Name: session_feedback_session_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX session_feedback_session_id_key ON public.session_feedback USING btree (session_id);


--
-- Name: session_outputs_session_id_sequence_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX session_outputs_session_id_sequence_key ON public.session_outputs USING btree (session_id, sequence);


--
-- Name: sessions_share_token_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sessions_share_token_key ON public.sessions USING btree (share_token);


--
-- Name: sessions_user_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sessions_user_id_created_at_idx ON public.sessions USING btree (user_id, created_at DESC);


--
-- Name: sessions_user_id_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sessions_user_id_status_idx ON public.sessions USING btree (user_id, status);


--
-- Name: task_locks_expires_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX task_locks_expires_at_idx ON public.task_locks USING btree (expires_at);


--
-- Name: task_locks_user_id_file_path_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX task_locks_user_id_file_path_key ON public.task_locks USING btree (user_id, file_path);


--
-- Name: usage_records_user_id_type_recorded_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX usage_records_user_id_type_recorded_at_idx ON public.usage_records USING btree (user_id, type, recorded_at);


--
-- Name: users_email_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX users_email_key ON public.users USING btree (email);


--
-- Name: workspaces_platform_platform_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workspaces_platform_platform_id_key ON public.workspaces USING btree (platform, platform_id);


--
-- Name: agent_schedules agent_schedules_agent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_schedules
    ADD CONSTRAINT agent_schedules_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: agent_schedules agent_schedules_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agent_schedules
    ADD CONSTRAINT agent_schedules_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: agents agents_api_key_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES public.api_keys(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: agents agents_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: api_keys api_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: clarifications clarifications_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.clarifications
    ADD CONSTRAINT clarifications_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: conversation_history conversation_history_agent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_history
    ADD CONSTRAINT conversation_history_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: conversation_history conversation_history_workspace_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_history
    ADD CONSTRAINT conversation_history_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: delegated_tasks delegated_tasks_child_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delegated_tasks
    ADD CONSTRAINT delegated_tasks_child_session_id_fkey FOREIGN KEY (child_session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: delegated_tasks delegated_tasks_from_agent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delegated_tasks
    ADD CONSTRAINT delegated_tasks_from_agent_id_fkey FOREIGN KEY (from_agent_id) REFERENCES public.agents(id) ON UPDATE CASCADE ON DELETE RESTRICT;


--
-- Name: delegated_tasks delegated_tasks_parent_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delegated_tasks
    ADD CONSTRAINT delegated_tasks_parent_session_id_fkey FOREIGN KEY (parent_session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: delegated_tasks delegated_tasks_to_agent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delegated_tasks
    ADD CONSTRAINT delegated_tasks_to_agent_id_fkey FOREIGN KEY (to_agent_id) REFERENCES public.agents(id) ON UPDATE CASCADE ON DELETE RESTRICT;


--
-- Name: delegated_tasks delegated_tasks_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.delegated_tasks
    ADD CONSTRAINT delegated_tasks_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: engine_executions engine_executions_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.engine_executions
    ADD CONSTRAINT engine_executions_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: github_connections github_connections_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.github_connections
    ADD CONSTRAINT github_connections_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: issue_pipelines issue_pipelines_agent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issue_pipelines
    ADD CONSTRAINT issue_pipelines_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: issue_pipelines issue_pipelines_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issue_pipelines
    ADD CONSTRAINT issue_pipelines_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: issue_pipelines issue_pipelines_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.issue_pipelines
    ADD CONSTRAINT issue_pipelines_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: session_feedback session_feedback_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.session_feedback
    ADD CONSTRAINT session_feedback_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: session_outputs session_outputs_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.session_outputs
    ADD CONSTRAINT session_outputs_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: sessions sessions_agent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: sessions sessions_parent_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_parent_session_id_fkey FOREIGN KEY (parent_session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: sessions sessions_schedule_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES public.agent_schedules(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: sessions sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: usage_records usage_records_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.sessions(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: usage_records usage_records_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: workspaces workspaces_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

