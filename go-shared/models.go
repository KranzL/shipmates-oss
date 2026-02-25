package goshared

import (
	"encoding/json"
	"time"
)

type User struct {
	ID        string    `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

type GithubConnection struct {
	ID             string    `json:"id" db:"id"`
	UserID         string    `json:"userId" db:"user_id"`
	GithubUsername string    `json:"githubUsername" db:"github_username"`
	GithubToken    string    `json:"-" db:"github_token"`
	Repos          []string  `json:"repos" db:"repos"`
	WebhookSecret  *string   `json:"-" db:"webhook_secret"`
	IssueLabel     *string   `json:"issueLabel,omitempty" db:"issue_label"`
	CreatedAt      time.Time `json:"createdAt" db:"created_at"`
}

type ApiKey struct {
	ID        string      `json:"id" db:"id"`
	UserID    string      `json:"userId" db:"user_id"`
	Provider  LLMProvider `json:"provider" db:"provider"`
	APIKey    string      `json:"-" db:"api_key"`
	Label     string      `json:"label" db:"label"`
	BaseURL   *string     `json:"baseUrl,omitempty" db:"base_url"`
	ModelID   *string     `json:"modelId,omitempty" db:"model_id"`
	CreatedAt time.Time   `json:"createdAt" db:"created_at"`
}

type Agent struct {
	ID                 string          `json:"id" db:"id"`
	UserID             string          `json:"userId" db:"user_id"`
	Name               string          `json:"name" db:"name"`
	Role               string          `json:"role" db:"role"`
	Personality        string          `json:"personality" db:"personality"`
	VoiceID            string          `json:"voiceId" db:"voice_id"`
	ScopeConfig        json.RawMessage `json:"scopeConfig" db:"scope_config"`
	APIKeyID           *string         `json:"apiKeyId,omitempty" db:"api_key_id"`
	CanDelegateTo      []string        `json:"canDelegateTo" db:"can_delegate_to"`
	MaxCostPerTask     float64         `json:"maxCostPerTask" db:"max_cost_per_task"`
	MaxDelegationDepth int             `json:"maxDelegationDepth" db:"max_delegation_depth"`
	AutonomyLevel      string          `json:"autonomyLevel" db:"autonomy_level"`
	CreatedAt          time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt          time.Time       `json:"updatedAt" db:"updated_at"`
}

type Workspace struct {
	ID          string    `json:"id" db:"id"`
	UserID      string    `json:"userId" db:"user_id"`
	Platform    Platform  `json:"platform" db:"platform"`
	PlatformID  string    `json:"platformId" db:"platform_id"`
	BotToken    *string   `json:"-" db:"bot_token"`
	BotUserID   *string   `json:"botUserId,omitempty" db:"bot_user_id"`
	TeamName    *string   `json:"teamName,omitempty" db:"team_name"`
	ConnectedAt time.Time `json:"connectedAt" db:"connected_at"`
}

type ConversationHistory struct {
	ID          string    `json:"id" db:"id"`
	WorkspaceID string    `json:"workspaceId" db:"workspace_id"`
	AgentID     string    `json:"agentId" db:"agent_id"`
	Role        string    `json:"role" db:"role"`
	Content     string    `json:"content" db:"content"`
	CreatedAt   time.Time `json:"createdAt" db:"created_at"`
}

type Session struct {
	ID                  string          `json:"id" db:"id"`
	UserID              string          `json:"userId" db:"user_id"`
	AgentID             string          `json:"agentId" db:"agent_id"`
	Task                string          `json:"task" db:"task"`
	Status              SessionStatus   `json:"status" db:"status"`
	ExecutionMode       ExecutionMode   `json:"executionMode" db:"execution_mode"`
	Output              *string         `json:"output,omitempty" db:"output"`
	BranchName          *string         `json:"branchName,omitempty" db:"branch_name"`
	PrURL               *string         `json:"prUrl,omitempty" db:"pr_url"`
	Repo                *string         `json:"repo,omitempty" db:"repo"`
	ParentSessionID     *string         `json:"parentSessionId,omitempty" db:"parent_session_id"`
	ChainID             *string         `json:"chainId,omitempty" db:"chain_id"`
	Position            int             `json:"position" db:"position"`
	StartedAt           *time.Time      `json:"startedAt,omitempty" db:"started_at"`
	FinishedAt          *time.Time      `json:"finishedAt,omitempty" db:"finished_at"`
	ShareToken          *string         `json:"shareToken,omitempty" db:"share_token"`
	ShareTokenExpiresAt *time.Time      `json:"shareTokenExpiresAt,omitempty" db:"share_token_expires_at"`
	CoverageData        json.RawMessage `json:"coverageData,omitempty" db:"coverage_data"`
	Starred             bool            `json:"starred" db:"starred"`
	PlatformThreadID    *string         `json:"platformThreadId,omitempty" db:"platform_thread_id"`
	PlatformUserID      *string         `json:"platformUserId,omitempty" db:"platform_user_id"`
	ScheduleID          *string         `json:"scheduleId,omitempty" db:"schedule_id"`
	CreatedAt           time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt           time.Time       `json:"updatedAt" db:"updated_at"`
}

type UsageRecord struct {
	ID         string          `json:"id" db:"id"`
	UserID     string          `json:"userId" db:"user_id"`
	Type       UsageType       `json:"type" db:"type"`
	Quantity   float64         `json:"quantity" db:"quantity"`
	Metadata   json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	SessionID  *string         `json:"sessionId,omitempty" db:"session_id"`
	RecordedAt time.Time       `json:"recordedAt" db:"recorded_at"`
}

type SessionOutput struct {
	ID        string    `json:"id" db:"id"`
	SessionID string    `json:"sessionId" db:"session_id"`
	Sequence  int       `json:"sequence" db:"sequence"`
	Content   string    `json:"content" db:"content"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

type Clarification struct {
	ID         string              `json:"id" db:"id"`
	SessionID  string              `json:"sessionId" db:"session_id"`
	Question   string              `json:"question" db:"question"`
	Answer     *string             `json:"answer,omitempty" db:"answer"`
	Status     ClarificationStatus `json:"status" db:"status"`
	AskedAt    time.Time           `json:"askedAt" db:"asked_at"`
	AnsweredAt *time.Time          `json:"answeredAt,omitempty" db:"answered_at"`
}

type EngineExecution struct {
	ID              string      `json:"id" db:"id"`
	SessionID       string      `json:"sessionId" db:"session_id"`
	Provider        LLMProvider `json:"provider" db:"provider"`
	ModelID         string      `json:"modelId" db:"model_id"`
	Strategy        string      `json:"strategy" db:"strategy"`
	EditFormat      string      `json:"editFormat" db:"edit_format"`
	ContextStrategy string      `json:"contextStrategy" db:"context_strategy"`
	InputTokens     int         `json:"inputTokens" db:"input_tokens"`
	OutputTokens    int         `json:"outputTokens" db:"output_tokens"`
	CostCents       float64     `json:"costCents" db:"cost_cents"`
	LatencyMs       int         `json:"latencyMs" db:"latency_ms"`
	RetryCount      int         `json:"retryCount" db:"retry_count"`
	FilesRead       int         `json:"filesRead" db:"files_read"`
	FilesModified   int         `json:"filesModified" db:"files_modified"`
	TaskType        *string     `json:"taskType,omitempty" db:"task_type"`
	Success         bool        `json:"success" db:"success"`
	CreatedAt       time.Time   `json:"createdAt" db:"created_at"`
}

type AgentSchedule struct {
	ID             string     `json:"id" db:"id"`
	UserID         string     `json:"userId" db:"user_id"`
	AgentID        string     `json:"agentId" db:"agent_id"`
	Name           string     `json:"name" db:"name"`
	Task           string     `json:"task" db:"task"`
	CronExpression string     `json:"cronExpression" db:"cron_expression"`
	Timezone       string     `json:"timezone" db:"timezone"`
	IsActive       bool       `json:"isActive" db:"is_active"`
	NextRunAt      time.Time  `json:"nextRunAt" db:"next_run_at"`
	LastRunAt      *time.Time `json:"lastRunAt,omitempty" db:"last_run_at"`
	LastStatus     *string    `json:"lastStatus,omitempty" db:"last_status"`
	RunCount       int        `json:"runCount" db:"run_count"`
	FailureCount   int        `json:"failureCount" db:"failure_count"`
	CreatedAt      time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt      time.Time  `json:"updatedAt" db:"updated_at"`
}

type IssuePipeline struct {
	ID           string     `json:"id" db:"id"`
	UserID       string     `json:"userId" db:"user_id"`
	AgentID      string     `json:"agentId" db:"agent_id"`
	Repo         string     `json:"repo" db:"repo"`
	IssueNumber  int        `json:"issueNumber" db:"issue_number"`
	IssueTitle   string     `json:"issueTitle" db:"issue_title"`
	IssueBody    string     `json:"issueBody" db:"issue_body"`
	IssueLabels  []string   `json:"issueLabels" db:"issue_labels"`
	Status       string     `json:"status" db:"status"`
	PrNumber     *int       `json:"prNumber,omitempty" db:"pr_number"`
	PrURL        *string    `json:"prUrl,omitempty" db:"pr_url"`
	BranchName   *string    `json:"branchName,omitempty" db:"branch_name"`
	ErrorMessage *string    `json:"errorMessage,omitempty" db:"error_message"`
	SessionID    *string    `json:"sessionId,omitempty" db:"session_id"`
	StartedAt    *time.Time `json:"startedAt,omitempty" db:"started_at"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty" db:"finished_at"`
	CreatedAt    time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time  `json:"updatedAt" db:"updated_at"`
}

type IssuePipelineWithAgent struct {
	IssuePipeline
	AgentName string `json:"agentName" db:"agent_name"`
}

type DelegatedTask struct {
	ID              string          `json:"id" db:"id"`
	UserID          string          `json:"userId" db:"user_id"`
	ParentSessionID *string         `json:"parentSessionId,omitempty" db:"parent_session_id"`
	ChildSessionID  *string         `json:"childSessionId,omitempty" db:"child_session_id"`
	FromAgentID     string          `json:"fromAgentId" db:"from_agent_id"`
	ToAgentID       string          `json:"toAgentId" db:"to_agent_id"`
	Description     string          `json:"description" db:"description"`
	Priority        int             `json:"priority" db:"priority"`
	Status          string          `json:"status" db:"status"`
	ErrorMessage    *string         `json:"errorMessage,omitempty" db:"error_message"`
	BlockedBy       []string        `json:"blockedBy" db:"blocked_by"`
	Context         json.RawMessage `json:"context" db:"context"`
	MaxCostUSD      *float64        `json:"maxCostUsd,omitempty" db:"max_cost_usd"`
	ActualCostUSD   *float64        `json:"actualCostUsd,omitempty" db:"actual_cost_usd"`
	ClaimedAt       *time.Time      `json:"claimedAt,omitempty" db:"claimed_at"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty" db:"completed_at"`
	CreatedAt       time.Time       `json:"createdAt" db:"created_at"`
}

type DelegatedTaskListItem struct {
	DelegatedTask
	FromAgentName       string  `json:"fromAgentName" db:"from_agent_name"`
	FromAgentRole       string  `json:"fromAgentRole" db:"from_agent_role"`
	ToAgentName         string  `json:"toAgentName" db:"to_agent_name"`
	ToAgentRole         string  `json:"toAgentRole" db:"to_agent_role"`
	IsBlocked           bool    `json:"isBlocked"`
	DurationMs          *int64  `json:"durationMs"`
	ParentSessionTask   *string `json:"parentSessionTask"`
	ParentSessionStatus *string `json:"parentSessionStatus"`
	ChildSessionTask    *string `json:"childSessionTask"`
	ChildSessionStatus  *string `json:"childSessionStatus"`
	ChildBranchName     *string `json:"childBranchName"`
	ChildPrURL          *string `json:"childPrUrl"`
}

type TaskLock struct {
	ID        string    `json:"id" db:"id"`
	UserID    string    `json:"userId" db:"user_id"`
	SessionID string    `json:"sessionId" db:"session_id"`
	AgentID   string    `json:"agentId" db:"agent_id"`
	FilePath  string    `json:"filePath" db:"file_path"`
	LockedAt  time.Time `json:"lockedAt" db:"locked_at"`
	ExpiresAt time.Time `json:"expiresAt" db:"expires_at"`
}

type DelegationEvent struct {
	ID            string    `json:"id" db:"id"`
	UserID        string    `json:"userId" db:"user_id"`
	FromSessionID string    `json:"fromSessionId" db:"from_session_id"`
	ToSessionID   *string   `json:"toSessionId,omitempty" db:"to_session_id"`
	FromAgentID   string    `json:"fromAgentId" db:"from_agent_id"`
	ToAgentID     string    `json:"toAgentId" db:"to_agent_id"`
	Action        string    `json:"action" db:"action"`
	Reason        *string   `json:"reason,omitempty" db:"reason"`
	CostUSD       *float64  `json:"costUsd,omitempty" db:"cost_usd"`
	Timestamp     time.Time `json:"timestamp" db:"timestamp"`
}

type SessionWithAgent struct {
	Session
	AgentName string `json:"agentName" db:"agent_name"`
}
