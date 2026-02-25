package engine

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type SketchResult struct {
	Explanation string     `json:"explanation"`
	Edits       []FileEdit `json:"edits"`
	NewFiles    []NewFile  `json:"newFiles"`
}

type DelegationBlock struct {
	ToAgentID   string   `json:"toAgentId"`
	Description string   `json:"description"`
	Priority    *int     `json:"priority,omitempty"`
	BlockedBy   []string `json:"blockedBy,omitempty"`
	MaxCostUsd  *float64 `json:"maxCostUsd,omitempty"`
	ContextMode string   `json:"contextMode,omitempty"`
}

var (
	languageExtensions = map[string][]string{
		"typescript": {"ts", "tsx"},
		"javascript": {"js", "jsx", "mjs", "cjs"},
		"python":     {"py", "pyw"},
		"go":         {"go"},
		"rust":       {"rs"},
		"java":       {"java"},
		"kotlin":     {"kt", "kts"},
		"swift":      {"swift"},
		"ruby":       {"rb"},
		"php":        {"php"},
		"csharp":     {"cs"},
		"cpp":        {"cpp", "cc", "cxx", "h", "hpp"},
		"c":          {"c", "h"},
		"html":       {"html", "htm"},
		"css":        {"css", "scss", "sass", "less"},
		"json":       {"json"},
		"yaml":       {"yaml", "yml"},
		"markdown":   {"md", "mdx"},
		"shell":      {"sh", "bash", "zsh"},
		"sql":        {"sql"},
	}

	delegateTagPattern      = regexp.MustCompile(`(?s)<delegate>(.*?)</delegate>`)
	markdownFencePattern    = regexp.MustCompile("^```(?:json)?\\s*\\n?([\\s\\S]*?)\\n?```\\s*$")
	trailingCommaInArray    = regexp.MustCompile(",\\s*]")
	trailingCommaInObject   = regexp.MustCompile(",\\s*}")
)

func stripMarkdownFences(text string) string {
	matched := markdownFencePattern.FindStringSubmatch(text)
	if len(matched) > 1 {
		return strings.TrimSpace(matched[1])
	}
	return text
}

func extractJsonSubstring(text string) (string, error) {
	firstBrace := strings.Index(text, "{")
	lastBrace := strings.LastIndex(text, "}")
	if firstBrace == -1 || lastBrace == -1 || lastBrace < firstBrace {
		return "", fmt.Errorf("no JSON object found in text")
	}
	return text[firstBrace : lastBrace+1], nil
}

func stripTrailingCommas(text string) string {
	text = trailingCommaInArray.ReplaceAllString(text, "]")
	text = trailingCommaInObject.ReplaceAllString(text, "}")
	return text
}

func hasPathTraversal(filePath string) bool {
	decoded, err := url.QueryUnescape(filePath)
	if err != nil {
		decoded = filePath
	}
	normalized := strings.ReplaceAll(decoded, "\\", "/")
	if strings.Contains(normalized, "\x00") {
		return true
	}
	if strings.HasPrefix(normalized, "/") {
		return true
	}
	if strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") {
		return true
	}
	segments := strings.Split(normalized, "/")
	for _, s := range segments {
		if s == ".." {
			return true
		}
	}
	return false
}

func validateFileEdit(value interface{}, index int) (FileEdit, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return FileEdit{}, fmt.Errorf("edits[%d] is not an object", index)
	}
	filePath, ok := m["filePath"].(string)
	if !ok {
		return FileEdit{}, fmt.Errorf("edits[%d].filePath must be a string", index)
	}
	searchText, ok := m["searchText"].(string)
	if !ok {
		return FileEdit{}, fmt.Errorf("edits[%d].searchText must be a string", index)
	}
	replaceText, ok := m["replaceText"].(string)
	if !ok {
		return FileEdit{}, fmt.Errorf("edits[%d].replaceText must be a string", index)
	}
	return FileEdit{
		FilePath:    filePath,
		SearchText:  searchText,
		ReplaceText: replaceText,
	}, nil
}

