// Copyright 2025 bluefunda
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import "testing"

func TestExtractChartJSON_Valid(t *testing.T) {
	text := `{"chart_type": "bar", "x_field": "country", "y_field": "population", "group_by": "", "insight": "Population varies widely."}`
	out, err := extractChartJSON(text)
	if err != nil {
		t.Fatalf("extractChartJSON error: %v", err)
	}
	if out.ChartType != "bar" || out.XField != "country" || out.YField != "population" {
		t.Errorf("unexpected chart config: %+v", out)
	}
}

func TestExtractChartJSON_ToleratesSurroundingText(t *testing.T) {
	text := "Sure, here is the config:\n" +
		`{"chart_type": "line", "x_field": "date", "y_field": "revenue", "group_by": "region", "insight": "Revenue trends up."}` +
		"\nHope that helps!"
	out, err := extractChartJSON(text)
	if err != nil {
		t.Fatalf("extractChartJSON error: %v", err)
	}
	if out.ChartType != "line" || out.GroupBy != "region" {
		t.Errorf("unexpected chart config: %+v", out)
	}
}

func TestExtractChartJSON_NoJSON(t *testing.T) {
	if _, err := extractChartJSON("I cannot generate a chart for this."); err == nil {
		t.Error("expected error for text with no JSON object, got nil")
	}
}

func TestExtractChartJSON_IncompleteConfig(t *testing.T) {
	text := `{"chart_type": "", "insight": "not enough fields"}`
	if _, err := extractChartJSON(text); err == nil {
		t.Error("expected error for incomplete chart config, got nil")
	}
}
