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
	"io"
	"strings"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

// s3Store implements MetadataStore for any S3-compatible object storage:
// AWS S3, MinIO, GCS (S3 interop), Cloudflare R2, DigitalOcean Spaces, etc.
// It uses the S3 API via the MinIO Go SDK, which is a pure-Go S3 client.
type s3Store struct {
	client *minio.Client
	bucket string
	prefix string // key prefix filter, set at construction time
}

// newS3Store connects to an S3-compatible endpoint and verifies the bucket.
//
// endpoint examples:
//   - AWS S3:   "s3.amazonaws.com"
//   - MinIO:    "localhost:9000"  or  "minio.internal:9000"
//   - R2:       "<account>.r2.cloudflarestorage.com"
//
// Port 9000 with secure=true is automatically downgraded to HTTP because that
// is the conventional MinIO plaintext port.
func newS3Store(endpoint, accessKey, secretKey, bucket string, secure bool, prefix string) (*s3Store, error) {
	if endpoint == "" || bucket == "" {
		return nil, fmt.Errorf("s3 store: endpoint and bucket must not be empty")
	}

	if secure && strings.Contains(endpoint, ":9000") {
		logger.L.Warn("S3 endpoint uses port 9000 with secure=true; overriding to HTTP",
			zap.String("endpoint", endpoint))
		secure = false
	}

	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 store: creating client: %w", err)
	}

	ctx := context.Background()
	exists, err := mc.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("s3 store: checking bucket %q: %w", bucket, err)
	}
	if !exists {
		logger.L.Warn("S3 bucket not found, creating it", zap.String("bucket", bucket))
		if err := mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("s3 store: creating bucket %q: %w", bucket, err)
		}
	}

	logger.L.Info("S3 store initialised",
		zap.String("endpoint", endpoint),
		zap.String("bucket", bucket),
		zap.Bool("secure", secure),
		zap.String("prefix", prefix),
	)
	return &s3Store{client: mc, bucket: bucket, prefix: prefix}, nil
}

// ListXMLFiles returns all object keys under the store's prefix whose name ends in ".xml".
func (s *s3Store) ListXMLFiles(ctx context.Context) ([]string, error) {
	var files []string
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    s.prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("s3 store: listing objects: %w", obj.Err)
		}
		if strings.HasSuffix(strings.ToLower(obj.Key), ".xml") {
			files = append(files, obj.Key)
		}
	}
	logger.L.Info("S3 store: listed XML files",
		zap.String("prefix", s.prefix), zap.Int("count", len(files)))
	return files, nil
}

// GetXMLContent retrieves an object by key and returns its UTF-8 content.
func (s *s3Store) GetXMLContent(ctx context.Context, key string) (string, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return "", fmt.Errorf("s3 store: getting %q: %w", key, err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return "", fmt.Errorf("s3 store: reading %q: %w", key, err)
	}
	logger.L.Info("S3 store: retrieved XML", zap.String("key", key), zap.Int("bytes", len(data)))
	return string(data), nil
}

// compile-time interface check
var _ MetadataStore = (*s3Store)(nil)
