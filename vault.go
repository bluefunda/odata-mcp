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
	"crypto/tls"
	"fmt"
	"net/http"
	"os"

	"github.com/bluefunda/odata-mcp/internal/logger"
	vaultapi "github.com/hashicorp/vault/api"
	"go.uber.org/zap"
)

// SecretsProvider retrieves individual string fields from a named secret.
//
// Path semantics vary by backend:
//   - HashiCorp Vault KV v2: relative KV path (e.g. "infra/nats/config")
//   - AWS Secrets Manager: secret name or ARN; field selects a JSON key
type SecretsProvider interface {
	GetField(path, field string) (string, error)
}

// vaultSecrets holds the NATS credentials loaded from a SecretsProvider.
type vaultSecrets struct {
	NATSUrl       string
	NATSCredsFile string
}

// newSecretsProvider returns the active SecretsProvider based on environment
// variables, or nil if no provider is configured.
//
// Detection order:
//  1. VAULT_ADDR + VAULT_TOKEN → HashiCorp Vault KV v2
//  2. AWS_REGION (+ optional AWS_SECRET_PREFIX) → AWS Secrets Manager (future)
func newSecretsProvider() SecretsProvider {
	if os.Getenv("VAULT_ADDR") != "" && os.Getenv("VAULT_TOKEN") != "" {
		p, err := newHashiCorpProvider()
		if err != nil {
			logger.L.Warn("Failed to create HashiCorp Vault client", zap.Error(err))
			return nil
		}
		return p
	}
	return nil
}

// loadVaultSecrets resolves NATS secrets from the active SecretsProvider.
// Returns an empty struct (not an error) when no provider is configured.
func loadVaultSecrets() vaultSecrets {
	var result vaultSecrets

	p := newSecretsProvider()
	if p == nil {
		logger.L.Debug("No secrets provider configured, skipping secret loading")
		return result
	}

	if url, err := p.GetField("infra/nats/config", "url"); err == nil && url != "" {
		result.NATSUrl = url
	} else if err != nil {
		logger.L.Warn("Could not read NATS URL from secrets provider", zap.Error(err))
	}

	credsContent, err := p.GetField("infra/nats/creds/individual/admin", "creds_file")
	if err != nil {
		logger.L.Warn("Could not read NATS creds from secrets provider", zap.Error(err))
	} else if credsContent != "" {
		tmpFile, err := os.CreateTemp("", "nats-creds-*.creds")
		if err != nil {
			logger.L.Warn("Failed to create temp file for NATS creds", zap.Error(err))
		} else {
			if _, err := tmpFile.WriteString(credsContent); err != nil {
				_ = tmpFile.Close()
				_ = os.Remove(tmpFile.Name())
				logger.L.Warn("Failed to write NATS creds to temp file", zap.Error(err))
			} else {
				_ = tmpFile.Close()
				result.NATSCredsFile = tmpFile.Name()
			}
		}
	}

	return result
}

// hashiCorpProvider implements SecretsProvider for HashiCorp Vault KV v2.
//
// Configured via environment variables:
//   - VAULT_ADDR: Vault server address (required)
//   - VAULT_TOKEN: authentication token (required)
//   - VAULT_MOUNT: KV v2 mount path (default: "secret")
//   - VAULT_SKIP_VERIFY: skip TLS verification, "true" or "1" (optional)
type hashiCorpProvider struct {
	client *vaultapi.Client
	mount  string
}

func newHashiCorpProvider() (*hashiCorpProvider, error) {
	cfg := vaultapi.DefaultConfig()
	cfg.Address = os.Getenv("VAULT_ADDR")

	if os.Getenv("VAULT_SKIP_VERIFY") == "true" || os.Getenv("VAULT_SKIP_VERIFY") == "1" {
		cfg.HttpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // user-configured for self-signed certs
			},
		}
	}

	client, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("vault client: %w", err)
	}
	client.SetToken(os.Getenv("VAULT_TOKEN"))

	return &hashiCorpProvider{
		client: client,
		mount:  getEnv("VAULT_MOUNT", "secret"),
	}, nil
}

func (p *hashiCorpProvider) GetField(path, field string) (string, error) {
	secret, err := p.client.Logical().Read(fmt.Sprintf("%s/data/%s", p.mount, path))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("no secret at %s", path)
	}
	data, ok := secret.Data["data"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected data format at %s", path)
	}
	v, ok := data[field]
	if !ok {
		return "", fmt.Errorf("field %q not found at %s", field, path)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q at %s is not a string", field, path)
	}
	return s, nil
}
