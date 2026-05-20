# odata-mcp

[![Go Reference](https://pkg.go.dev/badge/github.com/bluefunda/odata-mcp.svg)](https://pkg.go.dev/github.com/bluefunda/odata-mcp)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/bluefunda/odata-mcp)](https://goreportcard.com/report/github.com/bluefunda/odata-mcp)

A [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server that connects AI assistants to any OData v2/v4 service. Ask a natural-language question; the server identifies the right EntitySet, queries the service, and returns real data — no SQL, no manual API calls.

Works with Claude Desktop, Claude Code, Cursor, Windsurf, and any MCP-compatible host.

## How it works

```
AI assistant (Claude Desktop / Claude Code / Cursor / Windsurf)
  → MCP protocol (stdio or HTTP/SSE)
    → odata-mcp  ← this repo
      → OData service (SAP BTP, SAP on-prem, Microsoft, any OData v2/v4)
```

On startup, `odata-mcp` reads EDMX metadata XML to discover available EntitySets and their properties. At query time, it matches the user's natural language to the right EntitySet (heuristic matching + optional Anthropic LLM fallback) and fetches live data.

## Installation

```bash
go install github.com/bluefunda/odata-mcp@latest
```

Or build from source:

```bash
git clone https://github.com/bluefunda/odata-mcp.git
cd odata-mcp
make build
```

Docker:

```bash
docker pull bluefunda/odata-mcp:latest
```

## Quick start: Claude Desktop

Add to your Claude Desktop configuration:

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`  
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "odata": {
      "command": "/usr/local/bin/odata-mcp",
      "env": {
        "ODATA_METADATA_PATH": "/path/to/your/metadata.xml",
        "ODATA_USERNAME": "your-username",
        "ODATA_PASSWORD": "your-password"
      }
    }
  }
}
```

Restart Claude Desktop. The server starts automatically via stdio.

See [`examples/`](examples/) for Claude Desktop, Cursor, and Docker Compose configurations.

## Metadata store

The server needs EDMX metadata XML to discover available entities. Choose one store:

### Local file (Claude Desktop / local development)

```bash
# Single XML file
ODATA_METADATA_PATH=/path/to/metadata.xml odata-mcp

# Directory of XML files (all *.xml files discovered recursively)
ODATA_METADATA_PATH=/path/to/xml-dir/ odata-mcp
```

### S3-compatible object storage

Works with AWS S3, MinIO, Cloudflare R2, GCS S3 interop, and any S3-compatible endpoint.

```bash
S3_ENDPOINT=localhost:9000 \
S3_BUCKET=odata-metadata \
S3_ACCESS_KEY=minioadmin \
S3_SECRET_KEY=minioadmin \
odata-mcp
```

### NATS JetStream KV (production)

Reads S3 and OData credentials from a NATS JetStream KV bucket (`ODataMCPConfigBucket` by default), then loads EDMX from S3.

```bash
NATS_URL=nats://nats.internal:4222 \
NATS_CREDS=/path/to/user.creds \
odata-mcp
```

## Modes

Set via `ODATA_MODE` env var:

| Mode | Transport | Use case |
|------|-----------|----------|
| `stdio` (default) | Standard I/O | Claude Desktop, Claude Code CLI, IDE extensions |
| `sse` | HTTP/SSE on `:8008` | Orchestrators, Docker, programmatic access |

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| **Metadata store** (one required) | | | |
| `ODATA_METADATA_PATH` | * | — | Path to a local `.xml` file or directory of `.xml` files |
| `S3_ENDPOINT` | * | — | S3-compatible endpoint (e.g. `localhost:9000`, `s3.amazonaws.com`) |
| `S3_BUCKET` | if S3 | `ODataMCPConfigBucket` | Bucket containing EDMX files under the `metaData/` prefix |
| `S3_ACCESS_KEY` | if S3 | — | S3 access key |
| `S3_SECRET_KEY` | if S3 | — | S3 secret key |
| `S3_SECURE` | No | `false` | Use HTTPS for S3 (`true`/`false`) |
| `NATS_URL` | * | — | NATS server URL; triggers cache-backed store selection |
| `NATS_CREDS` | if NATS | — | Path to NATS credentials file |
| **OData credentials** | | | |
| `ODATA_USERNAME` | No | — | OData service username |
| `ODATA_PASSWORD` | No | — | OData service password or bearer token |
| `ODATA_AUTH_TYPE` | No | `basic` | `basic`, `bearer`, or omit for no auth |
| **Transport** | | | |
| `ODATA_MODE` | No | `stdio` | `stdio` or `sse` |
| `ODATA_HTTP_PORT` | No | `8008` | HTTP port for SSE mode |
| `ODATA_HTTP_HOST` | No | `0.0.0.0` | HTTP host for SSE mode |
| `ODATA_USE_LEGACY_SSE` | No | `true` | Use legacy SSE transport (`true`) or streamable HTTP (`false`) |
| **LLM fallback** | | | |
| `ANTHROPIC_API_KEY` | No | — | Enables LLM entity-selection fallback when heuristic matching fails |
| `ANTHROPIC_MODEL` | No | `claude-3-5-sonnet-latest` | Anthropic model for entity selection |
| `ANTHROPIC_BASE_URL` | No | `https://api.anthropic.com` | Anthropic API base URL |
| **Vault** | | | |
| `VAULT_ADDR` | No | — | HashiCorp Vault address; enables NATS credential loading from Vault |
| `VAULT_TOKEN` | No | — | Vault token |
| **Logging** | | | |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | No | `json` | `json` or `console` |

## MCP capabilities

### Tool

| Tool | Description |
|------|-------------|
| `query-odata-service` | Natural-language query over any discovered OData EntitySet. Returns real records from the service. |

The tool description is built dynamically at startup and includes the full list of available EntitySets, so the AI assistant can route queries without additional context.

**Example queries:**
- "list all countries"
- "show me open purchase orders"
- "get currencies for Germany"
- "what business partners are available?"

### Resources

| Resource URI | Description |
|---|---|
| `mcp://odata_server_llm/metadata.xml` | Raw EDMX XML of the first discovered metadata file |
| `mcp://odata_server_llm/metadata-info.json` | JSON summary of all services, EntitySets, and file sources |

## Entity matching

Queries are matched to EntitySets in two passes:

1. **Heuristic** — exact substring match, plural forms (`Countries` → `Country`), and space-stripped partial match. Prefers the longest matching name.
2. **LLM fallback** — when the heuristic fails and `ANTHROPIC_API_KEY` is set, the server calls the Anthropic Messages API with the entity list and user query to select the best match.

## Development

```bash
make build   # compile binary to bin/odata-mcp
make test    # run tests with race detector
make fmt     # gofmt + goimports
make lint    # golangci-lint
make tidy    # go mod tidy
```

### Project structure

```
main.go            # entry point, transport routing (stdio / SSE)
config.go          # Config struct, environment variable loading, Vault overrides
handlers.go        # Handlers struct, Initialize(), store selection
tools.go           # query-odata-service MCP tool
resources.go       # MCP resources (metadata.xml, metadata-info.json)
metadata.go        # EDMX XML parser (token-stream, SAP namespace-aware)
odataclient.go     # HTTP OData client, filter builder, response normaliser
llm.go             # Anthropic API entity-selection fallback
store.go           # MetadataStore interface
store_s3.go        # S3-compatible store (MinIO Go SDK)
store_local.go     # Local filesystem store
cache.go           # CacheStore interface + config helpers
cache_nats.go      # NATS JetStream KV implementation of CacheStore
vault.go           # HashiCorp Vault secret loading
internal/logger/   # Structured logging (zap)
examples/          # Claude Desktop, Cursor, Docker Compose configs
```

Everything is `package main`. Do not introduce sub-packages.

## Support model

`odata-mcp` is maintained by BlueFunda. Security fixes are released within 7 days of disclosure. Feature requests are evaluated quarterly. Please open a GitHub issue for bug reports or enhancement proposals.

## License

Apache 2.0 — see [LICENSE](LICENSE).

Authored by Phani Pavan, open-sourced under Apache 2.0 by BlueFunda, Inc.
