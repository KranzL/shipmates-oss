package diagnosis

import "strings"

var categoryKeywords = map[ErrorCategory][]string{
	CategoryBuild: {
		"compile", "compilation", "build failed", "build error",
		"cannot find module", "module not found", "undefined reference",
		"linker error", "go build", "cargo build", "make:",
	},
	CategoryTest: {
		"test failed", "FAIL", "assertion", "expect(",
		"AssertionError", "test suite", "jest", "vitest",
		"go test", "pytest", "spec failed",
	},
	CategoryLint: {
		"eslint", "golint", "pylint", "clippy",
		"lint error", "linting", "golangci-lint",
		"stylelint", "prettier",
	},
	CategoryTypeCheck: {
		"type error", "TypeError", "TS2", "TS7",
		"type mismatch", "incompatible types",
		"cannot assign", "tsc", "mypy",
	},
	CategoryDeploy: {
		"deploy", "deployment", "rollout",
		"terraform", "cloudformation", "helm",
		"kubectl", "docker push",
	},
	CategoryFormat: {
		"format", "formatting", "gofmt",
		"prettier --check", "black --check",
		"rustfmt",
	},
	CategoryImport: {
		"import", "require", "module not found",
		"cannot resolve", "no such file",
		"ModuleNotFoundError", "unresolved import",
	},
	CategoryConfig: {
		"config", "configuration", "yaml",
		"invalid option", "unknown flag",
		"missing env", "environment variable",
	},
}

func ClassifyError(errorText string) ErrorCategory {
	lower := strings.ToLower(errorText)

	scores := make(map[ErrorCategory]int)
	for category, keywords := range categoryKeywords {
		for _, kw := range keywords {
			count := strings.Count(lower, strings.ToLower(kw))
			scores[category] += count
		}
	}

	bestCategory := CategoryUnknown
	bestScore := 0
	for category, score := range scores {
		if score > bestScore {
			bestScore = score
			bestCategory = category
		}
	}

	return bestCategory
}
