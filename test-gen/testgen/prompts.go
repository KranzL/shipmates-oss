package testgen

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	funcPattern       = regexp.MustCompile(`^\s*(export\s+)?(async\s+)?function\s+(\w+)|^\s*const\s+(\w+)\s*=\s*(async\s+)?\(|^\s*(\w+)\s*\([^)]*\)\s*\{|^func\s+(\w+)\(|^func\s+\([^)]*\)\s+(\w+)\(`)
	funcNamePattern   = regexp.MustCompile(`function\s+(\w+)`)
	constNamePattern  = regexp.MustCompile(`const\s+(\w+)\s*=`)
	arrowNamePattern  = regexp.MustCompile(`^\s*(\w+)\s*\(`)
	goFuncPattern     = regexp.MustCompile(`^func\s+(\w+)\(`)
	goMethodPattern   = regexp.MustCompile(`^func\s+\([^)]*\)\s+(\w+)\(`)
)

func BuildTestGenSystemPrompt(framework TestFramework) string {
	var builder strings.Builder

	builder.WriteString("You are an expert test engineer generating comprehensive test coverage.\n\n")

	builder.WriteString("OUTPUT FORMAT:\n")
	builder.WriteString("Return a JSON array of test file objects:\n")
	builder.WriteString("[\n")
	builder.WriteString("  {\n")
	builder.WriteString("    \"filePath\": \"path/to/test/file\",\n")
	builder.WriteString("    \"content\": \"full test file content\"\n")
	builder.WriteString("  }\n")
	builder.WriteString("]\n\n")

	builder.WriteString("FRAMEWORK REQUIREMENTS:\n")
	switch framework {
	case FrameworkVitest:
		builder.WriteString("- Use Vitest framework (vitest package)\n")
		builder.WriteString("- ESM imports: import { describe, it, expect } from 'vitest'\n")
		builder.WriteString("- Use describe blocks for grouping related tests\n")
		builder.WriteString("- Use it or test for individual test cases\n")
		builder.WriteString("- Use expect for assertions\n")
		builder.WriteString("- Test files should be named *.test.ts or *.spec.ts\n\n")
	case FrameworkJest:
		builder.WriteString("- Use Jest framework (jest package)\n")
		builder.WriteString("- Support both CommonJS and ESM imports\n")
		builder.WriteString("- Use describe blocks for grouping related tests\n")
		builder.WriteString("- Use it or test for individual test cases\n")
		builder.WriteString("- Use expect for assertions\n")
		builder.WriteString("- Test files should be named *.test.ts, *.test.js, *.spec.ts, or *.spec.js\n\n")
	case FrameworkGotest:
		builder.WriteString("- Use Go testing package (testing)\n")
		builder.WriteString("- Use table-driven test pattern\n")
		builder.WriteString("- Test functions must start with Test and take *testing.T\n")
		builder.WriteString("- Use t.Run for subtests\n")
		builder.WriteString("- Test files should be named *_test.go\n")
		builder.WriteString("- Tests should be in the same package as the code (package foo for foo.go)\n\n")
	}

	builder.WriteString("TEST QUALITY RULES:\n")
	builder.WriteString("- Test real behavior, not implementation details\n")
	builder.WriteString("- Focus on edge cases, error conditions, and boundary values\n")
	builder.WriteString("- Use descriptive test names that explain what is being tested\n")
	builder.WriteString("- Avoid mocks unless absolutely necessary for external dependencies\n")
	builder.WriteString("- Each test should be independent and not rely on other tests\n")
	builder.WriteString("- Aim for high branch coverage, not just line coverage\n")
	builder.WriteString("- Test both success and failure paths\n")
	builder.WriteString("- Include validation of error messages and error types\n\n")

	builder.WriteString("IMPORTANT:\n")
	builder.WriteString("- Generate complete, runnable test files\n")
	builder.WriteString("- Include all necessary imports\n")
	builder.WriteString("- Ensure test file paths match the framework conventions\n")
	builder.WriteString("- Return ONLY valid JSON, no markdown code fences or explanations\n")

	return builder.String()
}

