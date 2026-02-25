package testgen

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func RunWithCoverage(ctx context.Context, repoDir string, framework *FrameworkDetectionResult, jobID string, iteration int, relay *RelayClient) (*CoverageReport, *TestRunResult, error) {
	coverageDir, err := os.MkdirTemp("", fmt.Sprintf("coverage-%s-iter%d-*", jobID, iteration))
	if err != nil {
		return nil, nil, fmt.Errorf("create coverage temp dir: %w", err)
	}
	defer os.RemoveAll(coverageDir)

	dockerImage := getDockerImage(framework.Framework)
	containerName := fmt.Sprintf("test-gen-%s-iter%d", jobID, iteration)

	testCommand := buildTestCommand(framework, coverageDir)

	if relay != nil {
		relay.EmitEvent(RelayEvent{
			Type:      "phase",
			Phase:     "testing",
			Message:   fmt.Sprintf("Running tests (iteration %d)", iteration),
			Iteration: iteration,
		})
	}

	args := []string{
		"run",
		"--name", containerName,
		"--network=none",
		"--memory=1536m",
		"--pids-limit=128",
		"--security-opt=no-new-privileges",
		"--cap-drop=ALL",
		"-v", fmt.Sprintf("%s:/workspace:rw", repoDir),
		"-v", fmt.Sprintf("%s:/coverage:rw", coverageDir),
		"-w", "/workspace",
		dockerImage,
		"sh", "-c", testCommand,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, TestRunTimeoutSeconds*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "docker", args...)
	start := time.Now()
	output, execErr := cmd.CombinedOutput()
	duration := time.Since(start).Milliseconds()
	const maxOutputBytes = 512 * 1024
	if len(output) > maxOutputBytes {
		output = output[len(output)-maxOutputBytes:]
	}

	exitCode := 0
	if execErr != nil {
		if timeoutCtx.Err() == context.DeadlineExceeded {
			killContainer(containerName)
			removeContainer(containerName)
			return nil, nil, fmt.Errorf("test execution timed out after %ds", TestRunTimeoutSeconds)
		}
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			removeContainer(containerName)
			return nil, nil, fmt.Errorf("docker run failed: %w", execErr)
		}
	}

	removeContainer(containerName)

	testResult := &TestRunResult{
		Passed:   exitCode == 0,
		ExitCode: exitCode,
		Output:   string(output),
		Duration: duration,
	}

	coverageFile, err := findCoverageFile(coverageDir, framework.Framework)
	if err != nil {
		return nil, testResult, fmt.Errorf("find coverage file: %w", err)
	}

	var coverageReport *CoverageReport
	switch framework.Framework {
	case FrameworkVitest, FrameworkJest:
		coverageReport, err = ParseLcov(coverageFile)
	case FrameworkGotest:
		coverageReport, err = ParseGoCoverage(coverageFile)
	default:
		return nil, testResult, fmt.Errorf("unsupported framework: %s", framework.Framework)
	}

	if err != nil {
		return nil, testResult, fmt.Errorf("parse coverage: %w", err)
	}

	if relay != nil {
		relay.EmitEvent(RelayEvent{
			Type:      "coverage_measured",
			Total:     coverageReport.TotalCoverage,
			Iteration: iteration,
		})
	}

	return coverageReport, testResult, nil
}

func getDockerImage(framework TestFramework) string {
	switch framework {
	case FrameworkVitest, FrameworkJest:
		return "node:22-alpine"
	case FrameworkGotest:
		return "golang:1.24-alpine"
	default:
		return "node:22-alpine"
	}
}

func buildTestCommand(framework *FrameworkDetectionResult, coverageDir string) string {
	switch framework.Framework {
	case FrameworkVitest:
		return fmt.Sprintf("npx vitest run --coverage --coverage.reportsDirectory=/coverage")
	case FrameworkJest:
		return fmt.Sprintf("npx jest --coverage --coverageDirectory=/coverage --coverageReporters=lcov")
	case FrameworkGotest:
		return fmt.Sprintf("go test -coverprofile=/coverage/coverage.out ./...")
	default:
		return framework.CoverageCommand
	}
}

func findCoverageFile(coverageDir string, framework TestFramework) (string, error) {
	var targetFile string
	switch framework {
	case FrameworkVitest, FrameworkJest:
		targetFile = "lcov.info"
	case FrameworkGotest:
		targetFile = "coverage.out"
	default:
		return "", fmt.Errorf("unknown framework: %s", framework)
	}

	fullPath := filepath.Join(coverageDir, targetFile)
	if _, err := os.Stat(fullPath); err != nil {
		if framework == FrameworkVitest || framework == FrameworkJest {
			lcovPath := filepath.Join(coverageDir, "lcov-report", "lcov.info")
			if _, err := os.Stat(lcovPath); err == nil {
				return lcovPath, nil
			}
		}
		return "", fmt.Errorf("coverage file not found: %s", targetFile)
	}

	return fullPath, nil
}

func killContainer(name string) {
	cmd := exec.Command("docker", "kill", name)
	cmd.Run()
}

func removeContainer(name string) {
	cmd := exec.Command("docker", "rm", name)
	cmd.Run()
}
