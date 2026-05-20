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

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// Version information (set via ldflags during build).
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("odata-mcp version %s\n", Version)
		fmt.Printf("Build time: %s\n", BuildTime)
		fmt.Printf("Git commit: %s\n", GitCommit)
		os.Exit(0)
	}

	logLevel := getEnv("LOG_LEVEL", "info")
	logFormat := getEnv("LOG_FORMAT", "json")
	_ = logger.Init(logger.Config{
		Level:      logLevel,
		Format:     logFormat,
		ServerName: "odata-mcp",
		Version:    Version,
	})
	defer logger.Sync()

	cfg := LoadConfig()

	logger.L.Info("Starting odata-mcp MCP server",
		zap.String("mode", cfg.Mode),
		zap.String("log_level", logLevel),
		zap.String("version", Version),
	)

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "odata-mcp",
			Version: Version,
		},
		nil,
	)

	handlers := NewHandlers(cfg)

	// Initialise server state: NATS → S3 → parse metadata → build OData clients.
	// A failure here is fatal; the MCP tool cannot function without metadata.
	initCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	if err := handlers.Initialize(initCtx); err != nil {
		cancel()
		logger.L.Fatal("Server initialisation failed", zap.Error(err))
	}
	cancel()

	registerTools(server, handlers)
	registerResources(server, handlers)

	ctx := context.Background()
	switch cfg.Mode {
	case "sse":
		runSSEMode(ctx, server, cfg)
	default:
		runStdioMode(ctx, server)
	}
}

func runStdioMode(ctx context.Context, server *mcp.Server) {
	logger.L.Info("Running in stdio mode")
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.L.Fatal("Server error", zap.Error(err))
	}
}

func runSSEMode(ctx context.Context, server *mcp.Server, cfg *Config) {
	logger.L.Info("Running in SSE/HTTP mode",
		zap.String("host", cfg.HTTPHost),
		zap.String("port", cfg.HTTPPort),
		zap.Bool("legacy_sse", cfg.UseLegacySSE),
	)

	var handler http.Handler
	if cfg.UseLegacySSE {
		handler = mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
			logger.L.Debug("SSE client connected",
				zap.String("remote_addr", req.RemoteAddr),
			)
			return server
		}, nil)
	} else {
		handler = mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
			logger.L.Debug("HTTP client connected",
				zap.String("remote_addr", req.RemoteAddr),
			)
			return server
		}, &mcp.StreamableHTTPOptions{
			Stateless:      false,
			JSONResponse:   false,
			SessionTimeout: 30 * time.Minute,
		})
	}

	mux := http.NewServeMux()
	mux.Handle("/", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"healthy","version":%q,"mode":"sse"}`, Version)
	})

	addr := fmt.Sprintf("%s:%s", cfg.HTTPHost, cfg.HTTPPort)
	logger.L.Info("SSE/HTTP MCP server listening",
		zap.String("address", "http://"+addr),
		zap.String("health_check", "http://"+addr+"/health"),
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.L.Fatal("HTTP server error", zap.Error(err))
	}
}
