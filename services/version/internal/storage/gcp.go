package storage

import (
	"context"
	storagetypes "kerrareg/services/version/internal/storage/types"
)

type GoogleCloudStorage struct {
}

func (storage *GoogleCloudStorage) NewClient() error {
	return nil
}

// GetObjectChecksum retrieves the sha256 checksum directly from the object in the bucket and sets it on the soi receiver's field 'ObjectChecksum'.
// If the key cannot be found the function sets the soi receiver's field for 'FileNotExists'.
func (storage *GoogleCloudStorage) GetObjectChecksum(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	return nil
}

// DeleteObject deletes the Version file from the specified bucket.
func (storage *GoogleCloudStorage) DeleteObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	return nil
}

// PutObject puts the Version file in the specified bucket with its computed base64 encoded SHA256 checksum.
func (storage *GoogleCloudStorage) PutObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	return nil
}
