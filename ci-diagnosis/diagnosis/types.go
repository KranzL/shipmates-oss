package diagnosis

import "time"

type ErrorCategory string

const (
	CategoryBuild     ErrorCategory = "build"
	CategoryTest      ErrorCategory = "test"
	CategoryLint      ErrorCategory = "lint"
	CategoryTypeCheck ErrorCategory = "type_check"
	CategoryDeploy    ErrorCategory = "deploy"
	CategoryFormat    ErrorCategory = "format"
	CategoryImport    ErrorCategory = "import"
	CategoryConfig    ErrorCategory = "config"
	CategoryUnknown   ErrorCategory = "unknown"
)

var AutoFixSafeCategories = map[ErrorCategory]bool{
	CategoryLint:      true,
	CategoryFormat:    true,
	CategoryTypeCheck: true,
	CategoryImport:    true,
	CategoryConfig:    true,
}

var AutoFixBlockedFiles = map[string]bool{
	".env":                true,
	".env.local":          true,
	".env.production":     true,
	"credentials.json":    true,
	"secrets.yaml":        true,
	".github/workflows/":  true,
}

type DiagnosisRequest struct {
	UserID       string `json:"userId"`
	AgentID      string `json:"agentId"`
	AgentName    string `json:"agentName"`
	AgentRole    string `json:"agentRole"`
	Personality  string `json:"personality"`
	GithubToken  string `json:"githubToken"`
	Provider     string `json:"provider"`
	APIKey       string `json:"apiKey"`
	BaseURL      string `json:"baseUrl,omitempty"`
	ModelID      string `json:"modelId,omitempty"`
	Repo         string `json:"repo"`
	PRNumber     int    `json:"prNumber,omitempty"`
	CommitSHA    string `json:"commitSha"`
	WorkflowName string `json:"workflowName"`
	RunID        int64  `json:"runId"`
}

type DiagnosisResult struct {
	ID            string        `json:"id"`
	ErrorCategory ErrorCategory `json:"errorCategory"`
	Diagnosis     string        `json:"diagnosis"`
	RootCause     string        `json:"rootCause"`
	SuggestedFix  string        `json:"suggestedFix"`
	Confidence    float64       `json:"confidence"`
	AutoFixable   bool          `json:"autoFixable"`
	InputTokens   int           `json:"inputTokens"`
	OutputTokens  int           `json:"outputTokens"`
}

type StoredDiagnosis struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	AgentID       string    `json:"agentId"`
	Repo          string    `json:"repo"`
	PRNumber      int       `json:"prNumber,omitempty"`
	CommitSHA     string    `json:"commitSha"`
	WorkflowName  string    `json:"workflowName"`
	RunID         int64     `json:"runId"`
	ErrorCategory string    `json:"errorCategory"`
	Diagnosis     string    `json:"diagnosis"`
	RootCause     string    `json:"rootCause"`
	Confidence    float64   `json:"confidence"`
	Status        string    `json:"status"`
	InputTokens   int       `json:"inputTokens"`
	OutputTokens  int       `json:"outputTokens"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type StoredAutoFix struct {
	ID           string    `json:"id"`
	DiagnosisID  string    `json:"diagnosisId"`
	Repo         string    `json:"repo"`
	BranchName   string    `json:"branchName"`
	PRNumber     int       `json:"prNumber,omitempty"`
	PRURL        string    `json:"prUrl,omitempty"`
	FilesChanged []string  `json:"filesChanged"`
	FixType      string    `json:"fixType"`
	Outcome      string    `json:"outcome"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type AutoFixRequest struct {
	DiagnosisID string `json:"diagnosisId"`
}
