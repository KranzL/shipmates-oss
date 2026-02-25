FROM node:22-slim
RUN npm install -g @anthropic-ai/claude-code
RUN apt-get update && apt-get install -y git && rm -rf /var/lib/apt/lists/*
RUN useradd -m -s /bin/bash coder
USER coder
WORKDIR /workspace
ENTRYPOINT ["claude", "-p", "--dangerously-skip-permissions"]
