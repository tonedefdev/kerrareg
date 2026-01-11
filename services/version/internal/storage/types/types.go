package types

import (
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
)

//go:generate stringer -type=StorageMethod
type StorageMethod int

const (
	Get StorageMethod = iota
	Delete
	Put
)

// StorageObjectInput is common configuration for various storage systems.
type StorageObjectInput struct {
	// The sha256 checksum of the Github archive as a base64 encoded string
	ArchiveChecksum *string
	// The storage method to use. One of 'Get', 'Delete', or 'Put'
	Method StorageMethod
	// The archive file as a bite slice
	FileBytes []byte
	// A flag to determine if the underlying storage system was able to determine if the file exists
	FileNotExists bool
	// The file path of the storage object. This may be a reference to a cloud storage path such as an S3 Bucket key or an Azure Storage Blob
	FilePath *string
	// The checksum of the object in the storage system.
	ObjectChecksum *string
	// The Version spec of the object Version
	Version *versionv1alpha1.Version
}
