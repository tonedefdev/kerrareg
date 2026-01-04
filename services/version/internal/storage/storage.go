package storage

import (
	"context"
	"fmt"
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
	"kerrareg/services/version/internal/storage/types"
	"regexp"
)

// Storage interface implements methods to store a specific Version in an external storage system.
type Storage interface {
	// Deletes a file from the configure storage system.
	DeleteObject(ctx context.Context, version *versionv1alpha1.Version)
	// Gets a file from the configured storage system.
	GetObject(ctx context.Context, version *versionv1alpha1.Version)
	// Puts a new file into the configured storage system.
	PutObject(ctx context.Context, storagePutObjectInput *types.StoragePutObjectInput) error
}

// RemoveTrailingSlash removes trailing char "/" from the string received by s.
func RemoveTrailingSlash(s *string) (sanitizedString *string, err error) {
	if s == nil {
		return nil, fmt.Errorf("the provided string was nil")
	}

	containsTrailingSlash, err := regexp.MatchString("\\/$", *s)
	if err != nil {
		return nil, fmt.Errorf("unable to validate regexp: %w", err)
	}

	if containsTrailingSlash {
		prepString := *s
		prepString = prepString[:len(prepString)-1]
		sanitizedString = &prepString
	}

	return s, err
}
