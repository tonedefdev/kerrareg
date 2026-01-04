package storage

import (
	"bytes"
	"context"
	"fmt"

	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
	storagetypes "kerrareg/services/version/internal/storage/types"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type AmazonS3Storage struct {
	client *s3.Client
}

// NewClient initializes a new AWS S3 storage client.
func (storage *AmazonS3Storage) NewClient(ctx context.Context, version *versionv1alpha1.Version) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(version.Spec.ModuleConfigRef.StorageConfig.S3.Region))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %w", err)
	}

	storage.client = s3.NewFromConfig(cfg)
	return nil
}

func (storage *AmazonS3Storage) GetObject(ctx context.Context, version *versionv1alpha1.Version) {

}

func (storage *AmazonS3Storage) DeleteObject(ctx context.Context, version *versionv1alpha1.Version) {

}

// PutObject puts the Version file in the specified bucket with its computed base64 encoded SHA256 checksum.
func (storage *AmazonS3Storage) PutObject(ctx context.Context, storagePutObjectInput *storagetypes.StoragePutObjectInput) error {
	_, err := storage.client.PutObject(ctx, &s3.PutObjectInput{
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		ChecksumSHA256:    storagePutObjectInput.Checksum,
		Bucket:            &storagePutObjectInput.Version.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
		Key:               storagePutObjectInput.FileDestinationPath,
		Body:              bytes.NewReader(storagePutObjectInput.FileBytes),
	})
	if err != nil {
		return err
	}

	return nil
}
