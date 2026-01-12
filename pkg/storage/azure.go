package storage

import (
	"context"
	"errors"
	storagetypes "kerrareg/pkg/storage/types"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

type AzureBlobStorage struct {
	blobClient     *azblob.Client
	storageClient  *armstorage.BlobContainersClient
	AccountName    string `json:"accountName"`
	AccountUrl     string `json:"accountUrl"`
	SubscriptionID string `json:"subscriptionID"`
	ResourceGroup  string `json:"resourceGroup"`
}

func (storage *AzureBlobStorage) NewClient() error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return err
	}

	storageFactory, err := armstorage.NewClientFactory(storage.SubscriptionID, cred, nil)
	if err != nil {
		return err
	}

	blobClient, err := azblob.NewClient(storage.AccountUrl, cred, nil)
	if err != nil {
		return err
	}

	storage.blobClient = blobClient
	storage.storageClient = storageFactory.NewBlobContainersClient()
	return nil
}

// GetObjectChecksum retrieves the sha256 checksum from the container and sets it on the soi receiver's field 'ObjectChecksum'.
// If the container cannot be found the function sets the soi receiver's field for 'FileNotExists'.
func (storage *AzureBlobStorage) GetObjectChecksum(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	ctr, err := storage.storageClient.Get(ctx, storage.ResourceGroup, storage.AccountName, *soi.Version.Spec.ModuleConfigRef.Name, nil)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) {
			if respErr.StatusCode == http.StatusNotFound {
				return err
			}
		}
	}

	soi.ObjectChecksum = ctr.ContainerProperties.Metadata["Checksum"]
	return nil
}

// DeleteObject deletes the Version file from the specified bucket.
func (storage *AzureBlobStorage) DeleteObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	return nil
}

// PutObject puts the Version file in the specified bucket with its computed base64 encoded SHA256 checksum.
func (storage *AzureBlobStorage) PutObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	ctr, err := storage.storageClient.Create(ctx, storage.ResourceGroup, storage.AccountName, *soi.Version.Spec.ModuleConfigRef.Name, armstorage.BlobContainer{
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
