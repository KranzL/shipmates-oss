package diagnosis

import (
	"context"
	"fmt"
	"log"
	"strings"

	gh "github.com/KranzL/shipmates-oss/go-github"
)

type FixFile struct {
	Path    string `json:"path"`
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

func IsAutoFixSafe(category ErrorCategory, fixFiles []FixFile) bool {
	if !AutoFixSafeCategories[category] {
		return false
	}

	for _, f := range fixFiles {
		for blocked := range AutoFixBlockedFiles {
			if strings.HasPrefix(f.Path, blocked) || f.Path == blocked {
				return false
			}
		}
	}

	return true
}

func BuildFixBranchName(workflowName string, commitSHA string) string {
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, workflowName)

	if len(sanitized) > 30 {
		sanitized = sanitized[:30]
	}

	shortSHA := commitSHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	return fmt.Sprintf("shipmates/fix/%s-%s", sanitized, shortSHA)
}

func PostDiagnosisComment(ctx context.Context, ghClient *gh.Client, repo string, prNumber int, agentName string, agentRole string, result *DiagnosisResult) error {
	if prNumber == 0 {
		return nil
	}

	var builder strings.Builder

	fmt.Fprintf(&builder, "**%s** (%s) diagnosed a CI failure\n\n", agentName, agentRole)

	fmt.Fprintf(&builder, "**Category:** %s\n", result.ErrorCategory)
	fmt.Fprintf(&builder, "**Confidence:** %.0f%%\n\n", result.Confidence*100)

	builder.WriteString("### Diagnosis\n")
	builder.WriteString(result.Diagnosis)
	builder.WriteString("\n\n")

	builder.WriteString("### Root Cause\n")
	builder.WriteString(result.RootCause)
	builder.WriteString("\n\n")

	builder.WriteString("### Suggested Fix\n")
	builder.WriteString(result.SuggestedFix)
	builder.WriteString("\n")

	if result.AutoFixable {
		builder.WriteString("\nThis looks auto-fixable. Reply with `/shipmates fix` to apply automatically.\n")
	}

	if err := ghClient.PostComment(ctx, repo, prNumber, builder.String()); err != nil {
		log.Printf("post diagnosis comment: %v", err)
		return err
	}

	return nil
}