func validateNewFile(value interface{}, index int) (NewFile, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return NewFile{}, fmt.Errorf("newFiles[%d] is not an object", index)
	}
	filePath, ok := m["filePath"].(string)
	if !ok {
		return NewFile{}, fmt.Errorf("newFiles[%d].filePath must be a string", index)
	}
	content, ok := m["content"].(string)
	if !ok {
		return NewFile{}, fmt.Errorf("newFiles[%d].content must be a string", index)
	}
	return NewFile{
		FilePath: filePath,
		Content:  content,
	}, nil
}

func normalizeEdits(raw interface{}) ([]FileEdit, error) {
	if raw == nil {
		return []FileEdit{}, nil
	}
	if m, ok := raw.(map[string]interface{}); ok {
		edit, err := validateFileEdit(m, 0)
		if err != nil {
			return nil, err
		}
		return []FileEdit{edit}, nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("edits must be an array or object")
	}
	edits := make([]FileEdit, len(arr))
	for i, item := range arr {
		edit, err := validateFileEdit(item, i)
		if err != nil {
			return nil, err
		}
		edits[i] = edit
	}
	return edits, nil
}

func normalizeNewFiles(raw interface{}) ([]NewFile, error) {
	if raw == nil {
		return []NewFile{}, nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("newFiles must be an array")
	}
	newFiles := make([]NewFile, len(arr))
	for i, item := range arr {
		file, err := validateNewFile(item, i)
		if err != nil {
			return nil, err
		}
		newFiles[i] = file
	}
	return newFiles, nil
}

func buildSketchResult(parsed map[string]interface{}) (SketchResult, error) {
	edits, err := normalizeEdits(parsed["edits"])
	if err != nil {
		return SketchResult{}, err
	}
	newFiles, err := normalizeNewFiles(parsed["newFiles"])
	if err != nil {
		return SketchResult{}, err
	}
	explanation := ""
	if exp, ok := parsed["explanation"].(string); ok {
		explanation = exp
	}

	safeEdits := make([]FileEdit, 0, len(edits))
	for _, edit := range edits {
		if !hasPathTraversal(edit.FilePath) {
			safeEdits = append(safeEdits, edit)
		}
	}

	safeNewFiles := make([]NewFile, 0, len(newFiles))
	for _, file := range newFiles {
		if !hasPathTraversal(file.FilePath) {
			safeNewFiles = append(safeNewFiles, file)
		}
	}

	return SketchResult{
		Explanation: explanation,
		Edits:       safeEdits,
		NewFiles:    safeNewFiles,
	}, nil
}

func ParseSketchResponse(raw string) (SketchResult, error) {
	const maxResponseSize = 1024 * 1024
	if len(raw) > maxResponseSize {
		raw = raw[:maxResponseSize]
	}

	if strings.TrimSpace(raw) == "" {
		return SketchResult{}, fmt.Errorf("empty response from LLM")
	}

	stripped := stripMarkdownFences(strings.TrimSpace(raw))
	extracted, err := extractJsonSubstring(stripped)
	if err != nil {
		return SketchResult{}, fmt.Errorf("no JSON object found in LLM response")
	}

	cleaned := stripTrailingCommas(extracted)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return SketchResult{}, fmt.Errorf("failed to parse JSON from LLM response: %w", err)
	}

	result, err := buildSketchResult(parsed)
	if err != nil {
		return SketchResult{}, fmt.Errorf("schema validation failed: %w", err)
	}

	return result, nil
}

func fileMatchesLanguages(filePath string, languages []string) bool {
	if len(languages) == 0 {
		return true
	}
	parts := strings.Split(filePath, ".")
	if len(parts) < 2 {
		return false
	}
	extension := strings.ToLower(parts[len(parts)-1])

	for _, language := range languages {
		extensions, exists := languageExtensions[strings.ToLower(language)]
		if exists {
			for _, ext := range extensions {
				if ext == extension {
					return true
				}
			}
		}
	}
	return false
}

func fileMatchesDirectories(filePath string, directories []string) bool {
	if len(directories) == 0 {
		return true
	}
	for _, directory := range directories {
		normalizedDirectory := directory
		if !strings.HasSuffix(normalizedDirectory, "/") {
			normalizedDirectory += "/"
		}
		if strings.HasPrefix(filePath, normalizedDirectory) || filePath == directory {
			return true
		}
	}
	return false
}

