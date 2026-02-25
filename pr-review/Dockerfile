FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go-llm/ /go-llm/
COPY go-github/ /go-github/
COPY pr-review/go.mod pr-review/go.sum ./
RUN go mod download

COPY pr-review/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o pr-review .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1000 appuser

COPY --from=builder /app/pr-review /usr/local/bin/pr-review

USER appuser

EXPOSE 8092

ENTRYPOINT ["pr-review"]
