package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type TestCommand struct {
	Command string
	Type    string
}

type TestRunOptions struct {
	RepoDir     string
	TestCommand TestCommand
	TimeoutMs   int
}

type TestRunResult struct {
	Passed            bool
	ExitCode          int
	Output            string
	Duration          time.Duration
	CoverageRawOutput string
}

type CoverageRunOptions struct {
	TestRunOptions
	CoverageCommand    string
	CoverageOutputPath string
}

var testImageMap = map[string]string{
	"npm":    "node:22-slim",
	"make":   "node:22-slim",
	"pytest": "python:3.12-slim",
	"go":     "golang:1.22-alpine",
	"cargo":  "rust:1.77-slim",
}

var allowedCommands = map[string]bool{
	"npm test":         true,
	"make test":        true,
	"python -m pytest": true,
	"go test ./...":    true,
	"cargo test":       true,
}

const defaultTestTimeout = 120 * time.Second

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

func DetectTestCommand(repoDir string) (*TestCommand, error) {
	packageJsonPath := filepath.Join(repoDir, "package.json")
	if fileExists(packageJsonPath) {
		data, err := os.ReadFile(packageJsonPath)
		if err == nil {
			if len(data) > 0 {
				return &TestCommand{Command: "npm test", Type: "npm"}, nil
			}
		}
	}

	makefilePath := filepath.Join(repoDir, "Makefile")
	if fileExists(makefilePath) {
		data, err := os.ReadFile(makefilePath)
		if err == nil {
			content := string(data)
			if len(content) > 0 {
				return &TestCommand{Command: "make test", Type: "make"}, nil
			}
		}
	}

	pytestIniPath := filepath.Join(repoDir, "pytest.ini")
	if fileExists(pytestIniPath) {
		return &TestCommand{Command: "python -m pytest", Type: "pytest"}, nil
	}

	pyprojectPath := filepath.Join(repoDir, "pyproject.toml")
	if fileExists(pyprojectPath) {
		data, err := os.ReadFile(pyprojectPath)
		if err == nil {
			content := string(data)
			if len(content) > 0 {
				return &TestCommand{Command: "python -m pytest", Type: "pytest"}, nil
			}
		}
	}

	goModPath := filepath.Join(repoDir, "go.mod")
	if fileExists(goModPath) {
		return &TestCommand{Command: "go test ./...", Type: "go"}, nil
	}

	cargoTomlPath := filepath.Join(repoDir, "Cargo.toml")
	if fileExists(cargoTomlPath) {
		return &TestCommand{Command: "cargo test", Type: "cargo"}, nil
	}

	return nil, nil
}

func RunTests(ctx context.Context, opts TestRunOptions) (*TestRunResult, error) {
	if !allowedCommands[opts.TestCommand.Command] {
		return nil, fmt.Errorf("disallowed test command: %s", opts.TestCommand.Command)
	}

	return &TestRunResult{
		Passed:   false,
		ExitCode: -1,
		Output:   "Test execution not implemented in direct LLM engine",
		Duration: 0,
	}, fmt.Errorf("test execution requires Docker integration")
}

func ParseTestOutput(output string, maxChars int) string {
	if len(output) <= maxChars {
		return output
	}
	return output[len(output)-maxChars:]
}

func RunWithCoverage(ctx context.Context, opts CoverageRunOptions) (*TestRunResult, error) {
	if !allowedCommands[opts.TestCommand.Command] {
		return nil, fmt.Errorf("disallowed test command: %s", opts.TestCommand.Command)
	}

	return &TestRunResult{
		Passed:            false,
		ExitCode:          -1,
		Output:            "Coverage execution not implemented in direct LLM engine",
		Duration:          0,
		CoverageRawOutput: "",
	}, fmt.Errorf("coverage execution requires Docker integration")
}
