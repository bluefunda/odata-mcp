// Copyright 2025 bluefunda
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// odata-mcp is a Model Context Protocol (MCP) server that connects AI
// assistants to any OData v2/v4 service. It parses EDMX metadata to discover
// available EntitySets and exposes natural-language tools that select the
// right entity, query the service, and return real data.
//
// # Tools
//
//   - query-odata-service: natural-language query across all discovered
//     EntitySets. Returns an error naming the candidate services (and pointing
//     at query-service) if the same EntitySet name is defined by more than one
//     service.
//   - query-service: natural-language query scoped to one explicitly chosen
//     service, for disambiguating EntitySet names that collide across services.
//   - list-available-services: lists discovered services with their base URLs
//     and EntitySets.
//   - list-metadata-files: lists the metadata source identifiers processed at
//     startup.
//   - generate-chart: suggests a structured chart configuration (chart type,
//     axes, grouping, insight) for a set of data records; does not render an
//     image itself.
//
// Supported AI clients: Claude Desktop, Claude Code, Cursor, Windsurf, and
// any MCP-compatible host.
//
// # Installation
//
//	go install github.com/bluefunda/odata-mcp@latest
//
// # Metadata store (pick one)
//
// Local file or directory — for Claude Desktop and local development:
//
//	ODATA_METADATA_PATH=/path/to/metadata.xml odata-mcp
//	ODATA_METADATA_PATH=/path/to/xml-dir/     odata-mcp
//
// SAP EDMX embeds atom:link[@rel="self"] to identify the service base URL;
// plain OData v2/v4 services (e.g. services.odata.org) often don't. Set
// ODATA_SERVICE_BASE_URL as a fallback for those — without it, such a file is
// skipped with a warning rather than failing the whole server:
//
//	ODATA_METADATA_PATH=/path/to/northwind.xml \
//	ODATA_SERVICE_BASE_URL=https://services.odata.org/V2/Northwind/Northwind.svc odata-mcp
//
// S3-compatible object storage (AWS S3, MinIO, R2, GCS interop):
//
//	S3_ENDPOINT=localhost:9000 S3_BUCKET=my-bucket \
//	S3_ACCESS_KEY=... S3_SECRET_KEY=... odata-mcp
//
// NATS JetStream KV (production; stores S3 and OData config in KV):
//
//	NATS_URL=nats://nats.internal:4222 NATS_CREDS=/path/to/user.creds odata-mcp
//
// # OData credentials
//
//	ODATA_USERNAME=user ODATA_PASSWORD=secret ODATA_AUTH_TYPE=basic odata-mcp
//
// auth types: basic (default), bearer, or omit for no auth.
//
// # Transport modes
//
// stdio (default) — for use with Claude Desktop and IDE extensions:
//
//	odata-mcp
//
// SSE / HTTP — for remote or container deployments:
//
//	ODATA_MODE=sse ODATA_HTTP_PORT=8008 odata-mcp
//
// # LLM entity-selection fallback
//
// When the heuristic name-matching fails, the server calls the Anthropic
// Messages API to select the best EntitySet for the user's query:
//
//	ANTHROPIC_API_KEY=sk-ant-... odata-mcp
//
// # Vault integration
//
// Set VAULT_ADDR and VAULT_TOKEN to load NATS credentials from HashiCorp Vault:
//
//	VAULT_ADDR=https://vault.internal VAULT_TOKEN=... odata-mcp
package main
