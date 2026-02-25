CREATE TABLE IF NOT EXISTS test_gens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    task TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    iterations INTEGER DEFAULT 0,
    baseline_coverage DOUBLE PRECISION DEFAULT 0,
    final_coverage DOUBLE PRECISION DEFAULT 0,
    tests_created TEXT[] DEFAULT '{}',
    coverage_data JSONB DEFAULT '{}',
    branch_name TEXT,
    pr_url TEXT,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    output TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_test_gens_user_id ON test_gens(user_id);
CREATE INDEX idx_test_gens_repo ON test_gens(repo);
CREATE INDEX idx_test_gens_status ON test_gens(status);

CREATE TABLE IF NOT EXISTS test_gen_iterations (
    id TEXT PRIMARY KEY,
    test_gen_id TEXT NOT NULL REFERENCES test_gens(id) ON DELETE CASCADE,
    iteration INTEGER NOT NULL,
    tests_generated TEXT[] DEFAULT '{}',
    coverage_total DOUBLE PRECISION DEFAULT 0,
    coverage_delta DOUBLE PRECISION DEFAULT 0,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_test_gen_iterations_test_gen_id ON test_gen_iterations(test_gen_id);
