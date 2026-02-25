package goshared

type TestFramework string

const (
	TestFrameworkVitest TestFramework = "vitest"
	TestFrameworkJest   TestFramework = "jest"
	TestFrameworkGotest TestFramework = "gotest"
)

type TestGenConfig struct {
	TargetCoverage  float64         `json:"targetCoverage"`
	MaxIterations   int             `json:"maxIterations"`
	Frameworks      []TestFramework `json:"frameworks"`
	ExcludePatterns []string        `json:"excludePatterns"`
}

type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type FileCoverageDetail struct {
	Path                 string      `json:"path"`
	LinesCovered         int         `json:"linesCovered"`
	LinesTotal           int         `json:"linesTotal"`
	CoveragePercent      float64     `json:"coveragePercent"`
	UncoveredLineRanges  []LineRange `json:"uncoveredLineRanges"`
}

type CoverageReport struct {
	TotalCoverage  float64                       `json:"totalCoverage"`
	FileCoverage   map[string]FileCoverageDetail `json:"fileCoverage"`
	UncoveredLines map[string][]int              `json:"uncoveredLines"`
}

type TestGenIteration struct {
	Iteration      int            `json:"iteration"`
	TestsGenerated []string       `json:"testsGenerated"`
	TestsPassed    bool           `json:"testsPassed"`
	CoverageReport CoverageReport `json:"coverageReport"`
	CoverageDelta  float64        `json:"coverageDelta"`
}

type TestGenResult struct {
	Iterations    []TestGenIteration `json:"iterations"`
	FinalCoverage float64            `json:"finalCoverage"`
	TestsCreated  []string           `json:"testsCreated"`
	BranchName    string             `json:"branchName"`
	PrURL         *string            `json:"prUrl"`
}

type GeneratedTest struct {
	FilePath  string        `json:"filePath"`
	Content   string        `json:"content"`
	Framework TestFramework `json:"framework"`
}

type DirectEngineEvent struct {
	Type                string             `json:"type"`
	Phase               *Phase             `json:"phase,omitempty"`
	Message             *string            `json:"message,omitempty"`
	Path                *string            `json:"path,omitempty"`
	Additions           *int               `json:"additions,omitempty"`
	Deletions           *int               `json:"deletions,omitempty"`
	Steps               []string           `json:"steps,omitempty"`
	Timeout             *int               `json:"timeout,omitempty"`
	Command             *string            `json:"command,omitempty"`
	Passed              *bool              `json:"passed,omitempty"`
	Summary             *string            `json:"summary,omitempty"`
	Files               []string           `json:"files,omitempty"`
	Content             *string            `json:"content,omitempty"`
	Recoverable         *bool              `json:"recoverable,omitempty"`
	Attempt             *int               `json:"attempt,omitempty"`
	Reason              *string            `json:"reason,omitempty"`
	Total               *float64           `json:"total,omitempty"`
	ByFile              map[string]float64 `json:"byFile,omitempty"`
	Framework           *string            `json:"framework,omitempty"`
	TargetFile          *string            `json:"targetFile,omitempty"`
	Functions           []string           `json:"functions,omitempty"`
	Delta               *float64           `json:"delta,omitempty"`
	Iteration           *int               `json:"iteration,omitempty"`
	Coverage            *float64           `json:"coverage,omitempty"`
	Target              *float64           `json:"target,omitempty"`
	Continuing          *bool              `json:"continuing,omitempty"`
	File                *string            `json:"file,omitempty"`
	UncoveredFunctions  []string           `json:"uncoveredFunctions,omitempty"`
}
