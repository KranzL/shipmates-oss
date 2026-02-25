package testgen

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func DetectTestFramework(repoDir string) (*FrameworkDetectionResult, error) {
	packageJSONPath := filepath.Join(repoDir, "package.json")
	if _, err := os.Stat(packageJSONPath); err == nil {
		data, err := os.ReadFile(packageJSONPath)
		if err != nil {
			return nil, fmt.Errorf("read package.json: %w", err)
		}

		var pkg struct {
			DevDependencies map[string]string `json:"devDependencies"`
			Dependencies    map[string]string `json:"dependencies"`
		}

		if err := json.Unmarshal(data, &pkg); err != nil {
			return nil, fmt.Errorf("parse package.json: %w", err)
		}

		if _, hasVitest := pkg.DevDependencies["vitest"]; hasVitest {
			return &FrameworkDetectionResult{
				Framework:       FrameworkVitest,
				TestCommand:     "npx vitest run",
				CoverageCommand: "npx vitest run --coverage --reporter=json",
			}, nil
		}

		if _, hasVitestDep := pkg.Dependencies["vitest"]; hasVitestDep {
			return &FrameworkDetectionResult{
				Framework:       FrameworkVitest,
				TestCommand:     "npx vitest run",
				CoverageCommand: "npx vitest run --coverage --reporter=json",
			}, nil
		}

		if _, hasJest := pkg.DevDependencies["jest"]; hasJest {
			return &FrameworkDetectionResult{
				Framework:       FrameworkJest,
				TestCommand:     "npx jest",
				CoverageCommand: "npx jest --coverage --coverageReporters=lcov",
			}, nil
		}

		if _, hasJestDep := pkg.Dependencies["jest"]; hasJestDep {
			return &FrameworkDetectionResult{
				Framework:       FrameworkJest,
				TestCommand:     "npx jest",
				CoverageCommand: "npx jest --coverage --coverageReporters=lcov",
			}, nil
		}
	}

	goModPath := filepath.Join(repoDir, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		return &FrameworkDetectionResult{
			Framework:       FrameworkGotest,
			TestCommand:     "go test ./...",
			CoverageCommand: "go test -coverprofile=coverage.out ./...",
		}, nil
	}

	return nil, fmt.Errorf("no supported test framework found (checked vitest, jest, go test)")
}

func GetTestFilePattern(framework TestFramework) *regexp.Regexp {
	switch framework {
	case FrameworkVitest:
		return regexp.MustCompile(`\.test\.(ts|js|tsx|jsx)$`)
	case FrameworkJest:
		return regexp.MustCompile(`\.(test|spec)\.(ts|js|tsx|jsx)$`)
	case FrameworkGotest:
		return regexp.MustCompile(`_test\.go$`)
	default:
		return regexp.MustCompile(`_test\.go$`)
	}
}

func GetTestConventions(framework TestFramework) TestConventions {
	switch framework {
	case FrameworkVitest:
		return TestConventions{
			TestFileSuffix: ".test.ts",
			TestDir:        "",
			ImportStyle:    "esm",
		}
	case FrameworkJest:
		return TestConventions{
			TestFileSuffix: ".test.ts",
			TestDir:        "__tests__",
			ImportStyle:    "esm",
		}
	case FrameworkGotest:
		return TestConventions{
			TestFileSuffix: "_test.go",
			TestDir:        "",
			ImportStyle:    "go",
		}
	default:
		return TestConventions{
			TestFileSuffix: "_test.go",
			TestDir:        "",
			ImportStyle:    "go",
		}
	}
}

func GenerateTestFilePath(framework TestFramework, sourceFile string) string {
	conventions := GetTestConventions(framework)

	dir := filepath.Dir(sourceFile)
	base := filepath.Base(sourceFile)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)

	switch framework {
	case FrameworkVitest, FrameworkJest:
		testFileName := nameWithoutExt + ".test" + ext
		if conventions.TestDir != "" {
			return filepath.Join(dir, conventions.TestDir, testFileName)
		}
		return filepath.Join(dir, testFileName)
	case FrameworkGotest:
		testFileName := nameWithoutExt + "_test.go"
		return filepath.Join(dir, testFileName)
	default:
		testFileName := nameWithoutExt + "_test.go"
		return filepath.Join(dir, testFileName)
	}
}

func ValidateFrameworkBinary(framework TestFramework) error {
	var cmd *exec.Cmd

	switch framework {
	case FrameworkVitest:
		cmd = exec.Command("npx", "vitest", "--version")
	case FrameworkJest:
		cmd = exec.Command("npx", "jest", "--version")
	case FrameworkGotest:
		cmd = exec.Command("go", "version")
	default:
		return fmt.Errorf("unknown framework: %s", framework)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("framework binary check failed for %s: %w (output: %s)", framework, err, string(output))
	}

	return nil
}