func fileMatchesScope(filePath string, directories []string, languages []string) bool {
	return fileMatchesDirectories(filePath, directories) && fileMatchesLanguages(filePath, languages)
}

func ExtractDelegationBlocks(raw string) []DelegationBlock {
	if len(raw) > 100000 {
		return []DelegationBlock{}
	}

	blocks := make([]DelegationBlock, 0, 10)
	matches := delegateTagPattern.FindAllStringSubmatch(raw, -1)

	const maxDelegations = 10
	count := 0

	for _, match := range matches {
		if count >= maxDelegations {
			break
		}
		if len(match) < 2 {
			continue
		}
		content := strings.TrimSpace(match[1])
		if content == "" || len(content) > 5000 {
			continue
		}

		stripped := stripMarkdownFences(content)
		jsonStr, err := extractJsonSubstring(stripped)
		if err != nil {
			continue
		}
		cleaned := stripTrailingCommas(jsonStr)

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
			continue
		}

		toAgentID, ok := parsed["toAgentId"].(string)
		if !ok || len(toAgentID) >= 100 {
			continue
		}
		description, ok := parsed["description"].(string)
		if !ok || len(description) == 0 || len(description) >= 10000 {
			continue
		}

		block := DelegationBlock{
			ToAgentID:   toAgentID,
			Description: description,
		}

		if priority, ok := parsed["priority"].(float64); ok && priority >= 0 && priority <= 100 {
			p := int(priority)
			block.Priority = &p
		}

		if blockedByRaw, ok := parsed["blockedBy"].([]interface{}); ok && len(blockedByRaw) <= 50 {
			blockedBy := make([]string, 0, len(blockedByRaw))
			valid := true
			for _, id := range blockedByRaw {
				if str, ok := id.(string); ok && len(str) < 100 {
					blockedBy = append(blockedBy, str)
				} else {
					valid = false
					break
				}
			}
			if valid {
				block.BlockedBy = blockedBy
			}
		}

		if maxCostUsd, ok := parsed["maxCostUsd"].(float64); ok && maxCostUsd > 0 {
			block.MaxCostUsd = &maxCostUsd
		}

		if contextMode, ok := parsed["contextMode"].(string); ok {
			if contextMode == "summary" || contextMode == "diff" || contextMode == "full" {
				block.ContextMode = contextMode
			}
		}

		blocks = append(blocks, block)
		count++
	}

	return blocks
}

func StripDelegationTags(raw string) string {
	return delegateTagPattern.ReplaceAllString(raw, "")
}

func ParseSketchWithDelegations(raw string) (SketchResult, []DelegationBlock, error) {
	delegations := ExtractDelegationBlocks(raw)
	cleanedRaw := StripDelegationTags(raw)

	var sketch SketchResult
	var err error
	if len(cleanedRaw) > 0 && strings.Contains(cleanedRaw, "{") {
		sketch, err = ParseSketchResponse(cleanedRaw)
		if err != nil {
			return SketchResult{}, delegations, err
		}
	} else {
		sketch = SketchResult{Explanation: "", Edits: []FileEdit{}, NewFiles: []NewFile{}}
	}

	return sketch, delegations, nil
}

func ValidateAgentScope(result SketchResult, scope *AgentScope) SketchResult {
	if scope == nil {
		return result
	}

	directories := scope.Directories
	languages := scope.Languages

	if len(directories) == 0 && len(languages) == 0 {
		return result
	}

	filteredEdits := make([]FileEdit, 0, len(result.Edits))
	for _, edit := range result.Edits {
		if fileMatchesScope(edit.FilePath, directories, languages) {
			filteredEdits = append(filteredEdits, edit)
		}
	}

	filteredNewFiles := make([]NewFile, 0, len(result.NewFiles))
	for _, file := range result.NewFiles {
		if fileMatchesScope(file.FilePath, directories, languages) {
			filteredNewFiles = append(filteredNewFiles, file)
		}
	}

	return SketchResult{
		Explanation: result.Explanation,
		Edits:       filteredEdits,
		NewFiles:    filteredNewFiles,
	}
}
