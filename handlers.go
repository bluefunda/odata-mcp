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

	"github.com/bluefunda/odata-mcp/internal/logger"
	"go.uber.org/zap"
)

// ServiceInfo groups the parsed metadata and OData client for one base URL.
type ServiceInfo struct {
	BaseURL  string
	Entities map[string]EntityInfo // keyed by EntitySet name
	Files    []string              // store IDs that contributed
	Client   *ODataClient
}

// Handlers holds all server-level state built during initialisation.
type Handlers struct {
	cfg *Config

	// services maps service base URL → ServiceInfo
	services map[string]*ServiceInfo

	// entityIndex maps EntitySet name → base URL (fast lookup for entity matching)
	entityIndex map[string]string

	// store is the MetadataStore chosen by Initialize().
	store MetadataStore

	// firstMetadataID is the store ID of the first discovered metadata file,
	// served by the metadata.xml MCP resource.
	firstMetadataID string

	// allMetadataIDs lists all processed store IDs.
	allMetadataIDs []string

	// allEntityNames is the cached flat list of EntitySet names built once at the
	// end of Initialize; AllEntityNames() returns it without allocating.
	allEntityNames []string
}

// NewHandlers creates a Handlers with empty state; call Initialize() to populate.
func NewHandlers(cfg *Config) *Handlers {
	return &Handlers{
		cfg:         cfg,
		services:    make(map[string]*ServiceInfo),
		entityIndex: make(map[string]string),
	}
}

// Initialize selects a MetadataStore and loads all OData service metadata.
//
// Store selection priority (first match wins):
//  1. NATS_URL set       → NATS JetStream KV → fetch S3 config → s3Store
//  2. S3_ENDPOINT set    → s3Store directly from env vars (no NATS required)
//  3. ODATA_METADATA_PATH set → localStore (single .xml file or directory)
//
// OData credentials come from the cache's ODataConfig key (path 1) or from
// ODATA_USERNAME / ODATA_PASSWORD / ODATA_AUTH_TYPE env vars (all paths).
func (h *Handlers) Initialize(ctx context.Context) error {
	store, odataUser, odataPass, odataAuth, err := h.buildStore(ctx)
	if err != nil {
		return err
	}
	h.store = store

	xmlFiles, err := store.ListXMLFiles(ctx)
	if err != nil {
		return fmt.Errorf("listing XML files in metadata store: %w", err)
	}
	if len(xmlFiles) == 0 {
		logger.L.Warn("No XML metadata files found; server starts with no entities")
		return nil
	}
	logger.L.Info("Found metadata XML files", zap.Int("count", len(xmlFiles)))

	serviceGroups := make(map[string]*ServiceInfo)
	var allIDs []string

	for _, id := range xmlFiles {
		xmlContent, err := store.GetXMLContent(ctx, id)
		if err != nil {
			logger.L.Warn("Failed to get XML content", zap.String("id", id), zap.Error(err))
			continue
		}
		parsed, err := ParseMetadata(xmlContent)
		if err != nil {
			logger.L.Warn("Failed to parse EDMX", zap.String("id", id), zap.Error(err))
			continue
		}

		baseURL := parsed.BaseURL
		if _, exists := serviceGroups[baseURL]; !exists {
			serviceGroups[baseURL] = &ServiceInfo{
				BaseURL:  baseURL,
				Entities: make(map[string]EntityInfo),
			}
		}
		for setName, info := range parsed.Entities {
			serviceGroups[baseURL].Entities[setName] = info
		}
		serviceGroups[baseURL].Files = append(serviceGroups[baseURL].Files, id)
		allIDs = append(allIDs, id)

		logger.L.Info("Processed metadata file",
			zap.String("id", id),
			zap.String("base_url", baseURL),
			zap.Int("entities", len(parsed.Entities)),
		)
	}

	if len(serviceGroups) == 0 {
		return fmt.Errorf("no valid metadata could be parsed from any XML file")
	}

	if odataAuth == "" {
		odataAuth = "basic"
	}
	for baseURL, svc := range serviceGroups {
		svc.Client = newODataClient(baseURL, odataUser, odataPass, odataAuth)
		h.services[baseURL] = svc
		for setName := range svc.Entities {
			h.entityIndex[setName] = baseURL
		}
	}

	h.allMetadataIDs = allIDs
	if len(allIDs) > 0 {
		h.firstMetadataID = allIDs[0]
	}

	names := make([]string, 0, len(h.entityIndex))
	for name := range h.entityIndex {
		names = append(names, name)
	}
	h.allEntityNames = names

	total := len(names)
	logger.L.Info("Server state initialised",
		zap.Int("services", len(h.services)),
		zap.Int("total_entities", total),
		zap.Int("metadata_files", len(allIDs)),
	)
	return nil
}

