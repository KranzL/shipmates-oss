FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go-llm/ /go-llm/
COPY go-github/ /go-github/
COPY ci-diagnosis/go.mod ci-diagnosis/go.sum ./
RUN go mod download

COPY ci-diagnosis/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o ci-diagnosis .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates git && \
    adduser -D -u 1000 appuser

COPY --from=builder /app/ci-diagnosis /usr/local/bin/ci-diagnosis

USER appuser

EXPOSE 8093

ENTRYPOINT ["ci-diagnosis"]
