# Contributing

Thank you for improving Multi ISP Proxy. Keep changes scoped and include tests
for behavior that can regress.

## Local checks

```sh
gofmt -w .
go mod tidy
go vet ./...
go test -race ./...
```

Build both supported targets when changing platform boundaries:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/multi-isp-proxy
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/multi-isp-proxy
```

Linux contributions should preserve build tags and avoid claiming automatic
desktop proxy integration unless it is implemented and tested for the relevant
desktop environments. Windows registry changes must preserve the journal-first,
restore-before-delete transaction invariant documented in
[`docs/architecture.md`](docs/architecture.md).

Use semantic commit messages where practical. Releases are created only from
semantic version tags after the reusable CI workflow succeeds.
