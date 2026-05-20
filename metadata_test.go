// Copyright 2025 bluefunda
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"testing"
)

const sampleEDMX = `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx Version="1.0" xmlns:edmx="http://schemas.microsoft.com/ado/2007/06/edmx"
           xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata"
           xmlns:sap="http://www.sap.com/Protocols/SAPData">
  <edmx:DataServices m:DataServiceVersion="2.0">
    <Schema Namespace="DEMO_SRV" xml:lang="en"
            xmlns="http://schemas.microsoft.com/ado/2008/09/edm"
            xmlns:atom="http://www.w3.org/2005/Atom">
      <atom:link rel="self" href="https://example.com/sap/opu/odata/sap/DEMO_SRV/$metadata"/>
      <EntityType Name="Country" sap:label="Country">
        <Key><PropertyRef Name="CountryCode"/></Key>
        <Property Name="CountryCode" Type="Edm.String" MaxLength="2"/>
        <Property Name="CountryName" Type="Edm.String" MaxLength="100"/>
        <Property Name="ISOCode" Type="Edm.String" MaxLength="3"/>
      </EntityType>
      <EntityType Name="Currency" sap:label="Currency">
        <Key><PropertyRef Name="CurrencyCode"/></Key>
        <Property Name="CurrencyCode" Type="Edm.String" MaxLength="5"/>
        <Property Name="CurrencyName" Type="Edm.String" MaxLength="100"/>
      </EntityType>
      <EntityContainer Name="DEMO_SRV" m:IsDefaultEntityContainer="true">
        <EntitySet Name="Countries" EntityType="DEMO_SRV.Country" sap:label="Countries"/>
        <EntitySet Name="Currencies" EntityType="DEMO_SRV.Currency" sap:label="Currencies"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`

func TestParseMetadata_BaseURL(t *testing.T) {
	meta, err := ParseMetadata(sampleEDMX)
	if err != nil {
		t.Fatalf("ParseMetadata error: %v", err)
	}
	want := "https://example.com/sap/opu/odata/sap/DEMO_SRV"
	if meta.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", meta.BaseURL, want)
	}
}

func TestParseMetadata_EntitySets(t *testing.T) {
	meta, err := ParseMetadata(sampleEDMX)
	if err != nil {
		t.Fatalf("ParseMetadata error: %v", err)
	}

	if len(meta.Entities) != 2 {
		t.Fatalf("len(Entities) = %d, want 2", len(meta.Entities))
	}

	for _, name := range []string{"Countries", "Currencies"} {
		if _, ok := meta.Entities[name]; !ok {
			t.Errorf("entity %q missing from parsed metadata", name)
		}
	}
}

func TestParseMetadata_Properties(t *testing.T) {
	meta, err := ParseMetadata(sampleEDMX)
	if err != nil {
		t.Fatalf("ParseMetadata error: %v", err)
	}

	country := meta.Entities["Countries"]
	wantProps := []string{"CountryCode", "CountryName", "ISOCode"}
	if len(country.Properties) != len(wantProps) {
		t.Fatalf("Countries properties = %v, want %v", country.Properties, wantProps)
	}
	for i, p := range wantProps {
		if country.Properties[i] != p {
			t.Errorf("property[%d] = %q, want %q", i, country.Properties[i], p)
		}
	}
}

func TestParseMetadata_Label(t *testing.T) {
	meta, err := ParseMetadata(sampleEDMX)
	if err != nil {
		t.Fatalf("ParseMetadata error: %v", err)
	}
	if meta.Entities["Countries"].Label != "Country" {
		t.Errorf("Countries label = %q, want %q", meta.Entities["Countries"].Label, "Country")
	}
}

func TestParseMetadata_MissingLink(t *testing.T) {
	noLink := `<?xml version="1.0"?>
<edmx:Edmx xmlns:edmx="http://schemas.microsoft.com/ado/2007/06/edmx">
  <edmx:DataServices>
    <Schema Namespace="X" xmlns="http://schemas.microsoft.com/ado/2008/09/edm">
      <EntityType Name="Foo"><Key><PropertyRef Name="Id"/></Key><Property Name="Id" Type="Edm.Int32"/></EntityType>
      <EntityContainer Name="X"><EntitySet Name="Foos" EntityType="X.Foo"/></EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`
	_, err := ParseMetadata(noLink)
	if err == nil {
		t.Error("expected error for missing atom:link, got nil")
	}
}

func TestMatchEntity_ExactSubstring(t *testing.T) {
	entities := []string{"Countries", "Currencies", "Invoices"}
	got := matchEntity("list all countries please", entities)
	if got != "Countries" {
		t.Errorf("matchEntity = %q, want %q", got, "Countries")
	}
}

func TestMatchEntity_PluralForm(t *testing.T) {
	entities := []string{"Invoice"}
	got := matchEntity("show me invoices", entities)
	if got != "Invoice" {
		t.Errorf("matchEntity = %q, want %q", got, "Invoice")
	}
}

func TestMatchEntity_NoMatch(t *testing.T) {
	entities := []string{"Countries", "Currencies"}
	got := matchEntity("what is the weather today", entities)
	if got != "" {
		t.Errorf("matchEntity = %q, want empty string", got)
	}
}

func TestMatchEntity_LongestWins(t *testing.T) {
	entities := []string{"Bank", "BankAccount"}
	got := matchEntity("show bankaccount details", entities)
	if got != "BankAccount" {
		t.Errorf("matchEntity = %q, want %q", got, "BankAccount")
	}
}

func TestExtractRecords_ODataV4(t *testing.T) {
	input := map[string]any{
		"value": []any{
			map[string]any{"id": 1},
			map[string]any{"id": 2},
		},
	}
	records := extractRecords(input)
	if len(records) != 2 {
		t.Errorf("extractRecords v4 = %d records, want 2", len(records))
	}
}

func TestExtractRecords_ODataV2(t *testing.T) {
	input := map[string]any{
		"d": map[string]any{
			"results": []any{
				map[string]any{"id": 1},
			},
		},
	}
	records := extractRecords(input)
	if len(records) != 1 {
		t.Errorf("extractRecords v2 = %d records, want 1", len(records))
	}
}

func TestExtractRecords_List(t *testing.T) {
	input := []any{map[string]any{"x": 1}, map[string]any{"x": 2}}
	records := extractRecords(input)
	if len(records) != 2 {
		t.Errorf("extractRecords list = %d records, want 2", len(records))
	}
}

func TestBuildFilterString_Empty(t *testing.T) {
	if s := buildFilterString(nil); s != "" {
		t.Errorf("buildFilterString(nil) = %q, want empty", s)
	}
}

func TestBuildFilterString_StringValue(t *testing.T) {
	f := buildFilterString(map[string]any{"Country": "US"})
	if f != "Country eq 'US'" {
		t.Errorf("buildFilterString = %q, want %q", f, "Country eq 'US'")
	}
}

func TestBuildFilterString_BoolValue(t *testing.T) {
	f := buildFilterString(map[string]any{"Active": true})
	if f != "Active eq true" {
		t.Errorf("buildFilterString = %q, want %q", f, "Active eq true")
	}
}
