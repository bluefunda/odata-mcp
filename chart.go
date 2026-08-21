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

	"github.com/bluefunda/odata-mcp/internal/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// maxChartSampleRows caps how many records are sent to the LLM as a sample
// when suggesting a chart configuration.
const maxChartSampleRows = 5

// registerChartTool registers the generate-chart MCP tool on the server.
func registerChartTool(server *mcp.Server, h *Handlers) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "generate-chart",
		Description: "Suggest a chart configuration for a set of data records (e.g. the records returned " +
			"by query-odata-service or query-service). Returns a structured chart spec — chart_type, " +
			"x_field, y_field, group_by, and a brief insight — for the client to render. Does not render " +
			"an image itself.",
	}, h.HandleGenerateChart)
}

// GenerateChartInput is the input schema for generate-chart.
type GenerateChartInput struct {
	Query   string           `json:"query" jsonschema:"The natural-language question that produced the data"`
	Records []map[string]any `json:"records" jsonschema:"Data records to visualize"`
}

// GenerateChartOutput is the output schema for generate-chart.
type GenerateChartOutput struct {
	ChartType string `json:"chart_type,omitempty" jsonschema:"Suggested chart type, e.g. bar, line, pie"`
	XField    string `json:"x_field,omitempty"    jsonschema:"Field to use for the x-axis / categories"`
	YField    string `json:"y_field,omitempty"    jsonschema:"Field to use for the y-axis / values"`
	GroupBy   string `json:"group_by,omitempty"   jsonschema:"Field to group/series by, if any"`
	Insight   string `json:"insight,omitempty"    jsonschema:"One-sentence takeaway about the data"`
	Error     string `json:"error,omitempty"      jsonschema:"Error message if a chart config could not be generated"`
}

// HandleGenerateChart implements the generate-chart MCP tool.
func (h *Handlers) HandleGenerateChart(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input GenerateChartInput,
) (*mcp.CallToolResult, GenerateChartOutput, error) {
	if len(input.Records) == 0 {
		return nil, GenerateChartOutput{Error: "No records provided to chart."}, nil
	}

	out, err := chartConfigFromLLM(ctx, h.cfg, input.Query, input.Records)
	if err != nil {
		return nil, GenerateChartOutput{Error: err.Error()}, nil
	}
	return nil, *out, nil
}

// chartConfigFromLLM calls the Anthropic Messages API to suggest a chart
// configuration for the given records, reusing the same HTTP client and
// config fields as llm.go's entityFromLLM.
func chartConfigFromLLM(ctx context.Context, cfg *Config, query string, records []map[string]any) (*GenerateChartOutput, error) {
	if cfg.AnthropicAPIKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set; chart generation is unavailable")
	}

	fields := make(map[string]struct{})
	for _, rec := range records {
		for k := range rec {
			fields[k] = struct{}{}
		}
	}
	fieldNames := make([]string, 0, len(fields))
	for k := range fields {
		fieldNames = append(fieldNames, k)
	}

	sample := records
	if len(sample) > maxChartSampleRows {
		sample = sample[:maxChartSampleRows]
	}
	sampleJSON, err := json.Marshal(sample)
	if err != nil {
		return nil, fmt.Errorf("marshalling sample records: %w", err)
	}

	prompt := "You are a data visualization assistant. Given a user's question and a sample of the data " +
		"returned for it, suggest the best chart configuration.\n\n" +
		"User question: " + query + "\n" +
		"Available fields: " + strings.Join(fieldNames, ", ") + "\n" +
		"Sample records (JSON): " + string(sampleJSON) + "\n\n" +
		"Respond with ONLY a JSON object with this exact shape, no other text:\n" +
		`{"chart_type": "bar|line|pie|scatter", "x_field": "...", "y_field": "...", "group_by": "", "insight": "..."}` + "\n" +
		"Use one of the available fields for x_field and y_field. group_by may be empty. " +
		"insight is one short sentence describing the key takeaway from the data."

	payload := map[string]any{
		"model":      cfg.AnthropicModel,
		"max_tokens": 256,
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
		return nil, fmt.Errorf("building chart LLM request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.AnthropicAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Anthropic API: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logger.L.Warn("Anthropic API error (chart)",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(respBody)),
		)
		return nil, fmt.Errorf("anthropic API returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || len(result.Content) == 0 {
		return nil, fmt.Errorf("parsing Anthropic response: %w", err)
	}

	spec, err := extractChartJSON(result.Content[0].Text)
	if err != nil {
		return nil, fmt.Errorf("parsing chart config from LLM response: %w", err)
	}
	return spec, nil
}

// extractChartJSON extracts and parses the first {...} JSON object found in
// text, tolerating any stray text the LLM might add around it.
func extractChartJSON(text string) (*GenerateChartOutput, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON object found in response: %q", text)
	}

	var out GenerateChartOutput
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("invalid JSON in response: %w", err)
	}
	if out.ChartType == "" || out.YField == "" {
		return nil, fmt.Errorf("incomplete chart config in response: %q", text)
	}
	return &out, nil
}
