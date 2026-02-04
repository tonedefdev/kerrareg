package storage

import (
	"context"
	"errors"
	"io"
	"net/http"

	storagetypes "github.com/tonedefdev/kerrareg/pkg/storage/types"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

type AzureBlobStorage struct {
	blobClient    *azblob.Client
	storageClient *armstorage.BlobContainersClient
}

// NewClients creates a new azblob.Client and armstorage.BlobContainersClient to interact with the
// Azure storage systems.
func (storage *AzureBlobStorage) NewClients(subscriptionID string, storageAccountUrl string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return err
	}

	storageFactory, err := armstorage.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		return err
	}

	blobClient, err := azblob.NewClient(storageAccountUrl, cred, nil)
	if err != nil {
		return err
	}

	storage.blobClient = blobClient
	storage.storageClient = storageFactory.NewBlobContainersClient()
	return nil
}

// GetObject retrieves the object from the Azure Blob and returns an io.Reader to stream the file from the server
func (storage *AzureBlobStorage) GetObject(ctx context.Context, soi *storagetypes.StorageObjectInput) (io.Reader, error) {
	blob, err := storage.blobClient.DownloadStream(ctx,
		*soi.Version.Spec.ModuleConfigRef.Name,
		*soi.FilePath,
		&azblob.DownloadStreamOptions{},
	)
	if err != nil {
		return nil, err
	}

	return blob.Body, err
}

// GetObjectChecksum retrieves the sha256 checksum from the container's metadata and sets it on the soi receiver's field `ObjectChecksum`.
// If the container can be found the function sets the soi receiver's field for `FileExists` to `true`.
func (storage *AzureBlobStorage) GetObjectChecksum(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	ctr, err := storage.storageClient.Get(ctx,
		soi.Version.Spec.ModuleConfigRef.StorageConfig.AzureStorage.ResourceGroup,
		soi.Version.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountName,
		*soi.Version.Spec.ModuleConfigRef.Name,
		nil,
	)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) {
			if respErr.StatusCode == http.StatusNotFound {
				return err
			}
		}
	}

	soi.ObjectChecksum = ctr.ContainerProperties.Metadata["Checksum"]
	soi.FileExists = true
	return nil
}

// DeleteObject deletes the Version file from the specified bucket.
func (storage *AzureBlobStorage) DeleteObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	return nil
}

// PutObject puts the Version file in the specified bucket with its computed base64 encoded SHA256 checksum.
func (storage *AzureBlobStorage) PutObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	ctr, err := storage.storageClient.Create(ctx,
		soi.Version.Spec.ModuleConfigRef.StorageConfig.AzureStorage.ResourceGroup,
		soi.Version.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountName,
		*soi.Version.Spec.ModuleConfigRef.Name,
		armstorage.BlobContainer{
			ContainerProperties: &armstorage.ContainerProperties{
				Metadata: map[string]*string{
					"Checksum": soi.ArchiveChecksum,
				},
			},
		}, nil)
	if err != nil {
		return err
	}

	bufferOptions := &azblob.UploadBufferOptions{
		Concurrency: 10,
	}

	_, err = storage.blobClient.UploadBuffer(ctx, *ctr.Name, *soi.FilePath, soi.FileBytes, bufferOptions)
	if err != nil {
		return err
	}

	return nil
}
