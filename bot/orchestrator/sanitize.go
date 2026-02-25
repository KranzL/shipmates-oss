package orchestrator

import "regexp"

var (
	sensitivePattern = regexp.MustCompile(
		`sk-[a-zA-Z0-9]{20,}|` +
			`sk-ant-[a-zA-Z0-9\-]{20,}|` +
			`ghp_[a-zA-Z0-9]{36}|` +
			`ghu_[a-zA-Z0-9]{36}|` +
			`xoxb-[a-zA-Z0-9-]+|` +
			`AKIA[0-9A-Z]{16}|` +
			`npm_[a-zA-Z0-9]{36}|` +
			`-----BEGIN.*PRIVATE KEY-----|` +
			`ya29\.[a-zA-Z0-9\-_]+|` +
			`AIza[a-zA-Z0-9\-_]{35}|` +
			`sk_[a-zA-Z0-9]{40}|` +
			`[MN][A-Za-z\d]{23,}\.[A-Za-z\d-_]{6}\.[A-Za-z\d-_]{27}|` +
			`\d+-[a-z0-9]+\.apps\.googleusercontent\.com|` +
			`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}|` +
			`ANTHROPIC_API_KEY=\S+|` +
			`OPENAI_API_KEY=\S+|` +
			`GEMINI_API_KEY=\S+|` +
			`GITHUB_TOKEN=\S+|` +
			`RELAY_SECRET=\S+|` +
			`DEEPGRAM_API_KEY=\S+|` +
			`ELEVENLABS_API_KEY=\S+|` +
			`ENCRYPTION_KEY=\S+|` +
			`GROQ_API_KEY=\S+|` +
			`TOGETHER_API_KEY=\S+|` +
			`FIREWORKS_API_KEY=\S+|` +
			`DISCORD_TOKEN=\S+|` +
			`NEXTAUTH_SECRET=\S+|` +
			`x-access-token:[^@]+@|` +
			`postgresql://[^:]+:[^@]+@|` +
			`[A-Z_]{3,}(?:KEY|SECRET|TOKEN|PASSWORD)=[^\s]{8,}`,
	)

	ansiPattern    = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]|\x1b\].*?(?:\x07|\x1b\\)|\x1b[()#][A-Za-z0-9]|\x1b[\x20-\x2f][\x30-\x7e]|\x1b[=>NOMDEHFGJKSTclmnopq78]|\x9b[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]`)
	controlPattern = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
)

func SanitizeOutput(text string) string {
	text = sensitivePattern.ReplaceAllString(text, "[REDACTED]")
	text = ansiPattern.ReplaceAllString(text, "")
	text = controlPattern.ReplaceAllString(text, "")
	return text
}

func SanitizeTask(task string) string {
	result := make([]rune, 0, len(task))
	for _, r := range task {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == ' ' || r == ',' || r == '.' || r == '!' || r == '?' || r == '-' {
			result = append(result, r)
		}
	}
	return string(result)
}

func StripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}
