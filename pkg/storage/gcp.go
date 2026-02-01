package storage

import (
	"context"
	"errors"
	"io"
	storagetypes "kerrareg/pkg/storage/types"
	"net/http"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

type GoogleCloudStorage struct {
	client *storage.Client
}

// NewClient initializes a new Google Cloud Storage client using Application Default Credentials.
func (gcs *GoogleCloudStorage) NewClient(ctx context.Context) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	gcs.client = client
	return nil
}

// GetObjectChecksum retrieves the object metadata from GCS to get the SHA256 checksum stored in object metadata.
// If the object is found, it sets the soi receiver's field 'ObjectChecksum' and 'FileExists'.
// If the object cannot be found the function returns an error.
func (gcs *GoogleCloudStorage) GetObjectChecksum(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	bucketName := soi.Version.Spec.ModuleConfigRef.StorageConfig.GCS.Bucket
	objectName := *soi.FilePath

	bucket := gcs.client.Bucket(bucketName)
	obj := bucket.Object(objectName)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
			return err
		}
		return err
	}

	// Retrieve checksum from object metadata
	if checksum, exists := attrs.Metadata["sha256-checksum"]; exists {
		soi.ObjectChecksum = &checksum
	}
	soi.FileExists = true
	return nil
}

// GetObject retrieves the object from Google Cloud Storage and returns an io.Reader to stream the file.
func (gcs *GoogleCloudStorage) GetObject(ctx context.Context, soi *storagetypes.StorageObjectInput) (io.Reader, error) {
	bucketName := soi.Version.Spec.ModuleConfigRef.StorageConfig.GCS.Bucket
	objectName := *soi.FilePath

	bucket := gcs.client.Bucket(bucketName)
	obj := bucket.Object(objectName)
	reader, err := obj.NewReader(ctx)
	if err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
			return nil, err
		}
		return nil, err
	}

	return reader, nil
}

// DeleteObject deletes the Version file from the specified GCS bucket.
func (gcs *GoogleCloudStorage) DeleteObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	bucketName := soi.Version.Spec.ModuleConfigRef.StorageConfig.GCS.Bucket
	objectName := *soi.FilePath

	bucket := gcs.client.Bucket(bucketName)
	obj := bucket.Object(objectName)
	if err := obj.Delete(ctx); err != nil {
		var apiErr *googleapi.Error
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
			// Object doesn't exist, treat as success
			return nil
		}
		return err
	}

	return nil
}

// PutObject uploads the Version file to the specified GCS bucket with its computed base64 encoded SHA256 checksum
// stored in the object metadata.
func (gcs *GoogleCloudStorage) PutObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	bucketName := soi.Version.Spec.ModuleConfigRef.StorageConfig.GCS.Bucket
	objectName := *soi.FilePath

	bucket := gcs.client.Bucket(bucketName)
	obj := bucket.Object(objectName)
	wc := obj.NewWriter(ctx)

	// Set object metadata with checksum
	wc.Metadata = map[string]string{
		"sha256-checksum": *soi.ArchiveChecksum,
	}

	if strings.HasSuffix(*soi.FilePath, ".zip") {
		wc.ContentType = "application/zip"
	} else {
		wc.ContentType = "application/x-tar"
	}

	// Write the file bytes
	if _, err := wc.Write(soi.FileBytes); err != nil {
		wc.Close()
		return err
	}

	// Close the writer to finalize the upload
	if err := wc.Close(); err != nil {
		return err
	}

	return nil
}
