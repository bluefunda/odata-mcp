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
	"os"
	"path/filepath"
	"strings"

	"github.com/bluefunda/odata-mcp/internal/logger"
	"go.uber.org/zap"
)

// localStore implements MetadataStore for the local filesystem.
//
// path may be:
//   - a single .xml file  — ListXMLFiles returns that one file; prefix is ignored
//   - a directory         — ListXMLFiles returns all *.xml files found recursively
type localStore struct {
	path  string
	isDir bool
}

// newLocalStore validates path and returns a localStore.
func newLocalStore(path string) (*localStore, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("local store: cannot access %q: %w", path, err)
	}
	s := &localStore{path: path, isDir: info.IsDir()}
	logger.L.Info("Local metadata store initialised",
		zap.String("path", path),
		zap.Bool("is_dir", s.isDir),
	)
	return s, nil
}

// ListXMLFiles returns absolute paths for all XML files in the store.
func (s *localStore) ListXMLFiles(_ context.Context) ([]string, error) {
	if !s.isDir {
		if strings.HasSuffix(strings.ToLower(s.path), ".xml") {
			return []string{s.path}, nil
		}
		return nil, fmt.Errorf("local store: %q is not an XML file", s.path)
	}

	var files []string
	err := filepath.WalkDir(s.path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(p), ".xml") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("local store: walking %q: %w", s.path, err)
	}

	logger.L.Info("Local store: listed XML files",
		zap.String("dir", s.path), zap.Int("count", len(files)))
	return files, nil
}

// GetXMLContent reads the file at id (absolute path returned by ListXMLFiles).
func (s *localStore) GetXMLContent(_ context.Context, id string) (string, error) {
	data, err := os.ReadFile(id)
	if err != nil {
		return "", fmt.Errorf("local store: reading %q: %w", id, err)
	}
	logger.L.Info("Local store: read XML file", zap.String("path", id), zap.Int("bytes", len(data)))
	return string(data), nil
}

// compile-time interface check
var _ MetadataStore = (*localStore)(nil)
