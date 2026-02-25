# shipmates

talk to ai coding agents through discord and slack voice/text. give them work, hang up, they push code.

## quick start

```bash
git clone https://github.com/KranzL/shipmates-oss.git
cd shipmates-oss
./setup.sh
```

the setup script walks you through everything -- discord bot, llm provider, agents, then starts docker compose.

## what you need

- docker + docker compose
- an llm api key (anthropic, openai, google, or any openai-compatible endpoint)

## docker services

everything runs in docker compose. here's what each service does:

| service | port | what it does |
|---------|------|-------------|
| `db` | 5432 | postgres 16 -- stores agents, sessions, config, everything |
| `bot` | -- | the main brain. discord/slack bot, voice pipeline, orchestrator, task delegation |
| `stream-relay` | 8080 | websocket relay for streaming live terminal output from agents |
| `docker-proxy` | 2375 (internal) | proxies the docker socket so the bot can spin up coding containers safely |
| `webhook-router` | 8094 | receives github webhooks, routes to pr-review or ci-diagnosis |
| `pr-review` | 8092 (internal) | reviews pull requests using your llm |
| `ci-diagnosis` | 8093 (internal) | diagnoses ci failures, suggests fixes |
| `test-gen` | 8095 (internal) | coverage-guided test generation |

internal means the port isn't exposed to the host, only to other services.

the bot talks to docker-proxy to spin up isolated containers per task. each container clones your repo, runs a coding cli (claude code, codex, gemini cli), and pushes a branch.

## supported providers

| provider | type | what agents do |
|----------|------|----------------|
| anthropic | container | write code via claude code, push branches |
| openai | container | write code via codex cli, push branches |
| google | container | write code via gemini cli, push branches |
| venice ai | chat-only | review code, answer questions |
| groq | chat-only | review code, answer questions |
| together ai | chat-only | review code, answer questions |
| fireworks ai | chat-only | review code, answer questions |
| custom | chat-only | any openai-compatible endpoint |

container providers spin up docker containers that clone, code, and push. chat-only providers hit the llm api directly.

## usage

**discord** -- join a voice channel and talk, or @mention the bot in text.

**slack** -- @mention the bot in any channel, or use slash commands:
- `/shipmates ask <agent> <question>`
- `/shipmates review <owner/repo>`

route to an agent by name: "alex, review the auth module"

## config

everything lives in `shipmates.config.yml`. the setup script generates it, or copy from `shipmates.config.example.yml`.

## manual setup

```bash
cp .env.example .env
cp shipmates.config.example.yml shipmates.config.yml
# fill in your values
docker compose up --build -d
```

## env vars

check `.env.example` for the full list. the important ones:

- `DISCORD_TOKEN` / `SLACK_BOT_TOKEN` -- at least one platform
- `LLM_API_KEY` -- your llm provider key
- `GITHUB_TOKEN` -- for repo access
- `ENCRYPTION_KEY` -- encrypts stored api keys (32 bytes)
- `RELAY_SECRET` -- authenticates stream relay connections

## dev

each service is a standalone go module:

```bash
cd bot && go build ./...
cd stream-relay && go build ./...
cd webhook-router && go build ./...
cd pr-review && go build ./...
cd ci-diagnosis && go build ./...
cd test-gen && go build ./...
```

## license

apache 2.0
