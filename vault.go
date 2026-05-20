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
	vault "github.com/bluefunda/go-vault"
	"go.uber.org/zap"
)

// vaultSecrets holds the NATS credentials loaded from Vault.
type vaultSecrets struct {
	NATSUrl       string
	NATSCredsFile string
}

// loadVaultSecrets loads NATS URL and credentials from HashiCorp Vault KV v2.
//
// Mirrors vault.py's load_secrets_from_vault():
//   - NATS URL from secret/infra/nats/config → "url"
//   - NATS creds from secret/infra/nats/creds/individual/admin → "creds_file"
//
// Returns an empty struct (not an error) if VAULT_ADDR or VAULT_TOKEN are absent
// so the server can start with env-var-only configuration.
func loadVaultSecrets() vaultSecrets {
	var result vaultSecrets

	if os.Getenv("VAULT_ADDR") == "" || os.Getenv("VAULT_TOKEN") == "" {
		logger.L.Debug("VAULT_ADDR or VAULT_TOKEN not set, skipping Vault secret loading")
		return result
	}

	vc, err := vault.NewClientFromEnv()
	if err != nil {
		logger.L.Warn("Failed to create Vault client", zap.Error(err))
		return result
	}

	// NATS URL — single source of truth for all services
	if url, err := vc.GetField("infra/nats/config", "url"); err == nil && url != "" {
		result.NATSUrl = url
	} else if err != nil {
		logger.L.Warn("Could not read NATS URL from Vault", zap.Error(err))
	}

	// NATS credentials file content
	credsContent, err := vc.GetField("infra/nats/creds/individual/admin", "creds_file")
	if err != nil {
		logger.L.Warn("Could not read NATS creds from Vault", zap.Error(err))
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
