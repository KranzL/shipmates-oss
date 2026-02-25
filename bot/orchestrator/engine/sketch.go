package engine

import (
	"fmt"
	"strings"
)

type SketchPromptOptions struct {
	Task             string
	Repo             string
	FileTree         string
	FullFiles        []FileWithContent
	AgentPersonality string
	AgentScope       *AgentScope
	PreviousErrors   string
	FailedEdits      []FailedEditContext
	Iteration        int
}

type FileWithContent struct {
	Path    string
	Content string
}

type AgentScope struct {
	Directories []string
	Languages   []string
}

type FailedEditContext struct {
	FilePath      string
	SearchText    string
	ActualContent string
}

func BuildSketchSystemPrompt() string {
	return `You are a senior software engineer making targeted changes to a codebase.

You respond ONLY with a JSON object containing:
- "explanation": A 1-2 sentence summary of what you changed and why
- "edits": An array of edit blocks, each with:
  - "filePath": Relative path from repo root
  - "searchText": The exact text to find in the file (minimum 3 lines context)
  - "replaceText": The replacement text
- "newFiles": An array of new files to create, each with:
  - "filePath": Relative path from repo root
  - "content": Full file content

Rules:
- searchText must be an exact substring of the current file content
- Include 2-3 lines of unchanged context above and below
- For new files, use newFiles array
- If a file needs multiple edits, include multiple edit blocks in order
- Never include comments in the code
- Output valid JSON only, no markdown fencing

If a subtask should be handled by another agent, wrap a delegation request in <delegate> tags:
<delegate>
{
  "toAgentId": "agent-id",
  "description": "what to do",
  "priority": 50,
  "blockedBy": ["task-id-1"],
  "maxCostUsd": 10.0,
  "contextMode": "summary"
}
</delegate>
Delegation blocks are separate from the JSON response. Only delegate when the task explicitly involves another agent. All fields except toAgentId and description are optional.`
}

func formatFullFiles(files []FileWithContent) string {
	totalSize := 0
	for _, f := range files {
		totalSize += len(f.Path) + len(f.Content) + 20
	}
	var b strings.Builder
	b.Grow(totalSize)
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("### ")
		b.WriteString(f.Path)
		b.WriteString("\n```\n")
		b.WriteString(f.Content)
		b.WriteString("\n```")
	}
	return b.String()
}

func buildScopeContext(agentPersonality string, agentScope *AgentScope) string {
	var parts []string

	if agentPersonality != "" {
		parts = append(parts, fmt.Sprintf("Agent context: %s", agentPersonality))
	}

	if agentScope != nil {
		if len(agentScope.Directories) > 0 {
			parts = append(parts, fmt.Sprintf("Scoped directories: %s", strings.Join(agentScope.Directories, ", ")))
		}
		if len(agentScope.Languages) > 0 {
			parts = append(parts, fmt.Sprintf("Scoped languages: %s", strings.Join(agentScope.Languages, ", ")))
		}
	}

	return strings.Join(parts, "\n")
}

func BuildRetryContext(errors string, failedEdits []FailedEditContext) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("## Previous Errors\n%s", errors))

	if len(failedEdits) > 0 {
		failedSections := make([]string, 0, len(failedEdits))
		for _, edit := range failedEdits {
			failedSections = append(failedSections,
				fmt.Sprintf("### %s\nFailed searchText:\n```\n%s\n```\n\nActual file content:\n```\n%s\n```",
					edit.FilePath, edit.SearchText, edit.ActualContent))
		}
		failedSection := strings.Join(failedSections, "\n\n")
		parts = append(parts, fmt.Sprintf("## Failed Edits\nThe following edits failed because searchText did not match. Use the actual file content below to construct correct edits.\n\n%s", failedSection))
	}

	return strings.Join(parts, "\n\n")
}

func BuildSketchUserPrompt(opts SketchPromptOptions) string {
	var sections []string

	sections = append(sections, fmt.Sprintf("## Repository: %s", opts.Repo))
	sections = append(sections, fmt.Sprintf("## File Tree\n%s", opts.FileTree))

	if len(opts.FullFiles) > 0 {
		sections = append(sections, fmt.Sprintf("## File Contents\n%s", formatFullFiles(opts.FullFiles)))
	}

	sections = append(sections, fmt.Sprintf("## Task\n%s", opts.Task))

	scopeContext := buildScopeContext(opts.AgentPersonality, opts.AgentScope)
	if scopeContext != "" {
		sections = append(sections, fmt.Sprintf("## Agent Context\n%s", scopeContext))
	}

	if opts.PreviousErrors != "" || len(opts.FailedEdits) > 0 {
		retryIteration := ""
		if opts.Iteration > 0 {
			retryIteration = fmt.Sprintf(" (attempt %d)", opts.Iteration)
		}
		sections = append(sections, fmt.Sprintf("## Retry%s\n%s", retryIteration, BuildRetryContext(opts.PreviousErrors, opts.FailedEdits)))
	}

	return strings.Join(sections, "\n\n")
}
