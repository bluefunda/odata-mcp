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
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// maxEntitiesInError caps how many EntitySet names an "unknown entity" error lists.
const maxEntitiesInError = 60

// sapPrefix matches SAP's one-letter EntitySet prefixes: A_ (released API), I_ (interface view),
// C_ (consumption view), X_ (extension) — A_Customer, I_BusinessPartner, C_SalesOrder.
var sapPrefix = regexp.MustCompile(`^[aicx]_`)

var nonLetters = regexp.MustCompile(`[^a-z]+`)

// entityKey is an EntitySet name reduced to what a question would say: lower case, without an
// SAP prefix and without underscores — A_BusinessPartner → "businesspartner".
func entityKey(name string) string {
	return nonLetters.ReplaceAllString(sapPrefix.ReplaceAllString(strings.ToLower(name), ""), "")
}

// minKeyLen keeps very short EntitySet names (e.g. "id", "tax") from matching inside unrelated
// words of a question.
const minKeyLen = 4

// matchEntityByKey matches the question against entityKey of each name: "show me 5 customers"
// → A_Customer, "list business partners" → A_BusinessPartner. Prefers the longest key, so
// A_CustomerCompany beats A_Customer when the question names it. Returns "" when nothing matches.
func matchEntityByKey(query string, entityNames []string) string {
	q := nonLetters.ReplaceAllString(strings.ToLower(query), "")
	best, bestLen := "", 0
	for _, name := range entityNames {
		k := entityKey(name)
		if len(k) < minKeyLen {
			continue
		}
		if containsAny(q, k, strings.TrimSuffix(k, "y")+"ies") && len(k) > bestLen {
			best, bestLen = name, len(k)
		}
	}
	return best
}

// resolveEntity finds the EntitySet the caller named — exactly, case-insensitively, or by its
// entityKey (so "Customer" finds A_Customer). Returns "" when none matches.
func resolveEntity(requested string, entityNames []string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return ""
	}
	for _, name := range entityNames {
		if name == requested {
			return name
		}
	}
	for _, name := range entityNames {
		if strings.EqualFold(name, requested) {
			return name
		}
	}
	key := entityKey(requested)
	for _, name := range entityNames {
		if key != "" && entityKey(name) == key {
			return name
		}
	}
	return ""
}

// entitiesHint lists the available EntitySets so the calling model can retry with `entity`
// set, instead of asking the user to rephrase.
func entitiesHint(entityNames []string) string {
	names := append([]string(nil), entityNames...)
	sort.Strings(names)
	more := ""
	if len(names) > maxEntitiesInError {
		more = fmt.Sprintf(" (and %d more)", len(names)-maxEntitiesInError)
		names = names[:maxEntitiesInError]
	}
	return fmt.Sprintf(" Available entities: %s%s. Call the tool again with entity set to the one that fits.",
		strings.Join(names, ", "), more)
}

// selectEntity picks the EntitySet for a query: the one the caller named in `entity`, else the
// question's best match (matchEntity, then the SAP-aware key match), else the LLM fallback when
// configured. On failure it returns an error message listing the available entities.
func selectEntity(
	ctx context.Context, cfg *Config, query, requested string,
	entityNames []string, services map[string]*ServiceInfo,
) (entity string, errMsg string) {
	if requested != "" {
		if name := resolveEntity(requested, entityNames); name != "" {
			return name, ""
		}
		return "", fmt.Sprintf("There is no entity %q in your OData services.", requested) + entitiesHint(entityNames)
	}
	if name := matchEntity(query, entityNames); name != "" {
		return name, ""
	}
	if name := matchEntityByKey(query, entityNames); name != "" {
		return name, ""
	}
	if name := entityFromLLM(ctx, cfg, query, entityNames, services); name != "" {
		return name, ""
	}
	return "", fmt.Sprintf("I could not determine which data entity to use for your query: %q.", query) +
		entitiesHint(entityNames)
}
