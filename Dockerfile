# Copyright 2025 bluefunda
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.

# ---- Build stage ------------------------------------------------------------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache module downloads separately from source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags "-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o /bin/odata-mcp .

# ---- Runtime stage ----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/odata-mcp /usr/local/bin/odata-mcp

# Default: stdio mode (override with ODATA_MODE=sse for HTTP)
ENV ODATA_MODE=stdio

ENTRYPOINT ["/usr/local/bin/odata-mcp"]
