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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"go.uber.org/zap"
)

var llmHTTPClient = &http.Client{Timeout: 20 * time.Second}

// entityFromLLM calls the Anthropic Messages API to select the best matching
// entity name from the provided list for the given user query.
//
// Mirrors odata_mcp_server.py get_entity_from_llm() — but uses Anthropic instead
// of OpenAI, consistent with tools.py which already uses Anthropic.
// Returns "" when no match can be determined.
func entityFromLLM(ctx context.Context, cfg *Config, query string, entityNames []string, services map[string]*ServiceInfo) string {
	if cfg.AnthropicAPIKey == "" {
		logger.L.Warn("ANTHROPIC_API_KEY not set; LLM entity selection skipped")
		return ""
	}
	if len(entityNames) == 0 {
		return ""
	}

	// Build entity descriptions for the prompt, matching Python's format.
	var descLines []string
	for _, name := range entityNames {
		label := name
		for _, svc := range services {
			if info, ok := svc.Entities[name]; ok {
				label = info.Label
				break
			}
		}
		descLines = append(descLines, fmt.Sprintf("- %s: %s", name, label))
	}

	prompt := "You are an API routing assistant. Your job is to select the correct data entity to " +
		"answer a user's question. Here are the available entities with their descriptions:\n" +
		strings.Join(descLines, "\n") + "\n" +
		"Based on the user's query, choose the most appropriate entity from the provided list. " +
		"IMPORTANT RULES:\n" +
		"1. If the user's query contains the exact name of an entity (case-insensitive), select that entity\n" +
		"2. If there are multiple possible matches, select the entity whose name is the longest match in the query\n" +
		"3. Do NOT interpret the query as SQL - this is natural language entity selection\n" +
		"4. Match the entity name as closely as possible to the user's query\n" +
		"5. Consider plural forms and common variations of entity names\n" +
		"\nUser query: " + query + "\n" +
		"Respond with ONLY the entity name from the list above."

	payload := map[string]any{
		"model":      cfg.AnthropicModel,
		"max_tokens": 64,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, _ := json.Marshal(payload)

	base := strings.TrimRight(cfg.AnthropicBaseURL, "/")
	path := cfg.AnthropicAPIPath
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	apiURL := base + path

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		logger.L.Warn("LLM request build failed", zap.Error(err))
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.AnthropicAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		logger.L.Warn("LLM request failed", zap.Error(err))
		return ""
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logger.L.Warn("Anthropic API error",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(respBody)),
		)
		return ""
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || len(result.Content) == 0 {
		logger.L.Warn("LLM response parse failed", zap.Error(err))
		return ""
	}

	llmText := strings.TrimSpace(result.Content[0].Text)
	logger.L.Info("LLM entity selection", zap.String("response", llmText))

	// Exact match first
	for _, name := range entityNames {
		if strings.EqualFold(name, llmText) {
			return name
		}
	}
	// Partial match
	llmLower := strings.ToLower(llmText)
	for _, name := range entityNames {
		lower := strings.ToLower(name)
		if strings.Contains(lower, llmLower) || strings.Contains(llmLower, lower) {
			return name
		}
	}

	logger.L.Warn("LLM response did not match any entity", zap.String("response", llmText))
	return ""
}
