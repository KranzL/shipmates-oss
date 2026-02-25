package goshared

type LLMProvider string

const (
	LLMProviderAnthropic LLMProvider = "anthropic"
	LLMProviderOpenAI    LLMProvider = "openai"
	LLMProviderGoogle    LLMProvider = "google"
	LLMProviderVenice    LLMProvider = "venice"
	LLMProviderGroq      LLMProvider = "groq"
	LLMProviderTogether  LLMProvider = "together"
	LLMProviderFireworks LLMProvider = "fireworks"
	LLMProviderCustom    LLMProvider = "custom"
)

type Platform string

const (
	PlatformDiscord Platform = "discord"
	PlatformSlack   Platform = "slack"
)

type SessionStatus string

const (
	SessionStatusPending                 SessionStatus = "pending"
	SessionStatusRunning                 SessionStatus = "running"
	SessionStatusDone                    SessionStatus = "done"
	SessionStatusError                   SessionStatus = "error"
	SessionStatusWaitingForClarification SessionStatus = "waiting_for_clarification"
	SessionStatusCancelling              SessionStatus = "cancelling"
	SessionStatusCancelled               SessionStatus = "cancelled"
	SessionStatusPartial                 SessionStatus = "partial"
)

type UsageType string

const (
	UsageTypeVoiceMinutes     UsageType = "voice_minutes"
	UsageTypeContainerSeconds UsageType = "container_seconds"
	UsageTypeTaskCount        UsageType = "task_count"
	UsageTypeLLMTokens        UsageType = "llm_tokens"
)

type ClarificationStatus string

const (
	ClarificationStatusPending  ClarificationStatus = "pending"
	ClarificationStatusAnswered ClarificationStatus = "answered"
	ClarificationStatusExpired  ClarificationStatus = "expired"
)

type ExecutionMode string

const (
	ExecutionModeContainer ExecutionMode = "container"
	ExecutionModeDirect    ExecutionMode = "direct"
	ExecutionModeReview    ExecutionMode = "review"
)

type Phase string

const (
	PhaseCloning     Phase = "cloning"
	PhaseReading     Phase = "reading"
	PhasePlanning    Phase = "planning"
	PhaseWriting     Phase = "writing"
	PhaseTesting     Phase = "testing"
	PhasePushing     Phase = "pushing"
	PhaseCreatingPR  Phase = "creating_pr"
	PhaseAnalyzing   Phase = "analyzing"
	PhaseGenerating  Phase = "generating"
	PhaseMeasuring   Phase = "measuring"
)

type AgentRole string

const (
	AgentRoleLead   AgentRole = "lead"
	AgentRoleSenior AgentRole = "senior"
	AgentRoleJunior AgentRole = "junior"
)

type DelegatedTaskStatus string

const (
	DelegatedTaskStatusPending   DelegatedTaskStatus = "pending"
	DelegatedTaskStatusClaimed   DelegatedTaskStatus = "claimed"
	DelegatedTaskStatusRunning   DelegatedTaskStatus = "running"
	DelegatedTaskStatusDone      DelegatedTaskStatus = "done"
	DelegatedTaskStatusFailed    DelegatedTaskStatus = "failed"
	DelegatedTaskStatusCancelled DelegatedTaskStatus = "cancelled"
)

type DelegationAction string

const (
	DelegationActionDelegated DelegationAction = "delegated"
	DelegationActionCompleted DelegationAction = "completed"
	DelegationActionFailed    DelegationAction = "failed"
	DelegationActionEscalated DelegationAction = "escalated"
	DelegationActionCancelled DelegationAction = "cancelled"
)

type DelegationContextMode string

const (
	DelegationContextModeSummary DelegationContextMode = "summary"
	DelegationContextModeDiff    DelegationContextMode = "diff"
	DelegationContextModeFull    DelegationContextMode = "full"
)

type AgentScope struct {
	Directories []string `json:"directories"`
	Languages   []string `json:"languages"`
	Repos       []string `json:"repos"`
}

type AgentConfig struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Role        string      `json:"role"`
	Personality string      `json:"personality"`
	VoiceID     string      `json:"voiceId"`
	Scope       AgentScope  `json:"scope"`
	APIKeyID    *string     `json:"apiKeyId,omitempty"`
}

type UserApiKey struct {
	Provider LLMProvider `json:"provider"`
	APIKey   string      `json:"apiKey"`
	Label    *string     `json:"label,omitempty"`
	BaseURL  *string     `json:"baseUrl,omitempty"`
	ModelID  *string     `json:"modelId,omitempty"`
}

type UserConfig struct {
	ID          string        `json:"id"`
	APIKeys     []UserApiKey  `json:"apiKeys"`
	Agents      []AgentConfig `json:"agents"`
	GithubToken *string       `json:"githubToken"`
	Repos       []string      `json:"repos"`
}

