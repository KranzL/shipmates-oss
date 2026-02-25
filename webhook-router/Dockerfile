FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o webhook-router .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1000 appuser

COPY --from=builder /app/webhook-router /usr/local/bin/webhook-router

USER appuser

EXPOSE 8094

ENTRYPOINT ["webhook-router"]
