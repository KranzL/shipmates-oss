FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o stream-relay .

FROM alpine:3.20@sha256:1e42bbe2508154c9126d48c2b8a75420c3544343bf86fd041fb7527e017a4b4a
RUN apk --no-cache add ca-certificates
RUN addgroup -S relay && adduser -S relay -G relay
COPY --from=builder /app/stream-relay /usr/local/bin/stream-relay
EXPOSE 8080
USER relay
ENTRYPOINT ["stream-relay"]
