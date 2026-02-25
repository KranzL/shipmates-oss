package diagnosis

import (
	"regexp"
	"strings"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func StripANSI(input string) string {
	return ansiRegex.ReplaceAllString(input, "")
}

var errorPatterns = []string{
	"error:",
	"Error:",
	"ERROR:",
	"FAIL",
	"FAILED",
	"fatal:",
	"Fatal:",
	"FATAL:",
	"panic:",
	"exception:",
	"Exception:",
	"TypeError:",
	"SyntaxError:",
	"ReferenceError:",
	"CompileError:",
	"Build failed",
	"build failed",
	"test failed",
	"Test failed",
	"npm ERR!",
	"exit code 1",
	"exit status 1",
	"command failed",
}

func ExtractErrorSections(logs map[string]string, maxChars int) string {
	var sections []string
	totalChars := 0

	for name, content := range logs {
		cleaned := StripANSI(content)
		lines := strings.Split(cleaned, "\n")

		var errorLines []string
		included := make(map[int]bool)
		for i, line := range lines {
			isError := false
			for _, pattern := range errorPatterns {
				if strings.Contains(line, pattern) {
					isError = true
					break
				}
			}

			if isError {
				start := i - 5
				if start < 0 {
					start = 0
				}
				end := i + 10
				if end > len(lines) {
					end = len(lines)
				}

				for j := start; j < end; j++ {
					if !included[j] {
						errorLines = append(errorLines, lines[j])
						included[j] = true
					}
				}
				errorLines = append(errorLines, "---")
			}
		}

		if len(errorLines) > 0 {
			section := "=== " + name + " ===\n" + strings.Join(errorLines, "\n")
			sectionLen := len(section)

			if totalChars+sectionLen > maxChars {
				remaining := maxChars - totalChars
				if remaining > 100 {
					section = section[:remaining] + "\n... (truncated)"
					sections = append(sections, section)
				}
				break
			}

			sections = append(sections, section)
			totalChars += sectionLen
		}
	}

	if len(sections) == 0 {
		for name, content := range logs {
			cleaned := StripANSI(content)
			lines := strings.Split(cleaned, "\n")
			lastN := 50
			if len(lines) < lastN {
				lastN = len(lines)
			}
			tail := strings.Join(lines[len(lines)-lastN:], "\n")
			section := "=== " + name + " (tail) ===\n" + tail
			if totalChars+len(section) > maxChars {
				break
			}
			sections = append(sections, section)
			totalChars += len(section)
		}
	}

	return strings.Join(sections, "\n\n")
}
