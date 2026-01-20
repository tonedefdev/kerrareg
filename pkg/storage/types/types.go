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
	// The sha256 checksum of the Github archive as a base64 encoded string.
	ArchiveChecksum *string
	// The storage method to use. One of 'Get', 'Delete', or 'Put'
	Method StorageMethod
	// The archive file as a bite slice.
	FileBytes []byte
	// A flag set to true when the storage system determines the file exists.
	FileExists bool
	// The file path of the storage object. This may be a reference to a cloud storage path such as an `AWS S3 Bucket` key or an `Azure Storage Blob`.
	// It may also refer to a filesystem path like `/foo/bar` on *nix based systems or `C:\foo\bar` for Windows.
	FilePath *string
	// The sha256 checksum of the object from the storage system as a base64 encoded string.
	ObjectChecksum *string
	// The Version spec of the object Version.
	Version *versionv1alpha1.Version
}
