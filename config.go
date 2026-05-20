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
	"os"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"go.uber.org/zap"
)

const (
	defaultNATSKVBucket   = "ODataMCPConfigBucket"
	defaultHTTPPort       = "8008"
	defaultHTTPHost       = "0.0.0.0"
	defaultAnthropicBase  = "https://api.anthropic.com"
	defaultAnthropicPath  = "/v1/messages"
	defaultAnthropicModel = "claude-3-5-sonnet-latest"
	metaDataPrefix        = "metaData"
)

// Config holds all runtime configuration for the server.
// Values are sourced from environment variables, with Vault overrides applied
// for NATS_URL and NATS_CREDS when VAULT_ADDR and VAULT_TOKEN are set.
type Config struct {
	// Transport
	Mode     string // stdio (default) or sse
	HTTPPort string
	HTTPHost string
	// Legacy SSE vs. streamable HTTP (SSE mode only)
	UseLegacySSE bool

	// NATS
	NATSUrl            string
	NATSCreds          string
	NATSConnectTimeout int
	NATSKVBucket       string

	// Direct S3-compatible object storage (used when NATS_URL is absent).
	// Works with AWS S3, MinIO, Cloudflare R2, GCS S3-interop, and any
	// S3-compatible endpoint.
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3Secure    bool

	// Local metadata path (used when neither NATS nor object-store env vars are set).
	// May be a single .xml file or a directory of .xml files.
	// Intended for Claude Desktop / local development.
	ODataMetadataPath string

	// OData service credentials — env-var fallback for all store paths.
	// When NATS is used these are overridden by the ODataConfig KV key.
	ODataUsername string
	ODataPassword string
	ODataAuthType string

	// Anthropic API (LLM entity-selection fallback)
	AnthropicAPIKey  string
	AnthropicBaseURL string
	AnthropicAPIPath string
	AnthropicModel   string
}

// LoadConfig builds a Config from environment variables, then applies any
// Vault overrides for NATS credentials.
func LoadConfig() *Config {
	cfg := &Config{
		Mode:               getEnv("ODATA_MODE", "stdio"),
		HTTPPort:           getEnv("ODATA_HTTP_PORT", defaultHTTPPort),
		HTTPHost:           getEnv("ODATA_HTTP_HOST", defaultHTTPHost),
		UseLegacySSE:       getEnv("ODATA_USE_LEGACY_SSE", "true") == "true",
		NATSUrl:            getEnv("NATS_URL", ""),
		NATSCreds:          getEnv("NATS_CREDS", ""),
		NATSConnectTimeout: 120,
		NATSKVBucket:       defaultNATSKVBucket,
		S3Endpoint:  os.Getenv("S3_ENDPOINT"),
		S3AccessKey: os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey: os.Getenv("S3_SECRET_KEY"),
		S3Bucket:    getEnv("S3_BUCKET", defaultNATSKVBucket),
		S3Secure:    os.Getenv("S3_SECURE") == "true",

		ODataMetadataPath: os.Getenv("ODATA_METADATA_PATH"),
		ODataUsername:     os.Getenv("ODATA_USERNAME"),
		ODataPassword:     os.Getenv("ODATA_PASSWORD"),
		ODataAuthType:     getEnv("ODATA_AUTH_TYPE", "basic"),

		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicBaseURL: getEnv("ANTHROPIC_BASE_URL", defaultAnthropicBase),
		AnthropicAPIPath: getEnv("ANTHROPIC_API_PATH", defaultAnthropicPath),
		AnthropicModel:   getEnv("ANTHROPIC_MODEL", defaultAnthropicModel),
	}

	// Decode URL-encoded Anthropic API key (mirrors tools.py URL-decoding logic)
	cfg.AnthropicAPIKey = decodeAPIKey(cfg.AnthropicAPIKey)

	// Apply Vault overrides when credentials are available
	secrets := loadVaultSecrets()
	if secrets.NATSUrl != "" {
		cfg.NATSUrl = secrets.NATSUrl
		logger.L.Info("NATS URL loaded from Vault")
	}
	if secrets.NATSCredsFile != "" {
		cfg.NATSCreds = secrets.NATSCredsFile
		logger.L.Info("NATS credentials loaded from Vault", zap.String("path", secrets.NATSCredsFile))
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// decodeAPIKey URL-decodes an API key if it appears to contain percent-encoding.
// This mirrors the Python tools.py handling of URL-encoded ANTHROPIC_API_KEY values.
func decodeAPIKey(key string) string {
	if key == "" {
		return key
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '%' && i+2 < len(key) {
			// Attempt URL decoding
			decoded := urlDecodeKey(key)
			if decoded != key {
				logger.L.Info("Detected URL-encoded ANTHROPIC_API_KEY; decoded for use")
				return decoded
			}
			break
		}
	}
	return key
}

func urlDecodeKey(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) {
			hi := hexVal(s[i+1])
			lo := hexVal(s[i+2])
			if hi >= 0 && lo >= 0 {
				result = append(result, byte(hi<<4|lo))
				i += 3
				continue
			}
		}
		result = append(result, s[i])
		i++
	}
	return string(result)
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}
