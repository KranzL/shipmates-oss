#!/usr/bin/env bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

print_header() {
  echo ""
  echo -e "${CYAN}=== $1 ===${NC}"
  echo ""
}

print_success() {
  echo -e "${GREEN}$1${NC}"
}

print_warning() {
  echo -e "${YELLOW}$1${NC}"
}

print_error() {
  echo -e "${RED}$1${NC}"
}

check_command() {
  if ! command -v "$1" &> /dev/null; then
    print_error "$1 is not installed."
    return 1
  fi
  return 0
}

prompt_required() {
  local var_name="$1"
  local prompt_text="$2"
  local value=""

  while [ -z "$value" ]; do
    read -rp "$prompt_text: " value
    if [ -z "$value" ]; then
      print_error "This field is required."
    fi
  done

  eval "$var_name=\"$value\""
}

prompt_optional() {
  local var_name="$1"
  local prompt_text="$2"
  local default_value="${3:-}"
  local value=""

  if [ -n "$default_value" ]; then
    read -rp "$prompt_text [$default_value]: " value
    value="${value:-$default_value}"
  else
    read -rp "$prompt_text (press Enter to skip): " value
  fi

  eval "$var_name=\"$value\""
}

echo ""
echo -e "${CYAN}Shipmates Setup${NC}"
echo "This script will walk you through configuring Shipmates."
echo ""

print_header "Checking Prerequisites"

missing=0
for cmd in docker git; do
  if check_command "$cmd"; then
    print_success "  $cmd found"
  else
    missing=1
  fi
done

if ! docker compose version &> /dev/null; then
  print_error "  docker compose is not available. Install Docker Desktop or the compose plugin."
  missing=1
else
  print_success "  docker compose found"
fi

if [ "$missing" -eq 1 ]; then
  print_error ""
  print_error "Install the missing prerequisites and re-run this script."
  exit 1
fi

print_header "Platform Selection"
echo "Shipmates supports Discord (voice + text) and Slack (text only)."
echo ""
echo "  1) Discord only"
echo "  2) Slack only"
echo "  3) Both Discord and Slack"
echo ""

PLATFORM_CHOICE=""
while [ -z "$PLATFORM_CHOICE" ]; do
  read -rp "Select platform (1/2/3): " platform_input
  case "$platform_input" in
    1) PLATFORM_CHOICE="discord" ;;
    2) PLATFORM_CHOICE="slack" ;;
    3) PLATFORM_CHOICE="both" ;;
    *) print_error "Enter 1, 2, or 3." ;;
  esac
done

USE_DISCORD=false
USE_SLACK=false
DISCORD_TOKEN=""
DISCORD_CLIENT_ID=""
DISCORD_GUILD_ID=""
SLACK_BOT_TOKEN=""
SLACK_APP_TOKEN=""
SLACK_SIGNING_SECRET=""
SLACK_WORKSPACE_ID=""

if [ "$PLATFORM_CHOICE" = "discord" ] || [ "$PLATFORM_CHOICE" = "both" ]; then
  USE_DISCORD=true
fi
if [ "$PLATFORM_CHOICE" = "slack" ] || [ "$PLATFORM_CHOICE" = "both" ]; then
  USE_SLACK=true
fi

print_header "Accounts Check"
echo "Shipmates needs accounts with a few services. Let's make sure you're set up."
echo ""

if $USE_DISCORD; then
  echo -e "1. ${CYAN}Discord${NC} -- you need a bot application"
  echo "   https://discord.com/developers/applications"
  echo ""
  read -rp "   Do you have a Discord bot created? (Y/n): " has_discord
  if [[ "$has_discord" =~ ^[Nn]$ ]]; then
    echo ""
    echo "   Create one here: https://discord.com/developers/applications"
    echo "   1. Click 'New Application' and give it a name"
    echo "   2. Go to the Bot tab and click 'Add Bot'"
    echo "   3. Copy the bot token from the Bot tab"
    echo "   4. Copy the client ID from OAuth2 > General"
    echo "   5. You'll invite the bot to your server after setup completes"
    echo ""
    read -rp "   Press Enter when you're ready to continue..."
    echo ""
  fi
