package review

import "time"

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
	SeverityNit      Severity = "nit"
)

type Category string

const (
	CategoryBug         Category = "bug"
	CategorySecurity    Category = "security"
	CategoryPerformance Category = "performance"
	CategoryStyle       Category = "style"
	CategoryLogic       Category = "logic"
	CategoryNaming      Category = "naming"
	CategoryComplexity  Category = "complexity"
	CategoryDuplication Category = "duplication"
	CategoryOther       Category = "other"
)

type ReviewRequest struct {
	UserID      string `json:"userId"`
	AgentID     string `json:"agentId"`
	AgentName   string `json:"agentName"`
	AgentRole   string `json:"agentRole"`
	Personality string `json:"personality"`
	GithubToken string `json:"githubToken"`
	Provider    string `json:"provider"`
	APIKey      string `json:"apiKey"`
	BaseURL     string `json:"baseUrl,omitempty"`
	ModelID     string `json:"modelId,omitempty"`
	Repo        string `json:"repo"`
	PRNumber    int    `json:"prNumber"`
	PRTitle     string `json:"prTitle"`
	PRBody      string `json:"prBody"`
	Action      string `json:"action"`
}

type ReviewComment struct {
	FilePath string   `json:"file"`
	Line     int      `json:"line"`
	Severity Severity `json:"severity"`
	Category Category `json:"category"`
	Body     string   `json:"body"`
}

type ReviewResult struct {
	ID             string            `json:"id"`
	Summary        string            `json:"summary"`
	Comments       []ReviewComment   `json:"comments"`
	SeverityCounts map[string]int    `json:"severityCounts"`
	InputTokens    int               `json:"inputTokens"`
	OutputTokens   int               `json:"outputTokens"`
}

type StoredReview struct {
	ID             string         `json:"id"`
	UserID         string         `json:"userId"`
	AgentID        string         `json:"agentId"`
	Repo           string         `json:"repo"`
	PRNumber       int            `json:"prNumber"`
	SeverityCounts map[string]int `json:"severityCounts"`
	Status         string         `json:"status"`
	InputTokens    int            `json:"inputTokens"`
	OutputTokens   int            `json:"outputTokens"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type StoredComment struct {
	ID              string    `json:"id"`
	ReviewID        string    `json:"reviewId"`
	FilePath        string    `json:"filePath"`
	LineNumber      int       `json:"lineNumber"`
	Severity        string    `json:"severity"`
	Category        string    `json:"category"`
	Body            string    `json:"body"`
	GithubCommentID *int64    `json:"githubCommentId,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type FeedbackRequest struct {
	CommentID    string `json:"commentId"`
	FeedbackType string `json:"feedbackType"`
	Reason       string `json:"reason,omitempty"`
}

type CalibrationData struct {
	FalsePositiveRateBySeverity map[string]float64 `json:"falsePositiveRateBySeverity"`
	FalsePositiveRateByCategory map[string]float64 `json:"falsePositiveRateByCategory"`
	TotalReviewed               int                `json:"totalReviewed"`
	TotalApproved               int                `json:"totalApproved"`
	TotalRejected               int                `json:"totalRejected"`
}
