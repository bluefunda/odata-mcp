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
	"encoding/xml"
	"fmt"
	"strings"
)

// EntityInfo holds properties and label for one EntitySet.
type EntityInfo struct {
	Label      string
	Properties []string
}

// ServiceMetadata holds the parsed result of one EDMX file.
type ServiceMetadata struct {
	BaseURL  string
	Entities map[string]EntityInfo // keyed by EntitySet name
}

// ParseMetadata parses an EDMX XML string and returns ServiceMetadata.
// Mirrors metadata_parser.py parse_metadata_from_xml_content().
//
// The function uses a two-pass approach:
//  1. Walk all Schema nodes to collect EntityType definitions.
//  2. Walk EntityContainer nodes to map EntitySet names to those types.
//
// atom:link[@rel="self"] is searched inside Schema children because SAP EDMX
// places that element inside the Schema, not at the Edmx root.
func ParseMetadata(xmlContent string) (*ServiceMetadata, error) {
	dec := xml.NewDecoder(strings.NewReader(xmlContent))

	// Go's encoding/xml does not support namespace-prefixed attribute matching
	// out of the box. We collect everything manually via token streaming so we
	// can handle arbitrary namespace prefixes.
	return parseByTokenStream(dec)
}

// parseByTokenStream does a single-pass token walk of the EDMX document.
// It is namespace-aware by comparing the local part of attribute names
// (e.g. "label" regardless of prefix) — which is sufficient here because SAP
// EDMX uses only one attribute per namespace per element that we care about.
func parseByTokenStream(dec *xml.Decoder) (*ServiceMetadata, error) {
	meta := &ServiceMetadata{
		Entities: make(map[string]EntityInfo),
	}

	entityTypes := make(map[string]EntityInfo) // keyed by fully-qualified type name
	var schemaNamespace string

	var currentEntityType string
	var currentEntityProps []string
	var currentEntityLabel string

	inEntityType := false
	inEntityContainer := false

	for {
		tok, err := dec.Token()
		if err != nil {
			break // io.EOF or parse error — we accept partial results
		}

		switch t := tok.(type) {
		case xml.StartElement:
			localName := t.Name.Local

			switch localName {
			case "Schema":
				if ns := attrVal(t.Attr, "Namespace"); ns != "" {
					schemaNamespace = ns
				}

			case "link":
				// atom:link[@rel="self"] contains the service $metadata URL
				if attrVal(t.Attr, "rel") == "self" && meta.BaseURL == "" {
					if href := attrVal(t.Attr, "href"); href != "" {
						meta.BaseURL = strings.TrimSuffix(href, "/$metadata")
					}
				}

			case "EntityType":
				inEntityType = true
				name := attrVal(t.Attr, "Name")
				currentEntityType = schemaNamespace + "." + name
				currentEntityProps = nil
				// attrVal matches by Local name — namespace prefix (sap:label) is irrelevant
				currentEntityLabel = attrVal(t.Attr, "label")
				if currentEntityLabel == "" {
					currentEntityLabel = name
				}

			case "Property":
				if inEntityType {
					if propName := attrVal(t.Attr, "Name"); propName != "" {
						currentEntityProps = append(currentEntityProps, propName)
					}
				}

			case "EntityContainer":
				inEntityContainer = true

			case "EntitySet":
				if inEntityContainer {
					setName := attrVal(t.Attr, "Name")
					typeName := attrVal(t.Attr, "EntityType")
					if setName != "" && typeName != "" {
						if info, ok := entityTypes[typeName]; ok {
							meta.Entities[setName] = info
						}
					}
				}
			}

		case xml.EndElement:
			switch t.Name.Local {
			case "EntityType":
				if inEntityType && currentEntityType != "" {
					entityTypes[currentEntityType] = EntityInfo{
						Label:      currentEntityLabel,
						Properties: currentEntityProps,
					}
				}
				inEntityType = false
				currentEntityType = ""

			case "EntityContainer":
				inEntityContainer = false
			}
		}
	}

	if meta.BaseURL == "" {
		return nil, fmt.Errorf("atom:link[@rel=\"self\"] not found in EDMX — cannot determine base URL")
	}
	if len(meta.Entities) == 0 {
		return nil, fmt.Errorf("no EntitySets found in EDMX")
	}

	return meta, nil
}

// attrVal returns the value of a named attribute, matching on Local name only
// so namespace prefixes (e.g. sap:label → local "label") are ignored.
func attrVal(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}
