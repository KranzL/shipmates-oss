package testgen

import "time"

type TestFramework string

const (
	FrameworkVitest TestFramework = "vitest"
	FrameworkJest   TestFramework = "jest"
	FrameworkGotest TestFramework = "gotest"
)

type TestGenRequest struct {
	UserID      string         `json:"userId"`
	AgentID     string         `json:"agentId"`
	Task        string         `json:"task"`
	Repo        string         `json:"repo"`
	Provider    string         `json:"provider"`
	APIKey      string         `json:"apiKey"`
	GithubToken string         `json:"githubToken"`
	BaseURL     string         `json:"baseUrl,omitempty"`
	ModelID     string         `json:"modelId,omitempty"`
	SessionID   string         `json:"sessionId,omitempty"`
	Config      *TestGenConfig `json:"config,omitempty"`
}

type TestGenConfig struct {
	TargetCoverage  float64         `json:"targetCoverage"`
	MaxIterations   int             `json:"maxIterations"`
	Frameworks      []TestFramework `json:"frameworks"`
	ExcludePatterns []string        `json:"excludePatterns"`
}

type CoverageReport struct {
	TotalCoverage  float64
	FileCoverage   map[string]*FileCoverageDetail
	UncoveredLines map[string][]int
}

type FileCoverageDetail struct {
	Path                string
	LinesCovered        int
	LinesTotal          int
	CoveragePercent     float64
	UncoveredLineRanges []LineRange
}

type LineRange struct {
	Start int
	End   int
}

type GeneratedTest struct {
	FilePath  string `json:"filePath"`
	Content   string `json:"content"`
	Framework TestFramework
}

type FrameworkDetectionResult struct {
	Framework       TestFramework
	TestCommand     string
	CoverageCommand string
}

type TestGenIteration struct {
	Iteration      int
	TestsGenerated []string
	TestsPassed    bool
	CoverageReport *CoverageReport
	CoverageDelta  float64
}

type UncoveredFunction struct {
	FilePath     string
	FunctionName string
	StartLine    int
	EndLine      int
	Body         string
}

type TestExample struct {
	FilePath string
	Content  string
}

type PromptContext struct {
	Task                     string
	Repo                     string
	FileTree                 string
	Framework                TestFramework
	CoverageGaps             []*FileCoverageDetail
	UncoveredFunctionBodies  []UncoveredFunction
	ExistingTestExamples     []TestExample
	FailedTestContext        string
	Iteration                int
}

type TestGenResult struct {
	ID               string
	Iterations       int
	BaselineCoverage float64
	FinalCoverage    float64
	TestsCreated     []string
	BranchName       string
	PRURL            string
	InputTokens      int
	OutputTokens     int
}

type StoredTestGen struct {
	ID               string    `json:"id"`
	UserID           string    `json:"userId"`
	AgentID          string    `json:"agentId"`
	Repo             string    `json:"repo"`
	Task             string    `json:"task"`
	Status           string    `json:"status"`
	Iterations       int       `json:"iterations"`
	BaselineCoverage float64   `json:"baselineCoverage"`
	FinalCoverage    float64   `json:"finalCoverage"`
	TestsCreated     []string  `json:"testsCreated"`
	BranchName       string    `json:"branchName"`
	PRURL            string    `json:"prUrl"`
	InputTokens      int       `json:"inputTokens"`
	OutputTokens     int       `json:"outputTokens"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type RelayEvent struct {
	Type               string             `json:"type"`
	Phase              string             `json:"phase,omitempty"`
	Message            string             `json:"message,omitempty"`
	Total              float64            `json:"total,omitempty"`
	Delta              float64            `json:"delta,omitempty"`
	ByFile             map[string]float64 `json:"byFile,omitempty"`
	Framework          string             `json:"framework,omitempty"`
	Command            string             `json:"command,omitempty"`
	File               string             `json:"file,omitempty"`
	UncoveredFunctions []string           `json:"uncoveredFunctions,omitempty"`
	Path               string             `json:"path,omitempty"`
	TargetFile         string             `json:"targetFile,omitempty"`
	Iteration          int                `json:"iteration,omitempty"`
	Coverage           float64            `json:"coverage,omitempty"`
	Target             float64            `json:"target,omitempty"`
	Continuing         bool               `json:"continuing,omitempty"`
	Recoverable        bool               `json:"recoverable,omitempty"`
}

type CoverageGap struct {
	Path            string
	CoveragePercent float64
	UncoveredRanges []LineRange
	UncoveredCount  int
}

type TestRunResult struct {
	Passed   bool
	ExitCode int
	Output   string
	Duration int64
}

type TestConventions struct {
	TestFileSuffix string
	TestDir        string
	ImportStyle    string
}

const (
	DefaultTargetCoverage       = 80.0
	DefaultMaxIterations        = 5
	MaxOutputTokens             = 8192
	ConvergenceThreshold        = 2.0
	ConvergenceWindow           = 2
	LLMTimeoutSeconds           = 120
	TestRunTimeoutSeconds       = 120
	CloneTimeoutSeconds         = 60
	MaxCoverageFileSize         = 10 * 1024 * 1024
	MaxFilesToParse             = 500
	MaxCoverageFiles            = 50
	MaxUncoveredFunctions       = 50
	MaxTestExamples             = 5
	MaxFileTreeLines            = 200
	MaxSystemPromptBytes        = 2048
	MaxFileTreeBytes            = 5120
	MaxCoverageSummaryBytes     = 5120
	MaxUncoveredFuncBytes       = 51200
	MaxTestExamplesBytes        = 10240
	MaxFailedTestBytes          = 20480
	MaxGeneratedFilesPerIter    = 20
	MaxTotalGeneratedFiles      = 50
	MaxJobWallTime              = 15 * time.Minute
	LLMJitterFactor             = 0.2
)

var LLMRetryDelays = []int{1000, 2000, 4000}
