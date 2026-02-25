FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go-shared /build/go-shared
COPY go-llm /build/go-llm
COPY bot /build/bot
WORKDIR /build/bot
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o bot .

FROM alpine:3.21
RUN apk --no-cache add ca-certificates docker-cli
RUN addgroup -S bot && adduser -S bot -G bot
COPY --from=builder /build/bot/bot /usr/local/bin/bot
WORKDIR /app
EXPOSE 8091
USER bot
ENTRYPOINT ["bot"]
