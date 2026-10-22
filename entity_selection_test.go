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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var sapEntities = []string{"A_Customer", "A_CustomerCompany", "A_BusinessPartner", "A_Supplier", "A_SalesOrder", "A_Tax"}

func TestMatchEntityByKeySAPNames(t *testing.T) {
	cases := map[string]string{
		"show me 5 customers":               "A_Customer",
		"list business partners in Germany": "A_BusinessPartner",
		"open sales orders":                 "A_SalesOrder",
		"customer company codes for 1010":   "A_CustomerCompany",
		"which suppliers are blocked":       "A_Supplier",
		"what's the weather":                "",
		"tax":                               "", // too short to match inside a question
	}
	for query, want := range cases {
		if got := matchEntityByKey(query, sapEntities); got != want {
			t.Errorf("matchEntityByKey(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestResolveEntity(t *testing.T) {
	cases := map[string]string{
		"A_Customer":  "A_Customer",
		"a_customer":  "A_Customer",
		"Customer":    "A_Customer",
		" A_Supplier": "A_Supplier",
		"Invoice":     "",
		"":            "",
	}
	for in, want := range cases {
		if got := resolveEntity(in, sapEntities); got != want {
			t.Errorf("resolveEntity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelectEntity(t *testing.T) {
	cfg := &Config{}
	ctx := context.Background()

	if got, _ := selectEntity(ctx, cfg, "show me 5 customers", "", sapEntities, nil); got != "A_Customer" {
		t.Errorf("by question = %q, want A_Customer", got)
	}
	if got, _ := selectEntity(ctx, cfg, "anything", "A_Supplier", sapEntities, nil); got != "A_Supplier" {
		t.Errorf("by entity = %q, want A_Supplier", got)
	}
	// Northwind-style names still match as before.
	if got, _ := selectEntity(ctx, cfg, "show me 5 customers", "", []string{"Customers", "Orders"}, nil); got != "Customers" {
		t.Errorf("Northwind = %q, want Customers", got)
	}

	// No match: the error lists the entities so the caller can retry with entity set.
	got, msg := selectEntity(ctx, cfg, "what's the weather", "", sapEntities, nil)
	if got != "" || !strings.Contains(msg, "A_Customer") || !strings.Contains(msg, "entity set") {
		t.Errorf("no match = %q, %q", got, msg)
	}
	got, msg = selectEntity(ctx, cfg, "x", "Invoice", sapEntities, nil)
	if got != "" || !strings.Contains(msg, `no entity "Invoice"`) || !strings.Contains(msg, "A_SalesOrder") {
		t.Errorf("unknown entity = %q, %q", got, msg)
	}
}

func TestEntitiesHintCaps(t *testing.T) {
	var names []string
	for i := 0; i < maxEntitiesInError+5; i++ {
		names = append(names, "E"+strings.Repeat("x", i%7)+string(rune('a'+i%26)))
	}
	if !strings.Contains(entitiesHint(names), "(and 5 more)") {
		t.Error("hint doesn't cap the list")
	}
}

func TestCatalogDiscoveryPrefersReleasedAPIs(t *testing.T) {
	sys := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"d":{"results":[` +
			`{"ServiceUrl":"https://gw.internal/sap/opu/odata/IWFND/SOME_TECH_SRV/","TechnicalServiceName":"/IWFND/SOME_TECH_SRV"},` +
			`{"ServiceUrl":"https://gw.internal/sap/opu/odata/sap/ZMY_SRV/","TechnicalServiceName":"ZMY_SRV"},` +
			`{"ServiceUrl":"https://gw.internal/sap/opu/odata/sap/API_BUSINESS_PARTNER/","TechnicalServiceName":"API_BUSINESS_PARTNER"}` +
			`]}}`))
	}))
	defer sys.Close()

	store := &remoteStore{conn: &UserConnection{Host: sys.URL, Username: "u", Password: "p"}, http: userHTTPClient(true)}
	services, err := store.discoverServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/sap/opu/odata/sap/API_BUSINESS_PARTNER/", "/sap/opu/odata/sap/ZMY_SRV/", "/sap/opu/odata/IWFND/SOME_TECH_SRV/"}
	if strings.Join(services, ",") != strings.Join(want, ",") {
		t.Errorf("services = %v, want %v", services, want)
	}
}

func TestQueryToolTakesEntity(t *testing.T) {
	sap := newFakeSAP(t)
	bff := fakeBFF(t, map[string]UserConnection{
		"user-1": {Host: sap.URL, Client: "100", Username: "alice", Password: "hunter2", Services: []string{"SVC"}},
	})
	// The question names nothing the matcher knows; entity picks the set.
	out := callTool(t, sessionsFor(bff.URL, true), "user-1", "query-odata-service",
		map[string]any{"query": "first five please", "entity": "countries"})
	if !strings.Contains(out, `"DE"`) {
		t.Fatalf("result = %s", out)
	}
}
