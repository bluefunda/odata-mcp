# Contributing to odata-mcp

Thank you for your interest in contributing! This document covers setup, conventions, and the pull request process.

## Development setup

### Prerequisites

- Go 1.25 or later
- An OData v2 or v4 service with EDMX metadata (for integration testing)
- Optional: MinIO or any S3-compatible store for metadata XML files

### Getting started

```bash
git clone https://github.com/bluefunda/odata-mcp.git
cd odata-mcp
make build
make test
```

### Local smoke test

Point the server at a local metadata file and run in console mode:

```bash
LOG_FORMAT=console ODATA_METADATA_PATH=/path/to/metadata.xml ./bin/odata-mcp
```

## Code style

- Standard Go conventions (`gofmt`, `goimports`)
- Run `make fmt` before committing
- Run `make lint` and fix all reported issues
- No sub-packages — everything stays in `package main`
- No new external dependencies without discussion

## Conventional commits

All commit messages must follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): subject
```

| Type | When to use |
|------|-------------|
| `feat` | New feature visible to users |
| `fix` | Bug fix |
| `perf` | Performance improvement |
| `docs` | Documentation only |
| `test` | Tests only |
| `refactor` | Refactoring without behaviour change |
| `ci` | CI/CD workflow changes |
| `chore` | Dependency bumps, housekeeping |

Examples:

```
feat(tools): add $top and $skip query parameters to query-odata-service
fix(metadata): handle EDMX files without atom:link in root Schema
docs(examples): add Cursor MCP configuration example
```

## Adding a new metadata store

1. Create `store_<name>.go`
2. Implement the `MetadataStore` interface (`ListXMLFiles`, `GetXMLContent`)
3. Add a compile-time check: `var _ MetadataStore = (*myStore)(nil)`
4. Add a constructor `newMyStore(...)` and wire it into `buildStore` in `handlers.go`
5. Document the required env vars in `config.go` and `README.md`

## Adding a new cache backend

1. Create `cache_<name>.go`
2. Implement `CacheStore` (`GetConfig`, `Close`)
3. Add a compile-time check: `var _ CacheStore = (*myCache)(nil)`

## Pull request process

1. Fork and create a feature branch
2. Make changes, add/update tests, run `make fmt lint test`
3. Open a pull request against `main`
4. Fill in the pull request template
5. Link any related issues

### Pull request checklist

- [ ] `go test ./...` passes
- [ ] `golangci-lint run ./...` reports no issues
- [ ] Tested with a real OData service (stdio mode minimum)
- [ ] `README.md` updated if user-facing behaviour changed
- [ ] Commit messages follow Conventional Commits format

## Questions or issues?

Open a GitHub issue. Check existing issues and documentation first.

## License

By contributing, you agree your contributions will be licensed under the Apache 2.0 License.
