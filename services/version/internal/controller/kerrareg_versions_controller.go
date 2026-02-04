/*
Copyright 2025 Anthony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kerraregv1alpha1 "github.com/tonedefdev/kerrareg/api/v1alpha1"
	kerraregGithub "github.com/tonedefdev/kerrareg/pkg/github"
	"github.com/tonedefdev/kerrareg/pkg/storage"
	"github.com/tonedefdev/kerrareg/pkg/storage/types"
)

const (
	kerraregControllerName = "kerrareg-versions-controller"
)

var (
	defaultRequeueDuration = 30
)

// KerraregReconciler reconciles a Kerrareg object
type KerraregReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kerrareg.io,resources=versions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=versions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=versions/finalizers,verbs=update
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules,verbs=get
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KerraregReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	version := &kerraregv1alpha1.Version{}
	err := r.Get(ctx, req.NamespacedName, version)
	if err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("version resource not found. Ignoring since object must be deleted", "module", req.Name)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get version", "version", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info(
		"Version found: starting reconciliation",
		"version", version.Spec.Version,
		"versionName", version.Name,
	)

	moduleObject := client.ObjectKey{
		Name:      *version.Spec.ModuleConfigRef.Name,
		Namespace: req.Namespace,
	}

	var module kerraregv1alpha1.Module
	if err = r.Get(ctx, moduleObject, &module); err != nil {
		return ctrl.Result{}, err
	}

	if version.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(version, kerraregv1alpha1.KerraregFinalizer) {
			r.Log.V(5).Info("Adding finalizer",
				"version", version.Spec.Version,
				"versionName", version.Name,
			)

			updated := controllerutil.AddFinalizer(version, kerraregv1alpha1.KerraregFinalizer)
			if !updated {
				return ctrl.Result{}, err
			}

			var currentVersion kerraregv1alpha1.Version
			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.Get(ctx, req.NamespacedName, &currentVersion); err != nil {
					return err
				}

				currentVersion.ObjectMeta.Finalizers = version.ObjectMeta.Finalizers

				if err = r.Update(ctx, &currentVersion); err != nil {
					return err
				}

				return nil
			}); err != nil {
				r.Log.Error(err, "Failed to update Version finalizers",
					"version", version.Name,
				)
				return ctrl.Result{
					RequeueAfter: time.Duration(30 * time.Second),
				}, err
			}

			return ctrl.Result{
				RequeueAfter: time.Duration(1 * time.Second),
			}, nil
		}
	} else {
		// The object is being deleted
		r.Log.V(5).Info("Object is being deleted", "version", version.Spec)
		if controllerutil.ContainsFinalizer(version, kerraregv1alpha1.KerraregFinalizer) {
			filePath, err := getModuleFilePath(version)
			if err != nil {
				return ctrl.Result{}, err
			}

			version.Spec.ModuleConfigRef = &module.Spec.ModuleConfig
			soi := &types.StorageObjectInput{
				Method:   types.Delete,
				FilePath: filePath,
				Version:  version,
			}

			if err := r.InitStorageFactory(ctx, soi); err != nil {
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(version, kerraregv1alpha1.KerraregFinalizer)
			if err := r.Update(ctx, version); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}
	}

	if module.Status.ModuleVersionRefs[version.Spec.Version].FileName == nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	switch version.Spec.Type {
	case kerraregv1alpha1.KerraregModule:
		{
			version.Spec.ModuleConfigRef = &module.Spec.ModuleConfig
			version.Spec.FileName = module.Status.ModuleVersionRefs[version.Spec.Version].FileName

			if module.Spec.ModuleConfig.Name == nil {
				version.Spec.ModuleConfigRef.Name = &module.ObjectMeta.Name
			} else {
				version.Spec.ModuleConfigRef.Name = module.Spec.ModuleConfig.Name
			}
		}
	default:
		{
			return ctrl.Result{}, fmt.Errorf("No Type could be determined")
		}
	}

	var archiveChecksum *string
	var fileBytes []byte
	var filePath *string

	if version.Spec.ModuleConfigRef != nil && version.Spec.ProviderConfigRef != nil {
		version.Status.Synced = false
		version.Status.SyncStatus = "Only one of 'ModuleConfigRef' or 'ProviderConfigRef' can be provided: Both are defined"
		err = r.Status().Update(ctx, version)
		return ctrl.Result{}, err
	}

	var githubClientConfig *kerraregGithub.GithubClientConfig
	if version.Spec.ModuleConfigRef.Name != nil {
		var githubClient *github.Client
		if version.Spec.ModuleConfigRef.GithubClientConfig.UseAuthenticatedClient {
			githubClientConfig, err = kerraregGithub.GetGithubApplicationSecret(ctx, r.Client, version.Namespace)
			if err != nil {
				r.Log.Error(err, "Unable to retrieve Github Application secret",
					"version", version.Spec.Version,
					"versionName", version.Name,
				)
				return ctrl.Result{}, err
			}
		}

		authGithubClient, err := kerraregGithub.CreateGithubClient(ctx, version.Spec.ModuleConfigRef.GithubClientConfig.UseAuthenticatedClient, githubClientConfig)
		if err != nil {
			r.Log.Error(err, "Unable to create authenticated Github client",
				"version", version.Spec.Version,
				"versionName", version.Name,
			)
			return ctrl.Result{}, err
		}

		githubClient = authGithubClient
		r.Log.V(5).Info("Created authenticated Github client",
			"version", version.Spec.Version,
			"versionName", version.Name,
		)

		var fileFormat github.ArchiveFormat
		if strings.Contains(*version.Spec.FileName, "zip") {
			fileFormat = github.Zipball
		} else {
			fileFormat = github.Tarball
		}

		moduleBytes, checksum, err := kerraregGithub.GetModuleArchiveFromRef(ctx, r.Log, githubClient, version, fileFormat)
		if err != nil {
			version.Status.SyncStatus = fmt.Sprintf("Failed to retrieve Github archive: %v", err)
			err = r.Status().Update(ctx, version)
			return ctrl.Result{}, err
		}

		fileBytes = moduleBytes
		archiveChecksum = checksum

		r.Log.V(5).Info("Successfully retrieved file archive from Github",
			"version", version.Spec.Version,
			"versionName", version.Name,
		)

		// If Module is immutable, the checksum field is non-nil, and the calculated checksum between
		// the Github archive and the version stored in the resource's status do not match - stop processing, update status, and requeue.
		if *version.Spec.ModuleConfigRef.Immutable && version.Status.Checksum != nil && *version.Status.Checksum != *archiveChecksum {
			statusMsg := fmt.Errorf("Version is marked immutable: archive checksum doesn't match spec: got '%s'", *archiveChecksum)
			r.Log.Error(statusMsg, "checksum mismatch", "versionName", version.Name, "version", version.Spec.Version)

			version.Status.SyncStatus = statusMsg.Error()
			version.Status.Synced = false
			err = r.Status().Update(ctx, version)
			return ctrl.Result{}, err
		}

		filePath, err = getModuleFilePath(version)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if filePath == nil {
		return ctrl.Result{}, fmt.Errorf("the filePath was nil")
	}

	soi := &types.StorageObjectInput{
		FileBytes: fileBytes,
		FilePath:  filePath,
		Version:   version,
	}

	if version.Status.Checksum != nil {
		// Get the checksum of the object from the storage system
		// and set its value in soi receiver's ObjectChecksum field
		soi.Method = types.Get
		_ = r.InitStorageFactory(ctx, soi)
	} else {
		soi.Method = types.Put
		r.Log.V(5).Info("No checksum status: reconciling object",
			"version", version.Spec.Version,
			"versionName", version.Name,
		)
		if err = r.InitStorageFactory(ctx, soi); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !soi.FileExists || soi.ObjectChecksum != nil && version.Status.Checksum != nil && *soi.ObjectChecksum != *version.Status.Checksum {
		soi.Method = types.Put
		r.Log.V(5).Info("File is missing or checksum mismatch: reconciling object",
			"version", version.Spec.Version,
			"versionName", version.Name,
		)
		if err = r.InitStorageFactory(ctx, soi); err != nil {
			return ctrl.Result{}, err
		}
	}

	r.Log.V(5).Info("File exists and checksums match: finished reconciling",
		"version", version.Spec.Version,
		"versionName", version.Name,
	)

	var currentVersion kerraregv1alpha1.Version
	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, req.NamespacedName, &currentVersion); err != nil {
			return err
		}

		currentVersion.Spec.FileName = version.Spec.FileName
		currentVersion.Spec.ModuleConfigRef = version.Spec.ModuleConfigRef
		currentVersion.Spec.ProviderConfigRef = version.Spec.ProviderConfigRef

		r.Log.V(5).Info("old version", "old", currentVersion.Spec)
		r.Log.V(5).Info("new version", "new", version.Spec)
		if err := r.Update(ctx, &currentVersion); err != nil {
			return err
		}
		return nil
	}); err != nil {
		r.Log.Error(err, "Failed to update Version",
			"version", version.Name,
		)
		return ctrl.Result{
			RequeueAfter: time.Duration(30 * time.Second),
		}, err
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, req.NamespacedName, &currentVersion); err != nil {
			return err
		}

		version.Status.Synced = true
		version.Status.Checksum = archiveChecksum
		version.Status.SyncStatus = "Successfully synced version"

		err = r.Status().Update(ctx, version, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{
				FieldManager: kerraregControllerName,
			},
		})

		return nil
	}); err != nil {
		r.Log.Error(err, "Failed to update Version status",
			"version", version.Name,
		)
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("Successfully reconciled Version",
		"version", version.Name,
		"namespace", version.Namespace,
	)

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KerraregReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kerraregv1alpha1.Version{}).
		Named(kerraregControllerName).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}

// RunStorageFactory is the runtime handler for managing storage objects received by 'soi'. It uses the concrete implementation
// of the storage.Storage interface to interact with the underlying storage system.
func RunStorageFactory(ctx context.Context, storageInterface storage.Storage, soi *types.StorageObjectInput) error {
	switch soi.Method {
	case types.Delete:
		if err := storageInterface.DeleteObject(ctx, soi); err != nil {
			return err
		}
	case types.Get:
		if err := storageInterface.GetObjectChecksum(ctx, soi); err != nil {
			return err
		}
	case types.Put:
		if err := storageInterface.PutObject(ctx, soi); err != nil {
			return err
		}
	default:
		{
			return fmt.Errorf("No useable Type provided")
		}
	}

	return nil
}

// InitStorageFactory prepares and inits the storage factory runtime by providing a concrete implementation of the storage.Storage interface.
func (r *KerraregReconciler) InitStorageFactory(ctx context.Context, soi *types.StorageObjectInput) error {
	var storageInterface storage.Storage
	if soi.Version.Spec.ModuleConfigRef.StorageConfig.FileSystem != nil {
		storageInterface = &storage.FileSystem{}
		if err := RunStorageFactory(ctx, storageInterface, soi); err != nil {
			return err
		}

		return nil
	}

	if soi.Version.Spec.ModuleConfigRef.StorageConfig.S3 != nil {
		amazonS3Storage := &storage.AmazonS3Storage{}
		err := amazonS3Storage.NewClient(ctx, soi.Version.Spec.ModuleConfigRef.StorageConfig.S3.Region)
		if err != nil {
			return err
		}

		storageInterface = amazonS3Storage
		err = RunStorageFactory(ctx, storageInterface, soi)
		if err != nil {
			return err
		}

		return nil
	}

	return fmt.Errorf("At least one StorageConfig must be configured on the Module.")
}

// getModuleFilePath gets the Version's file path as either the user defined storage path
// or the Module's name if the relevant storage path field is nil. The function also removes
// any trailing slashes from the storage path field if it is a non-nil value.
func getModuleFilePath(version *kerraregv1alpha1.Version) (*string, error) {
	var filePath string
	if version.Spec.ModuleConfigRef.StorageConfig.S3 != nil && version.Spec.ModuleConfigRef.StorageConfig.S3.Key != nil {
		sanitized, err := storage.RemoveTrailingSlash(version.Spec.ModuleConfigRef.StorageConfig.S3.Key)
		if err != nil {
			return nil, err
		}

		filePath = fmt.Sprintf("%s/%s/%s", *sanitized, *version.Spec.ModuleConfigRef.Name, *version.Spec.FileName)
		return &filePath, nil
	}

	if version.Spec.ModuleConfigRef.StorageConfig.FileSystem != nil && version.Spec.ModuleConfigRef.StorageConfig.FileSystem.DirectoryPath != nil {
		sanitized, err := storage.RemoveTrailingSlash(version.Spec.ModuleConfigRef.StorageConfig.FileSystem.DirectoryPath)
		if err != nil {
			return nil, err
		}

		filePath = fmt.Sprintf("%s/%s/%s", *sanitized, *version.Spec.ModuleConfigRef.Name, *version.Spec.FileName)
		return &filePath, nil
	}

	filePath = fmt.Sprintf("%s/%s", *version.Spec.ModuleConfigRef.Name, *version.Spec.FileName)
	return &filePath, nil
}
