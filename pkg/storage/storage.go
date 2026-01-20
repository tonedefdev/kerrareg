package storage

import (
	"context"
	"fmt"
	"io"
	"kerrareg/pkg/storage/types"
)

// Storage interface implements methods to store a specific Version in an external storage system.
type Storage interface {
	// Deletes a file from the configured storage system.
	DeleteObject(ctx context.Context, soi *types.StorageObjectInput) error
	// GetObject returns an io.Reader to stream the file from the underlying storage system.
	GetObject(ctx context.Context, soi *types.StorageObjectInput) (io.Reader, error)
	// Gets the checksum of the file from the configured storage system. When it exists the function should store the value
	// in the soi receiver's field `ObjectChecksum` and set its `FileExists` field to `true`.
	GetObjectChecksum(ctx context.Context, soi *types.StorageObjectInput) error
	// Puts a new file into the configured storage system.
	PutObject(ctx context.Context, soi *types.StorageObjectInput) error
}

// RemoveTrailingSlash removes trailing slash characters from the string received by s.
func RemoveTrailingSlash(s *string) (*string, error) {
	if s == nil {
		return nil, fmt.Errorf("the provided string was nil")
	}

	cs := *s
	if cs[len(cs)-1] == '/' || cs[len(cs)-1] == '\\' {
		ps := *s
		ps = ps[:len(ps)-1]
		s = &ps
		return s, nil
	}

	return s, nil
}
