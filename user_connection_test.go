// Copyright 2025 bluefunda
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServiceURL(t *testing.T) {
	host := "https://sap.example.com:44300"
	ok := map[string]string{
		"API_BUSINESS_PARTNER":                          host + "/sap/opu/odata/sap/API_BUSINESS_PARTNER",
		" API_X/$metadata ":                             host + "/sap/opu/odata/sap/API_X",
		"/sap/opu/odata4/sap/api_y/srvd_a2x/sap/y/0001": host + "/sap/opu/odata4/sap/api_y/srvd_a2x/sap/y/0001",
		host + "/sap/opu/odata/sap/Z_SRV/?x=1":          host + "/sap/opu/odata/sap/Z_SRV",
	}
	for in, want := range ok {
		if got, err := serviceURL(host, in); err != nil || got != want {
			t.Errorf("serviceURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "a/b", "https://evil.example.com/sap/opu/odata/sap/X", "http://sap.example.com:44300/x"} {
		if got, err := serviceURL(host, bad); err == nil {
			t.Errorf("serviceURL(%q) = %q, want an error", bad, got)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "172.18.0.5", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fd00::1", "0.0.0.0"} {
		if isPublicIP(net.ParseIP(ip)) {
			t.Errorf("%s should not be public", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "20.50.1.1", "2606:4700::1111"} {
		if !isPublicIP(net.ParseIP(ip)) {
			t.Errorf("%s should be public", ip)
		}
	}
}

// fakeSAP serves $metadata, Countries and the gateway catalog, recording what
// it was sent.
type fakeSAP struct {
	*httptest.Server
	metadataCalls atomic.Int32
	badAuth       atomic.Int32
	noClient      atomic.Int32
}

func newFakeSAP(t *testing.T) *fakeSAP {
	f := &fakeSAP{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "alice" || p != "hunter2" {
			f.badAuth.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("sap-client") != "100" {
			f.noClient.Add(1)
		}
		switch r.URL.Path {
		case "/sap/opu/odata/sap/SVC/$metadata":
			f.metadataCalls.Add(1)
			// A self link to another host must not be where queries go.
			_, _ = w.Write([]byte(edmxWithCountries("https://evil.example.com/odata/SVC")))
		case "/sap/opu/odata/sap/SVC/Countries":
			_, _ = w.Write([]byte(`{"d":{"results":[{"CountryCode":"DE"},{"CountryCode":"IN"}]}}`))
		case "/sap/opu/odata/IWFND/CATALOGSERVICE;v=2/ServiceCollection":
			_, _ = w.Write([]byte(`{"d":{"results":[{"ServiceUrl":"http://sapgw.internal:8000/sap/opu/odata/sap/SVC/"}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// fakeBFF answers /internal/odata/credentials from conns.
func fakeBFF(t *testing.T, conns map[string]UserConnection) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/odata/credentials" || r.Header.Get("X-Internal-Secret") != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, ok := conns[r.URL.Query().Get("userID")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(conn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sessionsFor(bffURL string, allowPrivate bool) *sessionServers {
	cfg := &Config{CAIBFFInternalURL: bffURL, InternalServiceSecret: "s3cret", AllowPrivateHosts: allowPrivate}
	return &sessionServers{cfg: cfg, shared: NewHandlers(cfg), cache: newUserCache()}
}

// callTool opens a session as userID and calls one tool, returning the
// result's text.
func callTool(t *testing.T, s *sessionServers, userID, tool string, args map[string]any) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	if userID != "" {
		req.Header.Set(headerPrincipalUserID, userID)
	}
	server := s.serverFor(req)

	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func TestUserSessionQueriesTheirOwnSystem(t *testing.T) {
	sap := newFakeSAP(t)
	bff := fakeBFF(t, map[string]UserConnection{
		"user-1": {Host: sap.URL, Client: "100", Username: "alice", Password: "hunter2", Services: []string{"SVC"}},
	})
	s := sessionsFor(bff.URL, true)

	out := callTool(t, s, "user-1", "query-odata-service", map[string]any{"query": "get all countries"})
	if !strings.Contains(out, `"DE"`) || !strings.Contains(out, `"IN"`) {
		t.Fatalf("query result = %s", out)
	}
	if sap.badAuth.Load() != 0 || sap.noClient.Load() != 0 {
		t.Fatalf("requests without the user's login (%d) or sap-client (%d)", sap.badAuth.Load(), sap.noClient.Load())
	}

	// The next session (next chat turn) reuses the loaded metadata.
	callTool(t, s, "user-1", "list-available-services", nil)
	if n := sap.metadataCalls.Load(); n != 1 {
		t.Fatalf("$metadata fetched %d times, want 1", n)
	}
	services := callTool(t, s, "user-1", "list-available-services", nil)
	if !strings.Contains(services, sap.URL+"/sap/opu/odata/sap/SVC") || strings.Contains(services, "evil.example.com") {
		t.Fatalf("services = %s", services)
	}
}

func TestUserSessionDiscoversServicesFromCatalog(t *testing.T) {
	sap := newFakeSAP(t)
	bff := fakeBFF(t, map[string]UserConnection{
		"user-1": {Host: sap.URL, Client: "100", Username: "alice", Password: "hunter2"},
	})

	out := callTool(t, sessionsFor(bff.URL, true), "user-1", "query-odata-service", map[string]any{"query": "countries"})
	if !strings.Contains(out, `"DE"`) {
		t.Fatalf("query result = %s", out)
	}
}

func TestUserSessionErrors(t *testing.T) {
	sap := newFakeSAP(t)
	bff := fakeBFF(t, map[string]UserConnection{
		"wrong-password": {Host: sap.URL, Client: "100", Username: "alice", Password: "nope", Services: []string{"SVC"}},
		"private-host":   {Host: sap.URL, Client: "100", Username: "alice", Password: "hunter2", Services: []string{"SVC"}},
	})

	cases := []struct {
		user         string
		allowPrivate bool
		want         string
	}{
		{"not-connected", true, "connect SAP Analytics in Settings"},
		{"wrong-password", true, "rejected the username or password"},
		// The fake SAP is on 127.0.0.1: refused unless private hosts are allowed.
		{"private-host", false, "not allowed"},
	}
	for _, tc := range cases {
		out := callTool(t, sessionsFor(bff.URL, tc.allowPrivate), tc.user, "query-odata-service", map[string]any{"query": "countries"})
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s: result = %s, want it to mention %q", tc.user, out, tc.want)
		}
	}
	if sap.metadataCalls.Load() != 0 {
		t.Fatal("metadata served to a failing session")
	}
}

// A failing session's errors tell the model to relay them, not to answer
// from somewhere else (a web search for sample data was seen on test).
func TestUserSessionErrorsForbidFallback(t *testing.T) {
	s := sessionsFor(fakeBFF(t, nil).URL, true)
	for tool, args := range map[string]map[string]any{
		"query-odata-service":     {"query": "customers"},
		"list-available-services": nil,
	} {
		out := callTool(t, s, "not-connected", tool, args)
		if !strings.Contains(out, "Don't answer with data from any other source") {
			t.Errorf("%s: result = %s, want the no-fallback instruction", tool, out)
		}
	}
}