fi

if $USE_SLACK; then
  echo -e "${CYAN}Slack${NC} -- you need a Slack app"
  echo "   https://api.slack.com/apps"
  echo ""
  echo "   Create a new app with Socket Mode enabled."
  echo "   Under OAuth & Permissions, add these bot scopes:"
  echo "     app_mentions:read, chat:write, commands, im:history, im:read, im:write"
  echo "   Under Event Subscriptions, subscribe to: app_mention, message.im"
  echo "   Under Slash Commands, create: /shipmates"
  echo "   Install the app to your workspace."
  echo ""
  read -rp "   Do you have a Slack app configured? (Y/n): " has_slack
  if [[ "$has_slack" =~ ^[Nn]$ ]]; then
    echo ""
    echo "   Follow the steps above at https://api.slack.com/apps"
    echo ""
    read -rp "   Press Enter when you're ready to continue..."
    echo ""
  fi
fi

if $USE_DISCORD; then
  echo -e "${CYAN}Deepgram${NC} -- speech-to-text (free tier available)"
  echo "   https://console.deepgram.com/signup"
  echo ""
  read -rp "   Do you have a Deepgram account? (Y/n): " has_deepgram
  if [[ "$has_deepgram" =~ ^[Nn]$ ]]; then
    echo ""
    echo "   Sign up here: https://console.deepgram.com/signup"
    echo "   After signing up, create an API key from the dashboard."
    echo ""
    read -rp "   Press Enter when you're ready to continue..."
    echo ""
  fi
fi

echo -e "${CYAN}LLM Provider${NC} -- you need one of these:"
echo "   Anthropic (Claude):  https://console.anthropic.com/signup"
echo "   OpenAI:              https://platform.openai.com/signup"
echo "   Google (Gemini):     https://aistudio.google.com"
echo "   Venice:              https://venice.ai"
echo "   Groq:                https://console.groq.com"
echo "   Together:            https://api.together.ai"
echo "   Fireworks:           https://fireworks.ai"
echo "   Custom OpenAI-compatible endpoint"
echo ""
read -rp "   Do you have an account with at least one LLM provider? (Y/n): " has_llm
if [[ "$has_llm" =~ ^[Nn]$ ]]; then
  echo ""
  echo "   Pick one and sign up. Create an API key after signing up."
  echo ""
  read -rp "   Press Enter when you're ready to continue..."
  echo ""
fi

echo -e "${CYAN}GitHub${NC} -- optional, needed for coding tasks"
echo "   https://github.com/settings/tokens"
echo ""
read -rp "   Do you have a GitHub personal access token? (y/N): " has_github
echo ""

if $USE_DISCORD; then
  echo -e "${CYAN}ElevenLabs${NC} -- optional, for cloud text-to-speech voices"
  echo "   https://elevenlabs.io/sign-up"
  echo "   (skip this if you want to use Piper, which is free and runs locally)"
  echo ""
  read -rp "   Do you have an ElevenLabs account? (y/N): " has_elevenlabs
  echo ""
fi

if $USE_DISCORD; then
  print_header "Discord Bot"
  echo "From your Discord application (https://discord.com/developers/applications):"
  echo "  - Bot tab: copy the token"
  echo "  - OAuth2: copy the client ID"
  echo ""

  prompt_required DISCORD_TOKEN "Discord bot token"
  prompt_required DISCORD_CLIENT_ID "Discord client ID (application ID)"
fi

DEEPGRAM_API_KEY=""
if $USE_DISCORD; then
  print_header "Speech-to-Text (Deepgram)"
  echo "From your Deepgram dashboard (https://console.deepgram.com):"
  echo "  - Create an API key if you haven't already"
  echo ""

  prompt_required DEEPGRAM_API_KEY "Deepgram API key"
fi

TTS_PROVIDER="piper"
ELEVENLABS_API_KEY=""

