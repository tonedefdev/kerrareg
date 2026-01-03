package storage

import (
	"bytes"
	"context"
	"fmt"

	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type AmazonS3Storage struct {
	client *s3.Client
}

func (storage *AmazonS3Storage) New(ctx context.Context, moduleVersion *versionv1alpha1.ModuleVersion) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Region))
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %w", err)
	}

	storage.client = s3.NewFromConfig(cfg)
	return nil
}

func (storage *AmazonS3Storage) GetObject(ctx context.Context, moduleVersion *versionv1alpha1.ModuleVersion) {

}

func (storage *AmazonS3Storage) DeleteObject(ctx context.Context, moduleVersion *versionv1alpha1.ModuleVersion) {

}

func (storage *AmazonS3Storage) PutObject(ctx context.Context, moduleBytes []byte, moduleVersion *versionv1alpha1.ModuleVersion) error {
	_, err := storage.client.PutObject(ctx, &s3.PutObjectInput{
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		ChecksumSHA256:    moduleVersion.Spec.Checksum,
		Bucket:            &moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Bucket,
		Key:               &moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Key,
		Body:              bytes.NewReader(moduleBytes),
	})
	if err != nil {
		return err
	}

	return nil
}