type VoiceRequest struct {
	Text        string   `json:"text"`
	UserID      string   `json:"userId"`
	WorkspaceID string   `json:"workspaceId"`
	Platform    Platform `json:"platform"`
	GuildID     string   `json:"guildId"`
}

type VoiceResponse struct {
	AgentName string `json:"agentName"`
	Text      string `json:"text"`
	Audio     []byte `json:"audio"`
}

type FileEdit struct {
	FilePath    string `json:"filePath"`
	SearchText  string `json:"searchText"`
	ReplaceText string `json:"replaceText"`
}

type NewFile struct {
	FilePath string `json:"filePath"`
	Content  string `json:"content"`
}

type SketchResult struct {
	Explanation string     `json:"explanation"`
	Edits       []FileEdit `json:"edits"`
	NewFiles    []NewFile  `json:"newFiles"`
}

type ApplyFileResult struct {
	FilePath string  `json:"filePath"`
	Success  bool    `json:"success"`
	Error    *string `json:"error,omitempty"`
}

type ApplyResult struct {
	Applied      []ApplyFileResult `json:"applied"`
	Created      []ApplyFileResult `json:"created"`
	TotalApplied int               `json:"totalApplied"`
	TotalFailed  int               `json:"totalFailed"`
}

type EngineConfig struct {
	MaxIterations       int             `json:"maxIterations"`
	MaxOutputTokens     int             `json:"maxOutputTokens"`
	TestTimeout         int             `json:"testTimeout"`
	EnableTests         bool            `json:"enableTests"`
	RequirePlanApproval bool            `json:"requirePlanApproval"`
	TestGenConfig       *TestGenConfig  `json:"testGenConfig,omitempty"`
}

type EngineRequest struct {
	Task           string        `json:"task"`
	Repo           string        `json:"repo"`
	Provider       LLMProvider   `json:"provider"`
	APIKey         string        `json:"apiKey"`
	GithubToken    string        `json:"githubToken"`
	BaseURL        *string       `json:"baseUrl,omitempty"`
	ModelID        *string       `json:"modelId,omitempty"`
	ExecutionMode  *ExecutionMode `json:"executionMode,omitempty"`
	EngineConfig   *EngineConfig `json:"engineConfig,omitempty"`
	AgentScope     *AgentScope   `json:"agentScope,omitempty"`
}

type WorkRequest struct {
	UserID         string        `json:"userId"`
	AgentID        string        `json:"agentId"`
	Task           string        `json:"task"`
	Repo           string        `json:"repo"`
	Provider       LLMProvider   `json:"provider"`
	APIKey         string        `json:"apiKey"`
	GithubToken    string        `json:"githubToken"`
	AgentScope     *AgentScope   `json:"agentScope,omitempty"`
	BaseURL        *string       `json:"baseUrl,omitempty"`
	ModelID        *string       `json:"modelId,omitempty"`
	PlatformUserID *string       `json:"platformUserId,omitempty"`
	Mode           *string       `json:"mode,omitempty"`
	EngineConfig   *EngineConfig `json:"engineConfig,omitempty"`
}

type WorkResult struct {
	SessionID  string        `json:"sessionId"`
	Status     SessionStatus `json:"status"`
	Output     string        `json:"output"`
	BranchName *string       `json:"branchName"`
	PrURL      *string       `json:"prUrl"`
}

type ScheduleConfig struct {
	ID             string  `json:"id"`
	UserID         string  `json:"userId"`
	AgentID        string  `json:"agentId"`
	Name           string  `json:"name"`
	Task           string  `json:"task"`
	CronExpression string  `json:"cronExpression"`
	Timezone       string  `json:"timezone"`
	IsActive       bool    `json:"isActive"`
	NextRunAt      string  `json:"nextRunAt"`
	LastRunAt      *string `json:"lastRunAt"`
	LastStatus     *string `json:"lastStatus"`
	RunCount       int     `json:"runCount"`
	FailureCount   int     `json:"failureCount"`
}

type ScheduleCreateRequest struct {
	AgentID        string  `json:"agentId"`
	Name           string  `json:"name"`
	Task           string  `json:"task"`
	CronExpression string  `json:"cronExpression"`
	Timezone       *string `json:"timezone,omitempty"`
}

type ScheduleUpdateRequest struct {
	Name           *string `json:"name,omitempty"`
	Task           *string `json:"task,omitempty"`
	CronExpression *string `json:"cronExpression,omitempty"`
	Timezone       *string `json:"timezone,omitempty"`
	IsActive       *bool   `json:"isActive,omitempty"`
}

type UsageSummary struct {
	UserID        string    `json:"userId"`
	Type          UsageType `json:"type"`
	TotalQuantity float64   `json:"totalQuantity"`
	StartDate     string    `json:"startDate"`
	EndDate       string    `json:"endDate"`
}

