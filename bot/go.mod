module github.com/KranzL/shipmates-oss/bot

go 1.24.0

toolchain go1.24.4

require (
	github.com/KranzL/shipmates-oss/go-llm v0.0.0-20260221180811-9db94bacafca
	github.com/KranzL/shipmates-oss/go-shared v0.0.0-00010101000000-000000000000
	github.com/bwmarrin/discordgo v0.28.1
	github.com/docker/docker v28.0.0+incompatible
	github.com/gorilla/websocket v1.5.3
	github.com/hraban/opus v0.0.0-20230925203106-0188a62cb302
	github.com/slack-go/slack v0.14.0
	golang.org/x/time v0.14.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/adhocore/gronx v1.19.6 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/lib/pq v1.11.2 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/morikuni/aec v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.60.0 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)

replace (
	github.com/KranzL/shipmates-oss/go-llm => ../go-llm
	github.com/KranzL/shipmates-oss/go-shared => ../go-shared
)
