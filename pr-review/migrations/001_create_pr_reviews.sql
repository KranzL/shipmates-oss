CREATE TABLE IF NOT EXISTS pr_reviews (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    severity_counts JSONB DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pr_reviews_user_id ON pr_reviews(user_id);
CREATE INDEX idx_pr_reviews_repo_pr ON pr_reviews(repo, pr_number);

CREATE TABLE IF NOT EXISTS review_comments (
    id TEXT PRIMARY KEY,
    review_id TEXT NOT NULL REFERENCES pr_reviews(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    line_number INTEGER NOT NULL,
    severity TEXT NOT NULL,
    category TEXT NOT NULL,
    body TEXT NOT NULL,
    github_comment_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_review_comments_review_id ON review_comments(review_id);

CREATE TABLE IF NOT EXISTS review_feedback (
    id TEXT PRIMARY KEY,
    review_id TEXT NOT NULL REFERENCES pr_reviews(id) ON DELETE CASCADE,
    comment_id TEXT REFERENCES review_comments(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feedback_type TEXT NOT NULL,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_review_feedback_review_id ON review_feedback(review_id);
CREATE INDEX idx_review_feedback_comment_id ON review_feedback(comment_id);