if $USE_DISCORD; then
  print_header "Text-to-Speech"

  if [[ "${has_elevenlabs:-}" =~ ^[Yy]$ ]]; then
    read -rp "Use ElevenLabs instead of Piper (free/local)? (y/N): " use_elevenlabs
    if [[ "$use_elevenlabs" =~ ^[Yy]$ ]]; then
      TTS_PROVIDER="elevenlabs"
      echo ""
      echo "From your ElevenLabs dashboard (https://elevenlabs.io/app/settings/api-keys):"
      echo ""
      prompt_required ELEVENLABS_API_KEY "ElevenLabs API key"
    else
      print_success "Using Piper (free, local TTS)"
    fi
  else
    print_success "Using Piper (free, local TTS)"
  fi
fi

if $USE_SLACK; then
  print_header "Slack Bot"
  echo "From your Slack app (https://api.slack.com/apps):"
  echo "  - OAuth & Permissions: copy the Bot User OAuth Token"
  echo "  - Basic Information > App-Level Tokens: create one with connections:write scope"
  echo "  - Basic Information: copy the Signing Secret"
  echo ""

  prompt_required SLACK_BOT_TOKEN "Slack Bot User OAuth Token (xoxb-...)"
  prompt_required SLACK_APP_TOKEN "Slack App-Level Token (xapp-...)"
  prompt_required SLACK_SIGNING_SECRET "Slack Signing Secret"
fi

print_header "LLM Provider"
echo "Choose your LLM provider."
echo ""
echo "  1) Anthropic (Claude)  -- https://console.anthropic.com/settings#api-keys"
echo "  2) OpenAI              -- https://platform.openai.com/api-keys"
echo "  3) Google (Gemini)     -- https://aistudio.google.com/apikey"
echo "  4) Venice              -- https://venice.ai/settings/api"
echo "  5) Groq                -- https://console.groq.com/keys"
echo "  6) Together            -- https://api.together.ai/settings/api-keys"
echo "  7) Fireworks           -- https://fireworks.ai/account/api-keys"
echo "  8) Custom (OpenAI-compatible endpoint)"
echo ""

LLM_PROVIDER=""
LLM_BASE_URL=""
LLM_MODEL_ID=""
while [ -z "$LLM_PROVIDER" ]; do
  read -rp "Select provider (1-8): " provider_choice
  case "$provider_choice" in
    1) LLM_PROVIDER="anthropic" ;;
    2) LLM_PROVIDER="openai" ;;
    3) LLM_PROVIDER="google" ;;
    4) LLM_PROVIDER="venice" ;;
    5) LLM_PROVIDER="groq" ;;
    6) LLM_PROVIDER="together" ;;
    7) LLM_PROVIDER="fireworks" ;;
    8) LLM_PROVIDER="custom" ;;
    *) print_error "Enter a number between 1 and 8." ;;
  esac
done

echo ""
case "$LLM_PROVIDER" in
  anthropic)  echo "Get your API key at: https://console.anthropic.com/settings#api-keys" ;;
  openai)     echo "Get your API key at: https://platform.openai.com/api-keys" ;;
  google)     echo "Get your API key at: https://aistudio.google.com/apikey" ;;
  venice)     echo "Get your API key at: https://venice.ai/settings/api" ;;
  groq)       echo "Get your API key at: https://console.groq.com/keys" ;;
  together)   echo "Get your API key at: https://api.together.ai/settings/api-keys" ;;
  fireworks)  echo "Get your API key at: https://fireworks.ai/account/api-keys" ;;
  custom)     echo "You'll need the base URL, model ID, and API key for your endpoint." ;;
esac
echo ""

if [ "$LLM_PROVIDER" = "custom" ]; then
  prompt_required LLM_BASE_URL "Base URL (e.g. https://api.example.com/v1)"
  prompt_required LLM_MODEL_ID "Model ID (e.g. my-model-name)"
fi

prompt_required LLM_API_KEY "API key for $LLM_PROVIDER"

print_header "GitHub (Optional)"

