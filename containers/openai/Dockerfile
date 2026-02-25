FROM node:22-slim
RUN npm install -g @openai/codex
RUN apt-get update && apt-get install -y git && rm -rf /var/lib/apt/lists/*
RUN useradd -m -s /bin/bash coder
USER coder
WORKDIR /workspace
ENTRYPOINT ["codex", "exec", "--full-auto"]
