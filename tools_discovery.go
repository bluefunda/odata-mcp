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
	"errors"
	"fmt"
	"time"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// registerDiscoveryTools registers the list-metadata-files, list-available-services,
// and query-service MCP tools on the server.
func registerDiscoveryTools(server *mcp.Server, h *Handlers) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list-metadata-files",
		Description: "List the metadata source files/identifiers discovered by the server at startup.",
	}, h.HandleListMetadataFiles)

	mcp.AddTool(server, &mcp.Tool{
		Name: "list-available-services",
		Description: "List every OData service discovered at startup, with its base URL and the " +
			"EntitySets it exposes. Use this before query-service to find a base_url to scope a query to.",
	}, h.HandleListAvailableServices)

	mcp.AddTool(server, &mcp.Tool{
		Name: "query-service",
		Description: "Query OData data from one specific, explicitly chosen service. Use this instead of " +
			"query-odata-service when an EntitySet name is ambiguous across multiple services, or when you " +
			"already know which service (base_url, from list-available-services) you want. " +
			"If it returns an error, tell the user that error; never substitute data from another source.",
	}, h.HandleQueryService)
}

// ListMetadataFilesOutput is the output schema for list-metadata-files.
type ListMetadataFilesOutput struct {
	Files []string `json:"files" jsonschema:"Metadata source identifiers processed at startup"`
}

// HandleListMetadataFiles implements the list-metadata-files MCP tool.
func (h *Handlers) HandleListMetadataFiles(
	_ context.Context,
	_ *mcp.CallToolRequest,
	_ struct{},
) (*mcp.CallToolResult, ListMetadataFilesOutput, error) {
	if h.sessionErr != nil {
		return nil, ListMetadataFilesOutput{}, errors.New(toolError(h.sessionErr.Error()))
	}
	return nil, ListMetadataFilesOutput{Files: h.MetadataFileIDs()}, nil
}

// ListAvailableServicesOutput is the output schema for list-available-services.
type ListAvailableServicesOutput struct {
	Services []ServiceSummary `json:"services" jsonschema:"Discovered OData services and their entities"`
}

// HandleListAvailableServices implements the list-available-services MCP tool.
func (h *Handlers) HandleListAvailableServices(
	_ context.Context,
	_ *mcp.CallToolRequest,
	_ struct{},
) (*mcp.CallToolResult, ListAvailableServicesOutput, error) {
	if h.sessionErr != nil {
		return nil, ListAvailableServicesOutput{}, errors.New(toolError(h.sessionErr.Error()))
	}
	return nil, ListAvailableServicesOutput{Services: h.ServiceSummaries()}, nil
}

// QueryServiceInput is the input schema for query-service.
type QueryServiceInput struct {
	BaseURL string `json:"base_url" jsonschema:"Base URL of the service to query, from list-available-services"`
	Query   string `json:"query" jsonschema:"Natural language question, e.g. 'get all countries'"`
	Entity  string `json:"entity,omitempty" jsonschema:"Optional EntitySet to query, exactly as list-available-services lists it (e.g. A_Customer). Set it whenever you know which entity fits."`
}

// HandleQueryService implements the query-service MCP tool: like
// query-odata-service, but scoped to one explicitly chosen service so that
// EntitySet names which are ambiguous across services can be resolved.
func (h *Handlers) HandleQueryService(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input QueryServiceInput,
) (*mcp.CallToolResult, QueryODataOutput, error) {
	requestID := uuid.New().String()[:8]
	start := time.Now()
	log := logger.WithTool(requestID, "query-service")

	log.Info("Tool execution started", zap.String("base_url", input.BaseURL), zap.String("query", input.Query))

	if h.sessionErr != nil {
		return nil, QueryODataOutput{Error: toolError(h.sessionErr.Error())}, nil
	}

	svc, ok := h.services[input.BaseURL]
	if !ok {
		msg := fmt.Sprintf("Unknown service base_url %q. Call list-available-services to see valid values.", input.BaseURL)
		log.Warn(msg)
		return nil, QueryODataOutput{Error: toolError(msg)}, nil
	}

	entityNames := make([]string, 0, len(svc.Entities))
	for name := range svc.Entities {
		entityNames = append(entityNames, name)
	}

	targetEntity, selectErr := selectEntity(ctx, h.cfg, input.Query, input.Entity, entityNames,
		map[string]*ServiceInfo{input.BaseURL: svc})
	if targetEntity == "" {
		log.Warn("Entity selection failed", zap.String("entity", input.Entity))
		return nil, QueryODataOutput{Error: toolError(fmt.Sprintf("In service %s: %s", input.BaseURL, selectErr))}, nil
	}

	log.Info("Entity selected", zap.String("entity", targetEntity))

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	records, errMsg := svc.Client.FetchEntitySet(fetchCtx, targetEntity, nil)
	if errMsg != "" {
		log.Warn("OData fetch error", zap.String("error", errMsg))
		return nil, QueryODataOutput{Error: toolError(errMsg)}, nil
	}
	if records == nil {
		records = []any{}
	}

	log.Info("Tool execution completed",
		zap.String("entity", targetEntity),
		zap.Int("records", len(records)),
		zap.Duration("duration", time.Since(start)),
	)

	return nil, QueryODataOutput{Records: records}, nil
}
