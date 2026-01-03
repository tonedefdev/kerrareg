package storage

import (
	"context"
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
)

type ModuleStorage interface {
	DeleteObject(ctx context.Context, moduleVersion *versionv1alpha1.ModuleVersion)
	GetObject(ctx context.Context, moduleVersion *versionv1alpha1.ModuleVersion)
	PutObject(ctx context.Context, moduleBytes []byte, moduleVersion *versionv1alpha1.ModuleVersion) error
}
