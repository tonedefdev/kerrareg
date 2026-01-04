package types

import (
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
)

type StoragePutObjectInput struct {
	Checksum            *string
	FileBytes           []byte
	FileDestinationPath *string
	Version             *versionv1alpha1.Version
}