GITHUB_TOKEN=""
if [[ "$has_github" =~ ^[Yy]$ ]]; then
  echo "Paste your GitHub personal access token (needs repo scope)."
  echo "Manage tokens at: https://github.com/settings/tokens"
  echo ""
  prompt_optional GITHUB_TOKEN "GitHub personal access token"
else
  echo "Skipping GitHub. Agents won't be able to clone repos or push branches."
  echo "You can add a token later by editing .env."
  echo ""
fi

GITHUB_REPOS=""
if [ -n "$GITHUB_TOKEN" ]; then
  prompt_optional GITHUB_REPOS "GitHub repos (comma-separated, e.g. user/repo1,user/repo2)"
fi

if $USE_DISCORD; then
  print_header "Discord Server"
  echo "Right-click your Discord server name and select 'Copy Server ID'."
  echo "(Enable Developer Mode in Settings > App Settings > Advanced if you don't see it.)"
  echo ""

  prompt_required DISCORD_GUILD_ID "Discord server (guild) ID"
fi

if $USE_SLACK; then
  print_header "Slack Workspace"
  echo "Your Slack workspace ID (starts with T). Find it in your Slack app settings"
  echo "under Basic Information, or in the URL when visiting your workspace admin."
  echo ""

  prompt_required SLACK_WORKSPACE_ID "Slack workspace ID"
fi

print_header "Encryption Key"

ENCRYPTION_KEY=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32)
print_success "Generated encryption key: ${ENCRYPTION_KEY:0:4}****"

print_header "Agent Configuration"
echo "Configure your AI agents. Press Enter to use the defaults (Alex + Sam)."
echo ""

AGENTS=()

read -rp "Use default agents (Alex + Sam)? (Y/n): " use_defaults
if [[ ! "$use_defaults" =~ ^[Nn]$ ]]; then
  AGENTS+=("Alex|Full-Stack Developer|Pragmatic and fast. Ships clean code quickly.|default|src/,lib/|typescript,python")
  AGENTS+=("Sam|Backend Engineer|Methodical. Thinks about edge cases and scalability.|default|api/,services/|go,python")
