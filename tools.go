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
	"strings"
	"time"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// registerTools registers all MCP tools on the server.
// Called after Initialize() so h.entityIndex is populated and the description
// can include the full entity list — mirroring the Python server's post-init
// docstring patch on query_odata_service.
func registerTools(server *mcp.Server, h *Handlers) {
	desc := "Query OData services and return actual data. " +
		"Always use this tool to get real data — never generate or fabricate responses.\n\n" +
		"The tool automatically selects the correct service and entity from your natural language question."

	if names := h.AllEntityNames(); len(names) > 0 {
		desc += "\n\nAvailable entities: " + strings.Join(names, ", ")
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "query-odata-service",
		Description: desc,
	}, h.HandleQueryODataService)
}

// QueryODataInput is the input schema for query-odata-service.
type QueryODataInput struct {
	Query string `json:"query" jsonschema:"Natural language question (e.g. 'get all countries', 'show invoices', 'currency of Germany')"`
}

// QueryODataOutput is the output schema for query-odata-service.
type QueryODataOutput struct {
	Records []any  `json:"records,omitempty" jsonschema:"Data records returned from OData service"`
	Error   string `json:"error,omitempty"  jsonschema:"Error message if request failed"`
}

// HandleQueryODataService implements the query-odata-service MCP tool.
// Mirrors odata_mcp_server.py query_odata_service().
func (h *Handlers) HandleQueryODataService(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input QueryODataInput,
) (*mcp.CallToolResult, QueryODataOutput, error) {
	requestID := uuid.New().String()[:8]
	start := time.Now()
	log := logger.WithTool(requestID, "query-odata-service")

	log.Info("Tool execution started", zap.String("query", input.Query))

	if len(h.services) == 0 || len(h.entityIndex) == 0 {
		msg := "The OData client is not initialized. Please check server logs."
		log.Warn(msg)
		return nil, QueryODataOutput{Error: msg}, nil
	}

	entityNames := h.AllEntityNames()
	log.Info("Available entities", zap.Strings("entities", entityNames))

	// --- Entity selection ---------------------------------------------------
	// Step 1: direct substring / plural matching (mirrors Python logic exactly)
	targetEntity := matchEntity(input.Query, entityNames)

	// Step 2: LLM fallback when heuristic fails
	if targetEntity == "" {
		log.Info("Heuristic match failed, falling back to LLM")
		targetEntity = entityFromLLM(ctx, h.cfg, input.Query, entityNames, h.services)
	}

	if targetEntity == "" {
		msg := fmt.Sprintf(
			"I could not determine which data entity to use for your query: %q. Please try rephrasing.",
			input.Query,
		)
		log.Warn("Entity selection failed")
		return nil, QueryODataOutput{
			Error: msg,
		}, nil
	}

	log.Info("Entity selected", zap.String("entity", targetEntity))

	// --- Fetch data ---------------------------------------------------------
	svc := h.ServiceForEntity(targetEntity)
	if svc == nil {
		return nil, QueryODataOutput{
			Error: fmt.Sprintf("Could not find service for entity %q.", targetEntity),
		}, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	records, errMsg := svc.Client.FetchEntitySet(fetchCtx, targetEntity, nil)
	if errMsg != "" {
		log.Warn("OData fetch error", zap.String("error", errMsg))
		return nil, QueryODataOutput{Error: errMsg}, nil
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

// matchEntity applies the heuristic entity-selection logic from odata_mcp_server.py:
//  1. Exact/plural substring match, preferring longer entity names.
//  2. Partial match ignoring spaces.
//
// Returns "" when no match is found.
func matchEntity(query string, entityNames []string) string {
	queryLower := strings.ToLower(query)

	// Pass 1: exact and plural forms
	best := ""
	bestLen := 0
	for _, name := range entityNames {
		nl := strings.ToLower(name)
		if containsAny(queryLower, nl,
			nl+"s",
			strings.Replace(nl, "ies", "y", 1),
			strings.Replace(nl, "y", "ies", 1),
		) {
			if len(name) > bestLen {
				best = name
				bestLen = len(name)
			}
		}
	}
	if best != "" {
		return best
	}

	// Pass 2: partial match ignoring spaces
	queryClean := strings.ReplaceAll(queryLower, " ", "")
	for _, name := range entityNames {
		nl := strings.ReplaceAll(strings.ToLower(name), " ", "")
		if strings.Contains(queryClean, nl) && len(name) > bestLen {
			best = name
			bestLen = len(name)
		}
	}
	return best
}

func containsAny(s string, candidates ...string) bool {
	for _, c := range candidates {
		if c != "" && strings.Contains(s, c) {
			return true
		}
	}
	return false
}
