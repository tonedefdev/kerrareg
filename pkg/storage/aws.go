package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	storagetypes "kerrareg/pkg/storage/types"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type AmazonS3Storage struct {
	client *s3.Client
}

// NewClient initializes a new AWS S3 storage client.
func (storage *AmazonS3Storage) NewClient(ctx context.Context, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %w", err)
	}

	storage.client = s3.NewFromConfig(cfg)
	return nil
}

// GetObject retrieves the object from S3 and returns an io.Reader to stream the file from the server
func (storage *AmazonS3Storage) GetObject(ctx context.Context, soi *storagetypes.StorageObjectInput) (io.Reader, error) {
	resp, err := storage.client.GetObject(ctx, &s3.GetObjectInput{
		ChecksumMode: types.ChecksumModeEnabled,
		Bucket:       &soi.Version.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
		Key:          soi.FilePath,
	})

	if err != nil {
		var noSuchKey *types.NoSuchKey

		if errors.As(err, &noSuchKey) {
			return nil, err
		}

		return nil, err
	}

	return resp.Body, nil
}

// GetObjectChecksum retrieves the sha256 checksum directly from the object in the bucket and sets it on the soi receiver's field 'ObjectChecksum'.
// If the key cannot be found the function sets the soi receiver's field for 'FileNotExists'.
func (storage *AmazonS3Storage) GetObjectChecksum(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	resp, err := storage.client.GetObject(ctx, &s3.GetObjectInput{
		ChecksumMode: types.ChecksumModeEnabled,
		Bucket:       &soi.Version.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
		Key:          soi.FilePath,
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey

		if errors.As(err, &noSuchKey) {
			return err
		}

		return err
	}

	defer resp.Body.Close()

	if resp.ChecksumSHA256 != nil {
		soi.ObjectChecksum = resp.ChecksumSHA256
		soi.FileExists = true
	}

	return nil
}

// DeleteObject deletes the Version file from the specified bucket.
func (storage *AmazonS3Storage) DeleteObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	_, err := storage.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &soi.Version.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
		Key:    soi.FilePath,
	})
	if err != nil {
		return err
	}

	return nil
}

// PutObject puts the Version file in the specified bucket with its computed base64 encoded SHA256 checksum.
func (storage *AmazonS3Storage) PutObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	_, err := storage.client.PutObject(ctx, &s3.PutObjectInput{
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		ChecksumSHA256:    soi.ArchiveChecksum,
		Bucket:            &soi.Version.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
		Key:               soi.FilePath,
		Body:              bytes.NewReader(soi.FileBytes),
	})
	if err != nil {
		return err
	}

	return nil
}