func BuildTestGenUserPrompt(ctx PromptContext) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "## Task\n%s\n\n", ctx.Task)
	fmt.Fprintf(&builder, "## Repository\n%s\n\n", ctx.Repo)
	fmt.Fprintf(&builder, "## Test Framework\n%s\n\n", ctx.Framework)

	if ctx.Iteration > 0 {
		fmt.Fprintf(&builder, "## Iteration\nThis is iteration %d. Previous iterations did not achieve target coverage.\n\n", ctx.Iteration)
	}

	if ctx.FileTree != "" {
		builder.WriteString("## Repository Structure\n")
		truncated := truncateToBytes(ctx.FileTree, MaxFileTreeBytes)
		builder.WriteString(truncated)
		builder.WriteString("\n\n")
	}

	if len(ctx.CoverageGaps) > 0 {
		builder.WriteString("## Coverage Gaps (Top Uncovered Files)\n")
		summary := buildCoverageSummary(ctx.CoverageGaps)
		truncated := truncateToBytes(summary, MaxCoverageSummaryBytes)
		builder.WriteString(truncated)
		builder.WriteString("\n\n")
	}

	if len(ctx.UncoveredFunctionBodies) > 0 {
		builder.WriteString("## Uncovered Functions\n")
		builder.WriteString("Focus on testing these functions that currently lack coverage:\n\n")
		funcsContent := buildUncoveredFuncsContent(ctx.UncoveredFunctionBodies)
		truncated := truncateToBytes(funcsContent, MaxUncoveredFuncBytes)
		builder.WriteString(truncated)
		builder.WriteString("\n\n")
	}

	if len(ctx.ExistingTestExamples) > 0 {
		builder.WriteString("## Existing Test Examples\n")
		if ctx.Iteration <= 1 {
			builder.WriteString("Reference these existing tests for style and patterns:\n\n")
			examplesContent := buildTestExamplesContent(ctx.ExistingTestExamples)
			truncated := truncateToBytes(examplesContent, MaxTestExamplesBytes)
			builder.WriteString(truncated)
		} else {
			builder.WriteString("Existing test files (use these as style reference):\n")
			for _, ex := range ctx.ExistingTestExamples {
				fmt.Fprintf(&builder, "- %s\n", ex.FilePath)
			}
		}
		builder.WriteString("\n\n")
	}

	if ctx.FailedTestContext != "" {
		builder.WriteString("## Failed Test Context\n")
		builder.WriteString("The previous iteration generated tests that failed. Fix the issues:\n\n")
		truncated := truncateToBytes(ctx.FailedTestContext, MaxFailedTestBytes)
		builder.WriteString(truncated)
		builder.WriteString("\n\n")
	}

	builder.WriteString("Generate comprehensive tests to cover the gaps identified above.\n")

	return builder.String()
}

func ExtractFunctionBodies(filePath string, content string, ranges []LineRange) []UncoveredFunction {
	if len(ranges) == 0 {
		return nil
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return nil
	}

	var functions []UncoveredFunction

	for _, r := range ranges {
		funcName, funcStart, funcEnd := findEnclosingFunction(lines, r.Start, r.End)
		if funcName == "" {
			continue
		}

		alreadyAdded := false
		for _, f := range functions {
			if f.FunctionName == funcName && f.StartLine == funcStart {
				alreadyAdded = true
				break
			}
		}
		if alreadyAdded {
			continue
		}

		body := extractFunctionBody(lines, funcStart, funcEnd)
		functions = append(functions, UncoveredFunction{
			FilePath:     filePath,
			FunctionName: funcName,
			StartLine:    funcStart,
			EndLine:      funcEnd,
			Body:         body,
		})
	}

	return functions
}

func findEnclosingFunction(lines []string, rangeStart int, rangeEnd int) (string, int, int) {
	if rangeStart < 1 || rangeStart > len(lines) {
		return "", 0, 0
	}

	startIdx := rangeStart - 1
	for i := startIdx; i >= 0; i-- {
		if funcPattern.MatchString(lines[i]) {
			funcName := extractFunctionName(lines[i])
			funcStart := i + 1
			funcEnd := findFunctionEnd(lines, i)
			if funcEnd == 0 {
				funcEnd = len(lines)
			}
			return funcName, funcStart, funcEnd
		}
	}

	return "", 0, 0
}

