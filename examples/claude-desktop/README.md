# Claude Desktop examples

Two configuration variants — choose the one that matches your setup.

## Local metadata file (simplest)

Suitable for a single OData service whose metadata XML you have on disk.

Copy `claude_desktop_config.json` and adjust the three values:

| Field | Description |
|---|---|
| `ODATA_METADATA_PATH` | Absolute path to your EDMX metadata XML file or a directory of XML files |
| `ODATA_USERNAME` | OData service username |
| `ODATA_PASSWORD` | OData service password |

Config file locations:

- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

## S3 metadata store

Use `claude_desktop_config.s3.json` if your metadata XML lives in S3-compatible object storage. Fill in the S3 endpoint, bucket, and credentials alongside the OData credentials.

## After editing

Restart Claude Desktop. You should see `odata-mcp` listed in the MCP servers section of Claude settings.

Try asking: `list all available entities` or `show me the first 10 records from Countries`.
