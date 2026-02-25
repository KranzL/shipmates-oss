module github.com/KranzL/shipmates-oss/test-gen

go 1.24

toolchain go1.24.4

replace (
	github.com/KranzL/shipmates-oss/go-github => ../go-github
	github.com/KranzL/shipmates-oss/go-llm => ../go-llm
)

require (
	github.com/KranzL/shipmates-oss/go-github v0.0.0-00010101000000-000000000000
	github.com/KranzL/shipmates-oss/go-llm v0.0.0-00010101000000-000000000000
	github.com/gorilla/websocket v1.5.3
	github.com/lib/pq v1.11.2
)