type ClarificationRequest struct {
	SessionID string `json:"sessionId"`
	Question  string `json:"question"`
}

type IssuePipelineRecord struct {
	ID           string   `json:"id"`
	UserID       string   `json:"userId"`
	AgentID      string   `json:"agentId"`
	Repo         string   `json:"repo"`
	IssueNumber  int      `json:"issueNumber"`
	IssueTitle   string   `json:"issueTitle"`
	IssueBody    string   `json:"issueBody"`
	IssueLabels  []string `json:"issueLabels"`
	Status       string   `json:"status"`
	PrNumber     *int     `json:"prNumber"`
	PrURL        *string  `json:"prUrl"`
	BranchName   *string  `json:"branchName"`
	ErrorMessage *string  `json:"errorMessage"`
	SessionID    *string  `json:"sessionId"`
	StartedAt    *string  `json:"startedAt"`
	FinishedAt   *string  `json:"finishedAt"`
	CreatedAt    string   `json:"createdAt"`
}

type DelegationContext struct {
	Summary             string   `json:"summary"`
	RelevantFiles       []string `json:"relevantFiles"`
	GitDiff             *string  `json:"gitDiff,omitempty"`
	ParentSessionOutput *string  `json:"parentSessionOutput,omitempty"`
	Repo                *string  `json:"repo,omitempty"`
}

type DelegationRequest struct {
	FromSessionID string                 `json:"fromSessionId"`
	ToAgentID     string                 `json:"toAgentId"`
	Description   string                 `json:"description"`
	Priority      *int                   `json:"priority,omitempty"`
	BlockedBy     []string               `json:"blockedBy,omitempty"`
	MaxCostUSD    *float64               `json:"maxCostUsd,omitempty"`
	ContextMode   *DelegationContextMode `json:"contextMode,omitempty"`
}

type DelegationResponse struct {
	TaskID      string              `json:"taskId"`
	Status      DelegatedTaskStatus `json:"status"`
	ToAgentID   string              `json:"toAgentId"`
	Description string              `json:"description"`
	Priority    int                 `json:"priority"`
}

type DelegationBlock struct {
	ToAgentID   string                 `json:"toAgentId"`
	Description string                 `json:"description"`
	Priority    *int                   `json:"priority,omitempty"`
	BlockedBy   []string               `json:"blockedBy,omitempty"`
	MaxCostUSD  *float64               `json:"maxCostUsd,omitempty"`
	ContextMode *DelegationContextMode `json:"contextMode,omitempty"`
}

type DelegationStats struct {
	TotalTasks         int                             `json:"totalTasks"`
	ByStatus           map[DelegatedTaskStatus]int     `json:"byStatus"`
	TotalCostUSD       float64                         `json:"totalCostUsd"`
	AverageDurationMs  *float64                        `json:"averageDurationMs"`
}

var ProviderCLIMap = map[LLMProvider]string{
	LLMProviderAnthropic: "claude",
	LLMProviderOpenAI:    "codex",
	LLMProviderGoogle:    "gemini",
	LLMProviderVenice:    "venice",
	LLMProviderGroq:      "groq",
	LLMProviderTogether:  "together",
	LLMProviderFireworks: "fireworks",
	LLMProviderCustom:    "custom",
}

var ProviderImageMap = map[LLMProvider]string{
	LLMProviderAnthropic: "shipmates/anthropic:latest",
	LLMProviderOpenAI:    "shipmates/openai:latest",
	LLMProviderGoogle:    "shipmates/google:latest",
	LLMProviderVenice:    "shipmates/openai:latest",
	LLMProviderGroq:      "shipmates/openai:latest",
	LLMProviderTogether:  "shipmates/openai:latest",
	LLMProviderFireworks: "shipmates/openai:latest",
	LLMProviderCustom:    "shipmates/openai:latest",
}

var ContainerOnlyProviders = map[LLMProvider]bool{
	LLMProviderOpenAI: true,
}

var DirectEngineProviders = map[LLMProvider]bool{
	LLMProviderAnthropic: true,
	LLMProviderGoogle:    true,
	LLMProviderVenice:    true,
	LLMProviderGroq:      true,
	LLMProviderTogether:  true,
	LLMProviderFireworks: true,
	LLMProviderCustom:    true,
}

type DelegatedTaskFilter struct {
	Status      *DelegatedTaskStatus `json:"status,omitempty"`
	FromAgentID *string              `json:"fromAgentId,omitempty"`
	ToAgentID   *string              `json:"toAgentId,omitempty"`
	SortBy      string               `json:"sortBy"`
	SortOrder   string               `json:"sortOrder"`
}

func ValidateExecutionMode(provider LLMProvider, mode ExecutionMode) bool {
	if mode == ExecutionModeContainer {
		return true
	}
	if mode == ExecutionModeDirect || mode == ExecutionModeReview {
		return DirectEngineProviders[provider]
	}
	return false
}
