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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// natsCache implements CacheStore using NATS JetStream Key-Value storage.
type natsCache struct {
	conn   *nats.Conn
	bucket string
}

// newNATSCache connects to NATS and returns a CacheStore backed by the given
// JetStream KV bucket. The caller must call Close() when done.
func newNATSCache(url, creds string, timeoutSecs int, bucket string) (*natsCache, error) {
	nc, err := connectNATS(url, creds, timeoutSecs)
	if err != nil {
		return nil, err
	}
	return &natsCache{conn: nc, bucket: bucket}, nil
}

// GetConfig retrieves key from the JetStream KV bucket and JSON-decodes it.
// Returns (nil, nil) when the key does not exist.
func (c *natsCache) GetConfig(_ context.Context, key string) (map[string]any, error) {
	js, err := c.conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("opening JetStream: %w", err)
	}
	kv, err := js.KeyValue(c.bucket)
	if err != nil {
		return nil, fmt.Errorf("opening KV bucket %q: %w", c.bucket, err)
	}
	entry, err := kv.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			logger.L.Info("Cache key not found",
				zap.String("bucket", c.bucket), zap.String("key", key))
			return nil, nil
		}
		return nil, fmt.Errorf("fetching key %q from bucket %q: %w", key, c.bucket, err)
	}
	var result map[string]any
	if err := json.Unmarshal(entry.Value(), &result); err != nil {
		return nil, fmt.Errorf("decoding JSON for key %q: %w", key, err)
	}
	logger.L.Info("Cache config fetched",
		zap.String("bucket", c.bucket), zap.String("key", key))
	return result, nil
}

// Close closes the underlying NATS connection.
func (c *natsCache) Close() error {
	c.conn.Close()
	return nil
}

// compile-time interface check
var _ CacheStore = (*natsCache)(nil)

// connectNATS establishes a NATS connection with optional credentials and TLS.
func connectNATS(url, creds string, timeoutSecs int) (*nats.Conn, error) {
	if url == "" {
		return nil, fmt.Errorf("NATS URL is empty")
	}
	opts := []nats.Option{
		nats.Timeout(time.Duration(timeoutSecs) * time.Second),
		nats.Name("odata-mcp"),
	}
	if creds != "" {
		opts = append(opts, nats.UserCredentials(creds))
	}
	if caFile := os.Getenv("NATS_CA_FILE"); caFile != "" {
		opts = append(opts, nats.RootCAs(caFile))
	} else {
		opts = append(opts, nats.Secure(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to NATS at %s: %w", url, err)
	}
	logger.L.Info("Connected to NATS", zap.String("url", url))
	return nc, nil
}
