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
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerResources registers both MCP resources on the server.
func registerResources(server *mcp.Server, h *Handlers) {
	server.AddResource(&mcp.Resource{
		URI:         "mcp://odata_server_llm/metadata.xml",
		Name:        "metadata.xml",
		Description: "Raw XML content of the first OData EDMX metadata file used by the server.",
		MIMEType:    "application/xml",
	}, h.HandleMetadataXML)

	server.AddResource(&mcp.Resource{
		URI:         "mcp://odata_server_llm/metadata-info.json",
		Name:        "metadata-info.json",
		Description: "Summary of all OData services and their entities discovered at startup.",
		MIMEType:    "application/json",
	}, h.HandleMetadataInfo)
}

// HandleMetadataXML serves the raw EDMX XML of the first discovered metadata file.
// Mirrors odata_mcp_server.py odata_metadata_xml resource.
func (h *Handlers) HandleMetadataXML(
	ctx context.Context,
	req *mcp.ReadResourceRequest,
) (*mcp.ReadResourceResult, error) {
	if h.firstMetadataID == "" || h.store == nil {
		return xmlResult(req.Params.URI, "<error>Metadata file not found or server not initialized correctly.</error>"), nil
	}

	xmlContent, err := h.store.GetXMLContent(ctx, h.firstMetadataID)
	if err != nil {
		return xmlResult(req.Params.URI, fmt.Sprintf("<error>Could not read metadata file: %v</error>", err)), nil
	}

	return xmlResult(req.Params.URI, xmlContent), nil
}

// HandleMetadataInfo serves a JSON summary of all services and entities.
// Mirrors odata_mcp_server.py odata_metadata_info resource.
func (h *Handlers) HandleMetadataInfo(
	ctx context.Context,
	req *mcp.ReadResourceRequest,
) (*mcp.ReadResourceResult, error) {
	if len(h.allMetadataIDs) == 0 || len(h.services) == 0 {
		return jsonResult(req.Params.URI, map[string]string{
			"error": "No metadata files processed or server not initialized correctly.",
		}), nil
	}

	type serviceSummary struct {
		BaseURL     string   `json:"base_url"`
		Entities    []string `json:"entities"`
		EntityCount int      `json:"entity_count"`
	}

	var services []serviceSummary
	var allEntities []string
	for baseURL, svc := range h.services {
		names := make([]string, 0, len(svc.Entities))
		for name := range svc.Entities {
			names = append(names, name)
			allEntities = append(allEntities, name)
		}
		services = append(services, serviceSummary{
			BaseURL:     baseURL,
			Entities:    names,
			EntityCount: len(names),
		})
	}

	info := map[string]any{
		"total_files_processed":    len(h.allMetadataIDs),
		"files_used":               h.allMetadataIDs,
		"services":                 services,
		"total_entities_available": len(allEntities),
		"all_entities":             allEntities,
		"timestamp":                time.Now().UTC().Format(time.RFC3339),
	}

	return jsonResult(req.Params.URI, info), nil
}

func xmlResult(uri, content string) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{URI: uri, MIMEType: "application/xml", Text: content},
		},
	}
}

func jsonResult(uri string, v any) *mcp.ReadResourceResult {
	b, _ := json.MarshalIndent(v, "", "  ")
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{URI: uri, MIMEType: "application/json", Text: string(b)},
		},
	}
}
