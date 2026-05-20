# Docker example

Runs `odata-mcp` in SSE mode alongside a MinIO instance that holds the EDMX metadata XML files.

## Quick start

```bash
# 1. Create a .env file from the template
cp .env.example .env
# Edit .env with your OData credentials

# 2. Start the stack
docker compose up -d

# 3. Upload your metadata XML to MinIO
#    Open http://localhost:9001 (minioadmin / minioadmin)
#    Create bucket "odata-metadata", upload your *.xml files under the metaData/ prefix
#    e.g. metaData/my-service.xml

# 4. Verify health
curl http://localhost:8008/health
```

## Connecting from Claude Desktop (SSE mode)

In Claude Desktop settings → MCP → Add server → HTTP:

```
http://localhost:8008
```

## Files

| File | Purpose |
|---|---|
| `docker-compose.yml` | odata-mcp + MinIO stack |
| `.env.example` | Environment variable template |
