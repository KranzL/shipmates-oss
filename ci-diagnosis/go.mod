module github.com/KranzL/shipmates-oss/ci-diagnosis

go 1.24

toolchain go1.24.4

require (
	github.com/KranzL/shipmates-oss/go-github v0.0.0
	github.com/KranzL/shipmates-oss/go-llm v0.0.0
	github.com/lib/pq v1.10.9
)

replace (
	github.com/KranzL/shipmates-oss/go-github => ../go-github
	github.com/KranzL/shipmates-oss/go-llm => ../go-llm
)
