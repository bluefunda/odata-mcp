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

import "context"

// CacheStore provides access to remote configuration key-value storage.
//
// Implementations ship with this package:
//   - natsCache — NATS JetStream KV (see cache_nats.go)
//
// Additional backends (Redis, etcd, Consul, Kafka-backed KV, …) can be added
// by implementing this two-method interface.
type CacheStore interface {
	// GetConfig returns the JSON-decoded value for key.
	// Returns (nil, nil) when the key does not exist.
	GetConfig(ctx context.Context, key string) (map[string]any, error)

	// Close releases the underlying connection.
	Close() error
}

// Well-known config keys stored in the cache bucket.
const (
	cacheKeyS3Config    = "S3Config"     // S3 / object-storage connection params
	cacheKeyODataConfig = "ODataConfig"  // OData service credentials
)

// configString extracts a string value from a decoded config map.
func configString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// configBool extracts a bool value from a decoded config map.
func configBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}
