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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluefunda/odata-mcp/internal/logger"
)

func TestMain(m *testing.M) {
	_ = logger.Init(logger.Config{Level: "error", Format: "console", ServerName: "odata-mcp-test"})
	os.Exit(m.Run())
}

func edmxWithCountries(baseURL string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx Version="1.0" xmlns:edmx="http://schemas.microsoft.com/ado/2007/06/edmx"
           xmlns:sap="http://www.sap.com/Protocols/SAPData">
  <edmx:DataServices>
    <Schema Namespace="SVC" xml:lang="en"
            xmlns="http://schemas.microsoft.com/ado/2008/09/edm"
            xmlns:atom="http://www.w3.org/2005/Atom">
      <atom:link rel="self" href="` + baseURL + `/$metadata"/>
      <EntityType Name="Country" sap:label="Country">
        <Key><PropertyRef Name="CountryCode"/></Key>
        <Property Name="CountryCode" Type="Edm.String" MaxLength="2"/>
      </EntityType>
      <EntityContainer Name="SVC">
        <EntitySet Name="Countries" EntityType="SVC.Country"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`
}

// newInitializedHandlersWithCollision builds Handlers from two synthetic
// services that both define a "Countries" EntitySet, via a local directory
// metadata store.
func newInitializedHandlersWithCollision(t *testing.T) *Handlers {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "svc-a.xml"), []byte(edmxWithCountries("https://a.example.com/odata/SVC")), 0o600); err != nil {
		t.Fatalf("writing svc-a.xml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "svc-b.xml"), []byte(edmxWithCountries("https://b.example.com/odata/SVC")), 0o600); err != nil {
		t.Fatalf("writing svc-b.xml: %v", err)
	}

	cfg := &Config{ODataMetadataPath: dir, ODataAuthType: "basic"}
	h := NewHandlers(cfg)
	if err := h.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize error: %v", err)
	}
	return h
}

func TestServicesForEntity_DetectsCollision(t *testing.T) {
	h := newInitializedHandlersWithCollision(t)

	urls := h.ServicesForEntity("Countries")
	if len(urls) != 2 {
		t.Fatalf("ServicesForEntity(Countries) = %v, want 2 base URLs", urls)
	}
}

func TestServicesForEntity_NoCollisionForUniqueEntity(t *testing.T) {
	h := newInitializedHandlersWithCollision(t)

	// Only "Countries" is defined by both synthetic services; an unknown
	// name should report zero, not a false collision.
	if urls := h.ServicesForEntity("DoesNotExist"); len(urls) != 0 {
		t.Errorf("ServicesForEntity(DoesNotExist) = %v, want empty", urls)
	}
}

func TestHandleQueryODataService_AmbiguousEntityReturnsError(t *testing.T) {
	h := newInitializedHandlersWithCollision(t)

	_, out, err := h.HandleQueryODataService(context.Background(), nil, QueryODataInput{Query: "list all countries"})
	if err != nil {
		t.Fatalf("HandleQueryODataService returned error: %v", err)
	}
	if out.Error == "" {
		t.Fatal("expected an ambiguity error, got none")
	}
	if !strings.Contains(out.Error, "query-service") {
		t.Errorf("error message %q does not point the caller at query-service", out.Error)
	}
}

func TestHandleQueryService_UnknownBaseURL(t *testing.T) {
	h := newInitializedHandlersWithCollision(t)

	_, out, err := h.HandleQueryService(context.Background(), nil, QueryServiceInput{
		BaseURL: "https://unknown.example.com/odata/SVC",
		Query:   "list all countries",
	})
	if err != nil {
		t.Fatalf("HandleQueryService returned error: %v", err)
	}
	if out.Error == "" {
		t.Fatal("expected an unknown-service error, got none")
	}
}

func TestServiceSummaries_ListsAllServices(t *testing.T) {
	h := newInitializedHandlersWithCollision(t)

	summaries := h.ServiceSummaries()
	if len(summaries) != 2 {
		t.Fatalf("ServiceSummaries() = %d entries, want 2", len(summaries))
	}
	for _, s := range summaries {
		if s.EntityCount != 1 || len(s.Entities) != 1 || s.Entities[0] != "Countries" {
			t.Errorf("unexpected summary: %+v", s)
		}
	}
}

func TestHandleListMetadataFiles(t *testing.T) {
	h := newInitializedHandlersWithCollision(t)

	_, out, err := h.HandleListMetadataFiles(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("HandleListMetadataFiles returned error: %v", err)
	}
	if len(out.Files) != 2 {
		t.Fatalf("ListMetadataFiles = %v, want 2 files", out.Files)
	}
}