func extractFunctionName(line string) string {
	if match := funcNamePattern.FindStringSubmatch(line); len(match) > 1 {
		return match[1]
	}

	if match := constNamePattern.FindStringSubmatch(line); len(match) > 1 {
		return match[1]
	}

	if match := arrowNamePattern.FindStringSubmatch(line); len(match) > 1 {
		return match[1]
	}

	if match := goFuncPattern.FindStringSubmatch(line); len(match) > 1 {
		return match[1]
	}

	if match := goMethodPattern.FindStringSubmatch(line); len(match) > 1 {
		return match[1]
	}

	return "anonymous"
}

func findFunctionEnd(lines []string, startIdx int) int {
	braceCount := 0
	inFunction := false

	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		for _, ch := range line {
			if ch == '{' {
				braceCount++
				inFunction = true
			} else if ch == '}' {
				braceCount--
				if inFunction && braceCount == 0 {
					return i + 1
				}
			}
		}
	}

	return 0
}

func extractFunctionBody(lines []string, start int, end int) string {
	if start < 1 || end < start || start > len(lines) {
		return ""
	}

	startIdx := start - 1
	endIdx := end
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	bodyLines := lines[startIdx:endIdx]
	lineCount := len(bodyLines)

	if lineCount > 30 {
		var builder strings.Builder
		for i := 0; i < 10 && i < len(bodyLines); i++ {
			builder.WriteString(bodyLines[i])
			builder.WriteString("\n")
		}
		builder.WriteString(fmt.Sprintf("\n... (%d lines omitted) ...\n\n", lineCount-15))
		startOfTail := len(bodyLines) - 5
		for i := startOfTail; i < len(bodyLines); i++ {
			builder.WriteString(bodyLines[i])
			builder.WriteString("\n")
		}
		return builder.String()
	}

	return strings.Join(bodyLines, "\n")
}

func buildCoverageSummary(gaps []*FileCoverageDetail) string {
	var builder strings.Builder

	for _, gap := range gaps {
		fmt.Fprintf(&builder, "File: %s\n", gap.Path)
		fmt.Fprintf(&builder, "Coverage: %.1f%% (%d/%d lines)\n", gap.CoveragePercent, gap.LinesCovered, gap.LinesTotal)
		if len(gap.UncoveredLineRanges) > 0 {
			builder.WriteString("Uncovered ranges: ")
			for i, r := range gap.UncoveredLineRanges {
				if i > 0 {
					builder.WriteString(", ")
				}
				if r.Start == r.End {
					fmt.Fprintf(&builder, "%d", r.Start)
				} else {
					fmt.Fprintf(&builder, "%d-%d", r.Start, r.End)
				}
			}
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

func buildUncoveredFuncsContent(funcs []UncoveredFunction) string {
	var builder strings.Builder

	for i, fn := range funcs {
		if i > 0 {
			builder.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&builder, "File: %s\n", fn.FilePath)
		fmt.Fprintf(&builder, "Function: %s (lines %d-%d)\n\n", fn.FunctionName, fn.StartLine, fn.EndLine)
		builder.WriteString("```\n")
		builder.WriteString(fn.Body)
		builder.WriteString("\n```\n")
	}

	return builder.String()
}

func buildTestExamplesContent(examples []TestExample) string {
	var builder strings.Builder

	for i, ex := range examples {
		if i > 0 {
			builder.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&builder, "File: %s\n\n", ex.FilePath)
		builder.WriteString("```\n")
		builder.WriteString(ex.Content)
		builder.WriteString("\n```\n")
	}

	return builder.String()
}

func truncateToBytes(content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}

	truncated := content[:maxBytes]
	lastNewline := strings.LastIndex(truncated, "\n")
	if lastNewline > 0 {
		truncated = truncated[:lastNewline]
	}

	return truncated + "\n\n... (truncated)\n"
}
