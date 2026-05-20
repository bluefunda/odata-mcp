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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"go.uber.org/zap"
)

// ODataClient performs HTTP queries against a single OData service base URL.
// Mirrors odata_client.py ODataClient.
type ODataClient struct {
	baseURL  string
	headers  http.Header
	username string
	password string
	authType string
	http     *http.Client
}

// newODataClient builds a client.  authType is "basic", "bearer", or "" for no auth.
func newODataClient(baseURL, username, password, authType string) *ODataClient {
	c := &ODataClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		authType: authType,
		// Do not follow redirects automatically so we can detect and report them.
		http: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Timeout: 30 * time.Second,
		},
	}

	c.headers = http.Header{}
	c.headers.Set("Accept", "application/json")
	c.headers.Set("User-Agent", "OData-MCP-Client/1.0")

	if username != "" && password != "" {
		switch authType {
		case "bearer":
			c.headers.Set("Authorization", "Bearer "+password)
		case "basic":
			creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
			c.headers.Set("Authorization", "Basic "+creds)
		default:
			c.headers.Set("Authorization", password)
		}
	}

	return c
}

// buildFilterString converts a map of field→value pairs into an OData $filter expression.
// Mirrors odata_client.py _build_filter_string().
func buildFilterString(filters map[string]any) string {
	if len(filters) == 0 {
		return ""
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch val := filters[k].(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s eq '%s'", k, val))
		case bool:
			if val {
				parts = append(parts, fmt.Sprintf("%s eq true", k))
			} else {
				parts = append(parts, fmt.Sprintf("%s eq false", k))
			}
		default:
			parts = append(parts, fmt.Sprintf("%s eq %v", k, val))
		}
	}
	return strings.Join(parts, " and ")
}

// FetchEntitySet queries an EntitySet and returns the raw JSON records slice.
// Returns an error string (not a Go error) for service-level problems so the
// MCP tool can surface them as structured JSON responses — mirroring the
// Python implementation's string-return approach.
func (c *ODataClient) FetchEntitySet(ctx context.Context, entitySetName string, filters map[string]any) ([]any, string) {
	params := url.Values{}
	params.Set("$format", "json")
	if f := buildFilterString(filters); f != "" {
		params.Set("$filter", f)
	}

	target := fmt.Sprintf("%s/%s?%s", c.baseURL, entitySetName, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Sprintf("Error: building request for %s: %v", target, err)
	}
	for k, vs := range c.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	logger.L.Info("OData query", zap.String("url", target))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Sprintf("Error: Cannot connect to OData service at %s: %v", target, err)
	}
	defer resp.Body.Close()

	// Surface redirects, mirroring odata_client.py redirect detection.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		return nil, fmt.Sprintf("Redirect detected: %d -> %s", resp.StatusCode, loc)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Sprintf("Error: reading response body: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Sprintf("Error fetching data: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Sprintf("Error: response is not valid JSON: %v", err)
	}

	records := extractRecords(raw)
	return records, ""
}

// extractRecords normalises OData v2 and v4 response envelopes into a flat slice.
// Mirrors tools.py extract_records_from_response().
//
//   - OData v4: {"value": [...]}
//   - OData v2: {"d": {"results": [...]}}
//   - Already a list: returned as-is
//   - Fallback: first list value in a dict
func extractRecords(data any) []any {
	switch d := data.(type) {
	case []any:
		return d
	case map[string]any:
		// v4
		if v, ok := d["value"]; ok {
			if arr, ok := v.([]any); ok {
				return arr
			}
		}
		// v2
		if inner, ok := d["d"].(map[string]any); ok {
			if results, ok := inner["results"].([]any); ok {
				return results
			}
		}
		// fallback: first list in map
		for _, v := range d {
			if arr, ok := v.([]any); ok {
				return arr
			}
		}
	}
	return nil
}
