# Shipmates

Talk to your AI dev team through Discord and Slack. Give an agent work via voice or text, and walk away. They clone your repo, write code, push branches, and report back.

## Quick Start

```bash
git clone https://github.com/KranzL/shipmates-oss.git
cd shipmates-oss
./setup.sh
```

The setup script walks you through creating your own Discord bot and/or Slack app, picking an LLM provider, configuring your agents, and starting everything with Docker Compose.

## What You Need

- Docker and Docker Compose
- An LLM API key (or a self-hosted endpoint)

## Supported Providers

| Provider | Type | What agents do |
|----------|------|----------------|
| Anthropic | Container | Write code via Claude Code, push branches |
| OpenAI | Container | Write code via Codex CLI, push branches |
| Google | Container | Write code via Gemini CLI, push branches |
| Venice AI | Chat-only | Review code, answer questions |
| Groq | Chat-only | Review code, answer questions |
| Together AI | Chat-only | Review code, answer questions |
| Fireworks AI | Chat-only | Review code, answer questions |
| Custom | Chat-only | Any OpenAI-compatible endpoint |

Container providers spin up isolated Docker containers that clone your repo, run a coding CLI, and push a branch. Chat-only providers use the LLM API directly for code review and conversation.

## How It Works

**Discord** -- Join a voice channel and talk, or @mention the bot in text.

**Slack** -- @mention the bot in any channel, or use slash commands:
- `/shipmates ask <agent> <question>`
- `/shipmates review <owner/repo>`

The bot routes your message to the right agent by name ("Alex, review the auth module"). Agents with container providers clone your repo, make changes, and push a branch. Chat-only providers do code review using the repo context.

## Configuration

Everything is configured in `shipmates.config.yml`. The setup script generates this for you, or you can write it by hand. See `shipmates.config.example.yml` for the full format.

## Architecture

```
bot/              Go Discord/Slack bot, voice pipeline, orchestrator, delegation
go-shared/        Shared Go package (database, crypto, types)
go-llm/           Multi-provider LLM client (Anthropic, OpenAI, Google, etc.)
go-github/        GitHub API client
stream-relay/     Go WebSocket pub/sub relay for live output
webhook-router/   GitHub webhook routing
pr-review/        PR code review agent
ci-diagnosis/     CI failure diagnosis agent
test-gen/         Coverage-guided test generation
containers/       Per-provider Dockerfiles (Anthropic, OpenAI, Google)
database/         PostgreSQL schema
```

## Manual Setup

```bash
cp .env.example .env
cp shipmates.config.example.yml shipmates.config.yml
# Fill in your values, then:
docker compose up --build -d
```

## Development

Each service is a standalone Go module. To build:

```bash
cd bot && go build ./...
cd stream-relay && go build ./...
cd webhook-router && go build ./...
cd pr-review && go build ./...
cd ci-diagnosis && go build ./...
cd test-gen && go build ./...
```

## License

Apache 2.0