// buildStore selects the MetadataStore implementation and resolves OData credentials.
func (h *Handlers) buildStore(ctx context.Context) (store MetadataStore, user, pass, authType string, err error) {
	cfg := h.cfg
	user, pass, authType = cfg.ODataUsername, cfg.ODataPassword, cfg.ODataAuthType

	switch {
	// ── Path 1: cache (NATS JetStream KV) ───────────────────────────────────
	case cfg.NATSUrl != "":
		logger.L.Info("Metadata store: resolving via cache (NATS)")

		cache, nerr := newNATSCache(cfg.NATSUrl, cfg.NATSCreds, cfg.NATSConnectTimeout, cfg.NATSKVBucket)
		if nerr != nil {
			err = fmt.Errorf("cache connect failed: %w", nerr)
			return
		}
		defer cache.Close()

		s3Raw, nerr := cache.GetConfig(ctx, cacheKeyS3Config)
		if nerr != nil {
			err = fmt.Errorf("fetching S3 config from cache: %w", nerr)
			return
		}
		if s3Raw == nil {
			err = fmt.Errorf("S3 config key not found in cache bucket %q", cfg.NATSKVBucket)
			return
		}

		if odataRaw, _ := cache.GetConfig(ctx, cacheKeyODataConfig); odataRaw != nil {
			if v := configString(odataRaw, "username"); v != "" {
				user = v
			}
			if v := configString(odataRaw, "password"); v != "" {
				pass = v
			}
			if v := configString(odataRaw, "auth_type"); v != "" {
				authType = v
			}
		}

		store, err = newS3Store(
			configString(s3Raw, "endpoint"),
			configString(s3Raw, "access_key"),
			configString(s3Raw, "secret_key"),
			configString(s3Raw, "bucket_name"),
			configBool(s3Raw, "secure"),
			metaDataPrefix,
		)
		if err != nil {
			err = fmt.Errorf("initialising S3 store from cache config: %w", err)
		}

	// ── Path 2: direct S3 env vars ───────────────────────────────────────────
	case cfg.S3Endpoint != "":
		logger.L.Info("Metadata store: S3 (direct env vars)",
			zap.String("endpoint", cfg.S3Endpoint))

		store, err = newS3Store(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Secure, metaDataPrefix)
		if err != nil {
			err = fmt.Errorf("initialising S3 store: %w", err)
		}

	// ── Path 3: local filesystem ─────────────────────────────────────────────
	case cfg.ODataMetadataPath != "":
		logger.L.Info("Metadata store: local filesystem",
			zap.String("path", cfg.ODataMetadataPath))

		store, err = newLocalStore(cfg.ODataMetadataPath)
		if err != nil {
			err = fmt.Errorf("initialising local store: %w", err)
		}

	default:
		err = fmt.Errorf(
			"no metadata store configured — set one of: " +
				"NATS_URL (cache-backed), " +
				"S3_ENDPOINT (direct S3-compatible storage), " +
				"ODATA_METADATA_PATH (local file or directory)",
		)
	}
	return
}

// AllEntityNames returns the cached list of all EntitySet names built during Initialize.
func (h *Handlers) AllEntityNames() []string {
	return h.allEntityNames
}

// ServiceForEntity returns the ServiceInfo for the given EntitySet name, or nil.
func (h *Handlers) ServiceForEntity(entityName string) *ServiceInfo {
	baseURL, ok := h.entityIndex[entityName]
	if !ok {
		return nil
	}
	return h.services[baseURL]
}
