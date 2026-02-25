CREATE TABLE IF NOT EXISTS ci_diagnoses (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    pr_number INTEGER,
    commit_sha TEXT NOT NULL,
    workflow_name TEXT NOT NULL,
    run_id BIGINT,
    error_category TEXT NOT NULL DEFAULT 'unknown',
    diagnosis TEXT,
    root_cause TEXT,
    confidence REAL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ci_diagnoses_user_id ON ci_diagnoses(user_id);
CREATE INDEX idx_ci_diagnoses_repo ON ci_diagnoses(repo, commit_sha);

CREATE TABLE IF NOT EXISTS ci_auto_fixes (
    id TEXT PRIMARY KEY,
    diagnosis_id TEXT NOT NULL REFERENCES ci_diagnoses(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    branch_name TEXT NOT NULL,
    pr_number INTEGER,
    pr_url TEXT,
    files_changed TEXT[] DEFAULT '{}',
    fix_type TEXT NOT NULL,
    outcome TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ci_auto_fixes_diagnosis_id ON ci_auto_fixes(diagnosis_id);
