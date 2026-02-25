package goshared

import "regexp"

var LanguageExtensions = map[string][]string{
	"typescript": {".ts", ".tsx"},
	"javascript": {".js", ".jsx"},
	"python":     {".py"},
	"go":         {".go"},
	"rust":       {".rs"},
	"java":       {".java"},
	"ruby":       {".rb"},
	"php":        {".php"},
	"swift":      {".swift"},
	"kotlin":     {".kt"},
	"scala":      {".scala"},
	"c":          {".c", ".h"},
	"cpp":        {".cpp", ".hpp", ".cc", ".cxx"},
	"css":        {".css", ".scss"},
	"html":       {".html"},
	"vue":        {".vue"},
	"svelte":     {".svelte"},
	"sql":        {".sql"},
	"shell":      {".sh"},
}

var SkipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".next":        true,
	".venv":        true,
	"vendor":       true,
	".cache":       true,
	"coverage":     true,
	".turbo":       true,
}

var SkipFiles = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"bun.lockb":         true,
}

var SourceExtensions = map[string]bool{
	".ts":      true,
	".tsx":     true,
	".js":      true,
	".jsx":     true,
	".py":      true,
	".go":      true,
	".rs":      true,
	".java":    true,
	".rb":      true,
	".php":     true,
	".swift":   true,
	".kt":      true,
	".scala":   true,
	".c":       true,
	".cpp":     true,
	".h":       true,
	".css":     true,
	".scss":    true,
	".html":    true,
	".vue":     true,
	".svelte":  true,
	".json":    true,
	".yaml":    true,
	".yml":     true,
	".toml":    true,
	".prisma":  true,
	".graphql": true,
	".sql":     true,
	".sh":      true,
	".md":      true,
}

var TestFilePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\.test\.[jt]sx?$`),
	regexp.MustCompile(`\.spec\.[jt]sx?$`),
	regexp.MustCompile(`_test\.go$`),
	regexp.MustCompile(`/__tests__/`),
}
