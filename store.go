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

// MetadataStore abstracts the source of OData EDMX XML files.
//
// Implementations:
//   - s3Store    — S3-compatible object storage (AWS S3, MinIO, GCS, R2)
//   - localStore — local filesystem (directory or single file)
//
// The IDs returned by ListXMLFiles are opaque to callers; they must be passed
// unchanged to GetXMLContent.
type MetadataStore interface {
	// ListXMLFiles returns identifiers for all XML files in the store.
	// The search scope (prefix, directory) is determined at construction time.
	ListXMLFiles(ctx context.Context) ([]string, error)

	// GetXMLContent returns the UTF-8 content of the XML file identified by id.
	GetXMLContent(ctx context.Context, id string) (string, error)
}
