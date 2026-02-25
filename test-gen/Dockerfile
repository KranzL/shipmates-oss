FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go-llm/ /go-llm/
COPY go-github/ /go-github/
COPY test-gen/go.mod test-gen/go.sum ./
RUN go mod download

COPY test-gen/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o test-gen .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates git docker-cli wget && \
    adduser -D -u 1000 appuser

COPY --from=builder /app/test-gen /usr/local/bin/test-gen

USER appuser

EXPOSE 8095

ENTRYPOINT ["test-gen"]
