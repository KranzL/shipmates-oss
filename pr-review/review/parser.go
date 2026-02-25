package review

import (
	"strings"
)

type DiffFile struct {
	Path    string
	Hunks   []DiffHunk
	NewFile bool
	Deleted bool
}

type DiffHunk struct {
	StartLine int
	LineCount int
	Lines     []DiffLine
}

type DiffLine struct {
	Type     string
	Content  string
	OldLine  int
	NewLine  int
	Position int
}

func ParseDiff(diff string) []DiffFile {
	var files []DiffFile
	fileSections := splitDiffByFile(diff)

	for _, sectionLines := range fileSections {
		file := parseDiffFile(sectionLines)
		if file.Path != "" {
			files = append(files, file)
		}
	}

	return files
}

func splitDiffByFile(diff string) [][][]byte {
	lines := strings.Split(diff, "\n")
	var sections [][][]byte
	var current [][]byte

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			if len(current) > 0 {
				sections = append(sections, current)
			}
			current = [][]byte{[]byte(line)}
		} else {
			current = append(current, []byte(line))
		}
	}

	if len(current) > 0 {
		sections = append(sections, current)
	}

	return sections
}

func parseDiffFile(lines [][]byte) DiffFile {
	var file DiffFile
	position := 0

	for i, lineBytes := range lines {
		line := string(lineBytes)
		if strings.HasPrefix(line, "+++ b/") {
			file.Path = strings.TrimPrefix(line, "+++ b/")
		} else if strings.HasPrefix(line, "+++ /dev/null") {
			file.Deleted = true
		} else if strings.HasPrefix(line, "new file mode") {
			file.NewFile = true
		} else if strings.HasPrefix(line, "--- /dev/null") && !file.Deleted {
			file.NewFile = true
		}

		if strings.HasPrefix(line, "@@") {
			hunk := parseHunkHeader(line)
			hunkLines := collectHunkLines(lines[i+1:], &position, hunk.StartLine)
			hunk.Lines = hunkLines
			file.Hunks = append(file.Hunks, hunk)
		}
	}

	return file
}

func parseHunkHeader(line string) DiffHunk {
	hunk := DiffHunk{}

	atAt := strings.Index(line, "@@")
	if atAt < 0 {
		return hunk
	}
	rest := line[atAt+2:]
	nextAtAt := strings.Index(rest, "@@")
	if nextAtAt < 0 {
		return hunk
	}
	header := strings.TrimSpace(rest[:nextAtAt])

	parts := strings.Split(header, " ")
	for _, part := range parts {
		if strings.HasPrefix(part, "+") {
			nums := strings.TrimPrefix(part, "+")
			if commaIdx := strings.Index(nums, ","); commaIdx >= 0 {
				startLine := parseLineNum(nums[:commaIdx])
				lineCount := parseLineNum(nums[commaIdx+1:])
				hunk.StartLine = startLine
				hunk.LineCount = lineCount
			} else {
				hunk.StartLine = parseLineNum(nums)
				hunk.LineCount = 1
			}
		}
	}

	return hunk
}

func parseLineNum(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func collectHunkLines(lines [][]byte, position *int, startLine int) []DiffLine {
	var result []DiffLine
	newLine := startLine - 1

	for _, lineBytes := range lines {
		line := string(lineBytes)
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "@@") {
			break
		}

		*position++
		dl := DiffLine{Position: *position}

		if strings.HasPrefix(line, "+") {
			newLine++
			dl.Type = "add"
			dl.Content = line[1:]
			dl.NewLine = newLine
		} else if strings.HasPrefix(line, "-") {
			dl.Type = "remove"
			dl.Content = line[1:]
		} else {
			newLine++
			dl.Type = "context"
			if len(line) > 0 {
				dl.Content = line[1:]
			}
			dl.NewLine = newLine
		}

		result = append(result, dl)
	}

	return result
}

func LineToPosition(files []DiffFile, filePath string, lineNumber int) int {
	for _, file := range files {
		if file.Path != filePath {
			continue
		}

		for _, hunk := range file.Hunks {
			for _, line := range hunk.Lines {
				if line.NewLine == lineNumber && line.Type != "remove" {
					return line.Position
				}
			}
		}
	}
	return 0
}

func BuildDiffContext(files []DiffFile, maxTokensPerFile int) string {
	var builder strings.Builder
	maxCharsPerFile := maxTokensPerFile * 4

	for i, file := range files {
		if i >= 100 {
			break
		}

		builder.WriteString("\n--- ")
		builder.WriteString(file.Path)
		builder.WriteString(" ---\n")

		fileContent := buildFileContent(file)
		if len(fileContent) > maxCharsPerFile {
			fileContent = fileContent[:maxCharsPerFile] + "\n... (truncated)"
		}

		builder.WriteString(fileContent)
	}

	return builder.String()
}

func buildFileContent(file DiffFile) string {
	var builder strings.Builder

	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			switch line.Type {
			case "add":
				builder.WriteString("+")
			case "remove":
				builder.WriteString("-")
			default:
				builder.WriteString(" ")
			}
			builder.WriteString(line.Content)
			builder.WriteString("\n")
		}
	}

	return builder.String()
}
