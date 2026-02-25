package orchestrator

import (
	"regexp"
	"strings"
)

var (
	questionKeywords = regexp.MustCompile(`\b(which|should i|do you want|could you|please clarify|choose|select|prefer|would you like)\b`)
	codeLinePattern  = regexp.MustCompile(`^[\s]*[{(;}\])#\/\*]|^\s*(import|export|const|let|var|function|class|if|else|for|while|return|switch|case)\b`)
	listItemPattern  = regexp.MustCompile(`^\s*(\d+[.)]\s|[-*]\s)`)
	boundaryPattern  = regexp.MustCompile("^(```|---|\\*{3,}|={3,})$")
)

type RollingBuffer struct {
	buffer []string
	head   int
	count  int
	size   int
}

func NewRollingBuffer(size int) *RollingBuffer {
	return &RollingBuffer{
		buffer: make([]string, size),
		size:   size,
	}
}

func (rb *RollingBuffer) Push(item string) {
	rb.buffer[rb.head] = item
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

func (rb *RollingBuffer) ToArray() []string {
	if rb.count < rb.size {
		return rb.buffer[:rb.count]
	}
	result := make([]string, rb.size)
	copy(result, rb.buffer[rb.head:])
	copy(result[rb.size-rb.head:], rb.buffer[:rb.head])
	return result
}

func (rb *RollingBuffer) Clear() {
	rb.head = 0
	rb.count = 0
}

func LooksLikeQuestion(lines []string) bool {
	nonEmpty := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) == 0 {
		return false
	}

	lastLine := strings.TrimSpace(nonEmpty[len(nonEmpty)-1])
	if codeLinePattern.MatchString(lastLine) {
		return false
	}

	hasQuestionMark := strings.HasSuffix(lastLine, "?")
	hasKeyword := questionKeywords.MatchString(strings.ToLower(lastLine))
	startsWithUppercase := len(lastLine) > 0 && lastLine[0] >= 'A' && lastLine[0] <= 'Z'

	if hasQuestionMark && (hasKeyword || startsWithUppercase) {
		return true
	}
	if hasQuestionMark && len(lastLine) > 10 {
		return true
	}
	if hasKeyword && startsWithUppercase {
		return true
	}

	return false
}

func ExtractQuestion(lines []string) string {
	nonEmpty := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}

	collected := make([]string, 0, 15)
	maxExtractLines := 15

	for i := len(nonEmpty) - 1; i >= 0 && len(collected) < maxExtractLines; i-- {
		trimmed := strings.TrimSpace(nonEmpty[i])
		if boundaryPattern.MatchString(trimmed) {
			break
		}
		if codeLinePattern.MatchString(trimmed) && !listItemPattern.MatchString(trimmed) {
			break
		}
		collected = append([]string{trimmed}, collected...)
		if !listItemPattern.MatchString(trimmed) && len(collected) >= 3 && i < len(nonEmpty)-3 {
			break
		}
	}

	return strings.Join(collected, "\n")
}
