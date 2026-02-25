package review

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/KranzL/shipmates-oss/go-github"
)

func PostReview(ctx context.Context, client *gh.Client, repo string, prNumber int, agentName string, agentRole string, result *ReviewResult, files []DiffFile) error {
	var ghComments []gh.ReviewComment

	for _, comment := range result.Comments {
		position := LineToPosition(files, comment.FilePath, comment.Line)
		if position == 0 {
			continue
		}

		severityPrefix := formatSeverityPrefix(comment.Severity)
		body := fmt.Sprintf("%s **[%s]** %s", severityPrefix, comment.Category, comment.Body)

		ghComments = append(ghComments, gh.ReviewComment{
			Path:     comment.FilePath,
			Position: position,
			Body:     body,
		})
	}

	summaryBody := formatReviewSummary(agentName, agentRole, result)

	event := "COMMENT"
	hasCritical := result.SeverityCounts["critical"] > 0
	if hasCritical {
		event = "REQUEST_CHANGES"
	}

	review := gh.Review{
		Body:     summaryBody,
		Event:    event,
		Comments: ghComments,
	}

	return client.CreateReview(ctx, repo, prNumber, review)
}

func formatSeverityPrefix(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "[CRITICAL]"
	case SeverityWarning:
		return "[WARNING]"
	case SeverityInfo:
		return "[INFO]"
	case SeverityNit:
		return "[NIT]"
	default:
		return ""
	}
}

func formatReviewSummary(agentName string, agentRole string, result *ReviewResult) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "**%s** (%s) reviewed this PR\n\n", agentName, agentRole)

	builder.WriteString(result.Summary)
	builder.WriteString("\n\n")

	if len(result.SeverityCounts) > 0 {
		builder.WriteString("| Severity | Count |\n|----------|-------|\n")
		for _, sev := range []string{"critical", "warning", "info", "nit"} {
			if count, ok := result.SeverityCounts[sev]; ok && count > 0 {
				fmt.Fprintf(&builder, "| %s | %d |\n", sev, count)
			}
		}
	}

	return builder.String()
}