else
  echo ""
  echo "Enter agents one at a time. Leave name blank when done."
  echo ""

  while true; do
    read -rp "Agent name (blank to finish): " agent_name
    [ -z "$agent_name" ] && break

    read -rp "  Role: " agent_role
    read -rp "  Personality: " agent_personality
    read -rp "  Voice ID [default]: " agent_voice
    agent_voice="${agent_voice:-default}"
    read -rp "  Directories (comma-separated) [src/]: " agent_dirs
    agent_dirs="${agent_dirs:-src/}"
    read -rp "  Languages (comma-separated) [typescript]: " agent_langs
    agent_langs="${agent_langs:-typescript}"

    AGENTS+=("$agent_name|$agent_role|$agent_personality|$agent_voice|$agent_dirs|$agent_langs")
    echo ""
  done

  if [ ${#AGENTS[@]} -eq 0 ]; then
    print_warning "No agents defined, using defaults."
    AGENTS+=("Alex|Full-Stack Developer|Pragmatic and fast. Ships clean code quickly.|default|src/,lib/|typescript,python")
    AGENTS+=("Sam|Backend Engineer|Methodical. Thinks about edge cases and scalability.|default|api/,services/|go,python")
  fi
fi

print_header "Writing .env"

{
  if $USE_DISCORD; then
    echo "DISCORD_TOKEN=$DISCORD_TOKEN"
    echo "DISCORD_CLIENT_ID=$DISCORD_CLIENT_ID"
    echo ""
    echo "DEEPGRAM_API_KEY=$DEEPGRAM_API_KEY"
    echo "ELEVENLABS_API_KEY=$ELEVENLABS_API_KEY"
    echo ""
  fi
  if $USE_SLACK; then
    echo "SLACK_BOT_TOKEN=$SLACK_BOT_TOKEN"
    echo "SLACK_APP_TOKEN=$SLACK_APP_TOKEN"
    echo ""
  fi
  echo "LLM_API_KEY=$LLM_API_KEY"
  echo ""
  echo "GITHUB_TOKEN=$GITHUB_TOKEN"
  echo ""
  echo "ENCRYPTION_KEY=$ENCRYPTION_KEY"
  echo "RELAY_SECRET=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32)"
  echo "AUTH_SECRET=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32)"
  echo ""
  echo "POSTGRES_USER=shipmates"
  echo "POSTGRES_PASSWORD=shipmates"
  echo "POSTGRES_DB=shipmates"
} > .env

print_success "Wrote .env"

print_header "Writing shipmates.config.yml"

{
  if $USE_DISCORD; then
    echo "discord_guild_id: \"$DISCORD_GUILD_ID\""
  fi
  if $USE_SLACK; then
    echo "slack_workspace_id: \"$SLACK_WORKSPACE_ID\""
  fi
  echo ""
  echo "llm:"
  echo "  provider: $LLM_PROVIDER"
  if [ -n "$LLM_BASE_URL" ]; then
    echo "  base_url: \"$LLM_BASE_URL\""
  fi
  if [ -n "$LLM_MODEL_ID" ]; then
    echo "  model_id: \"$LLM_MODEL_ID\""
  fi

  if [ -n "$GITHUB_REPOS" ]; then
    echo ""
    echo "github:"
    echo "  repos:"
    IFS=',' read -ra REPO_LIST <<< "$GITHUB_REPOS"
    for repo in "${REPO_LIST[@]}"; do
      repo=$(echo "$repo" | xargs)
      echo "    - $repo"
    done
  fi

  echo ""
  echo "agents:"

  for agent_str in "${AGENTS[@]}"; do
    IFS='|' read -r name role personality voice dirs langs <<< "$agent_str"
    echo "  - name: $name"
    echo "    role: $role"
    echo "    personality: \"$personality\""
    echo "    voice: $voice"
    echo "    scope:"

    printf "      directories: ["
    IFS=',' read -ra DIR_LIST <<< "$dirs"
    first=true
    for d in "${DIR_LIST[@]}"; do
      d=$(echo "$d" | xargs)
      if $first; then first=false; else printf ", "; fi
      printf "\"%s\"" "$d"
    done
    echo "]"

    printf "      languages: ["
    IFS=',' read -ra LANG_LIST <<< "$langs"
    first=true
    for l in "${LANG_LIST[@]}"; do
      l=$(echo "$l" | xargs)
      if $first; then first=false; else printf ", "; fi
      printf "\"%s\"" "$l"
    done
    echo "]"
    echo ""
  done
} > shipmates.config.yml

print_success "Wrote shipmates.config.yml"

print_header "Building Services"

docker compose build

print_header "Invite Your Bot"

if $USE_DISCORD; then
  echo -e "${CYAN}Discord:${NC}"
  echo "  1. Go to https://discord.com/developers/applications"
  echo "  2. Select your app > OAuth2 > URL Generator"
  echo "  3. Select scopes: bot, applications.commands"
  echo "  4. Select permissions: Connect, Speak, Send Messages, Read Message History, Use Slash Commands"
  echo "  5. Copy the generated URL and open it in your browser to invite the bot to your server"
  echo "  6. Join a voice channel and type /join"
  echo ""
  echo "  Or use this URL (replace CLIENT_ID with your actual client ID):"
  echo "  https://discord.com/oauth2/authorize?client_id=${DISCORD_CLIENT_ID}&scope=bot+applications.commands&permissions=36727824"
  echo ""
fi

if $USE_SLACK; then
  echo -e "${CYAN}Slack:${NC}"
  echo "  Your Slack app was installed to your workspace during setup."
  echo "  To use it in a channel, type: /invite @YourBotName"
  echo "  Then mention it (@YourBotName) or use /shipmates commands."
  echo ""
fi

print_header "Starting Shipmates"

echo "Starting services with docker compose..."
echo "Press Ctrl+C to stop."
echo ""

docker compose up --build
