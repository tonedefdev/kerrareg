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
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v50/github"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
	kerraregGithub "kerrareg/services/version/internal/github"
	"kerrareg/services/version/internal/storage"
	"kerrareg/services/version/internal/storage/types"
)

const (
	kerraregControllerName                  = "kerrareg-versions-controller"
	kerraregGithubSecretName                = "kerrareg-github-application-secret"
	kerraregGithubSecretDataFieldAppID      = "githubAppID"
	kerraregGithubSecretDataFieldInstallID  = "githubInstallID"
	kerraregGithubSecretDataFieldPrivateKey = "githubPrivateKey"
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
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KerraregReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	version := &versionv1alpha1.Version{}
	err := r.Get(ctx, req.NamespacedName, version)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("version resource not found. Ignoring since object must be deleted", "module", req.Name)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get version", "version", req.Name)
		return ctrl.Result{}, err
	}

	if !version.Spec.ForceSync && version.Status.Synced {
		return ctrl.Result{}, err
	}

	r.Log.Info(
		"Version found: starting reconciliation",
		"version", version.Spec.Version,
		"versionName", version.Name,
	)

	var fileBytes []byte
	var fileChecksum *string

	if version.Spec.ModuleConfigRef.Name != nil && version.Spec.ProviderConfigRef.Name != nil {
		version.Status.Synced = false
		version.Status.SyncStatus = "Only one of 'ModuleConfigRef' or 'ProviderConfigRef' can be provided: Both are defined"
		err = r.Status().Update(ctx, version)
		return requeueForDuration(30, err)
	}

	if version.Spec.ModuleConfigRef.Name != nil {
		var githubClient *github.Client
		if version.Spec.ModuleConfigRef.GithubClientConfig.UseAuthenticatedClient {
			githubClientConfig, err := r.GetGithubApplicationSecret(ctx, version)
			if err != nil {
				r.Log.Error(err, "Unable to retrieve Github Application secret",
					"version", version.Spec.Version,
					"versionName", version.Name,
				)
				return requeueForDuration(30, err)
			}

			authGithubClient, err := kerraregGithub.CreateGithubClient(ctx, version, githubClientConfig)
			if err != nil {
				r.Log.Error(err, "Unable to create authenticated Github client",
					"version", version.Spec.Version,
					"versionName", version.Name,
				)
				return requeueForDuration(30, err)
			}

			githubClient = authGithubClient
			r.Log.Info("Created authenticated Github client",
				"version", version.Spec.Version,
				"versionName", version.Name,
			)
		}

		var fileFormat github.ArchiveFormat
		if strings.Contains(*version.Spec.FileName, "zip") {
			fileFormat = github.Zipball
		} else {
			fileFormat = github.Tarball
		}

		moduleBytes, checksum, err := kerraregGithub.GetModuleArchiveFromRef(ctx, githubClient, version, fileFormat)
		if err != nil {
			version.Status.Synced = false
			version.Status.SyncStatus = fmt.Sprintf("Failed to retrieve Github archive: %v", err)
			err = r.Status().Update(ctx, version)
			return requeueForDuration(30, err)
		}

		fileBytes = moduleBytes
		fileChecksum = checksum

		// If Module is immutable, the checksum field is non-nil, and the calculated checksum between
		// the Github archive and the stored resource are not a match, then stop processing, update status, and requeue
		if version.Spec.ModuleConfigRef.Immutable && version.Status.Checksum != nil && *version.Status.Checksum != *fileChecksum {
			statusMsg := fmt.Errorf("Version is marked immutable: file checksum doesn't match spec: got '%s'", *fileChecksum)
			r.Log.Error(statusMsg, "checksum mismatch", "versionName", version.Name, "version", version.Spec.Version)

			version.Status.SyncStatus = statusMsg.Error()
			version.Status.Synced = false
			err = r.Status().Update(ctx, version)
			return requeueForDuration(30, err)
		}

		r.Log.Info("Successfully retrieved file archive from Github",
			"version", version.Spec.Version,
			"versionName", version.Name,
		)
	}

	storagePutObjectInput := types.StoragePutObjectInput{
		Checksum:  fileChecksum,
		FileBytes: fileBytes,
		Version:   version,
	}

	if version.Spec.ModuleConfigRef.StorageConfig.S3 != nil {
		amazonS3Storage := &storage.AmazonS3Storage{}
		err = r.NewS3Client(ctx, version, amazonS3Storage)
		if err != nil {
			return requeueForDuration(30, err)
		}

		bucketKey, err := getModuleS3BucketKey(version)
		if err != nil {
			return requeueForDuration(30, err)
		}

		storageInterface := amazonS3Storage
		storagePutObjectInput.FileDestinationPath = bucketKey
		err = r.PutS3Object(ctx, storageInterface, version.Spec.ModuleConfigRef.StorageConfig.S3.Bucket, bucketKey, &storagePutObjectInput)
		if err != nil {
			return requeueForDuration(30, err)
		}
	}

	err = r.ProcessUpdate(ctx, version)
	if err != nil {
		return requeueForDuration(30, err)
	}

	version.Status.Synced = true
	version.Status.Checksum = fileChecksum
	version.Status.SyncStatus = "Successfully synced version"
	err = r.Status().Update(ctx, version, &client.SubResourceUpdateOptions{
		UpdateOptions: client.UpdateOptions{
			FieldManager: kerraregControllerName,
		},
	})

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KerraregReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&versionv1alpha1.Version{}).
		Named(kerraregControllerName).
		Complete(r)
}

// NewS3Client creates a new S3 client based on the configuration received by version.
func (r *KerraregReconciler) NewS3Client(ctx context.Context, version *versionv1alpha1.Version, amazonS3Storage *storage.AmazonS3Storage) error {
	err := amazonS3Storage.NewClient(ctx, version)
	if err != nil {
		r.Log.Error(err, "Unable to create S3 client",
			"version", version.Spec.Version,
			"versionName", version.Name,
		)

		return err
	}

	return nil
}

// PutS3Object is a helper method to put the file received by storagePutObjectInput into an S3 bucket.
func (r *KerraregReconciler) PutS3Object(ctx context.Context, storageInterface storage.Storage, bucket string, bucketKey *string, storagePutObjectInput *types.StoragePutObjectInput) error {
	r.Log.Info("Putting Version in S3 bucket",
		"bucket", bucket,
		"bucketKey", *bucketKey,
		"version", storagePutObjectInput.Version.Spec.Version,
		"versionName", storagePutObjectInput.Version.Name,
	)

	err := storageInterface.PutObject(ctx, storagePutObjectInput)
	if err != nil {
		r.Log.Error(err, "Failed to put Version in S3",
			"bucket", bucket,
			"bucketKey", *bucketKey,
			"version", storagePutObjectInput.Version.Spec.Version,
			"versionName", storagePutObjectInput.Version.Name,
		)

		return err
	}

	r.Log.Info("Successfully put Version in S3",
		"bucket", bucket,
		"bucketKey", *bucketKey,
		"version", storagePutObjectInput.Version.Spec.Version,
		"versionName", storagePutObjectInput.Version.Name,
	)

	return nil
}

// ProcessUpdate processes an update on the received version object. The object must be a pointer to a struct of type version.
func (r *KerraregReconciler) ProcessUpdate(ctx context.Context, version *versionv1alpha1.Version) error {
	// If we have forced synced and reached here then to set this back to
	// false.
	if version.Spec.ForceSync {
		version.Spec.ForceSync = false
	}

	err := r.Update(ctx, version, &client.SubResourceUpdateOptions{
		UpdateOptions: client.UpdateOptions{
			FieldManager: kerraregControllerName,
		},
	})
	if err != nil {
		r.Log.Error(err, "Unable to update Version resource",
			"module", version.Spec.ModuleConfigRef.Name,
			"version", version.Spec.Version,
			"versionName", version.Name,
		)
		return err
	}

	return nil
}

// GetGithubApplicationSecret retrieves the kerrareg-github-application-secret kubernetes secret from the cluster
// and returns a GithubClientConfig for making authenticated requests to the Github API.
func (r *KerraregReconciler) GetGithubApplicationSecret(ctx context.Context, version *versionv1alpha1.Version) (*kerraregGithub.GithubClientConfig, error) {
	object := client.ObjectKey{
		Name:      kerraregGithubSecretName,
		Namespace: version.Namespace,
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, object, secret); err != nil {
		return nil, err
	}

	appID, err := strconv.ParseInt(string(secret.Data[kerraregGithubSecretDataFieldAppID]), 0, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse '%s' as int64: %w", kerraregGithubSecretDataFieldAppID, err)
	}

	installID, err := strconv.ParseInt(string(secret.Data[kerraregGithubSecretDataFieldInstallID]), 0, 64)
	if err != nil {
		return nil, fmt.Errorf("unable to parse '%s' as int64: %w", kerraregGithubSecretDataFieldInstallID, err)
	}

	keyData, err := base64.StdEncoding.DecodeString(string(secret.Data[kerraregGithubSecretDataFieldPrivateKey]))
	if err != nil {
		return nil, fmt.Errorf("unable to decode '%s': %w", kerraregGithubSecretDataFieldPrivateKey, err)
	}

	githubClientConfig := &kerraregGithub.GithubClientConfig{
		AppID:          appID,
		InstallationID: installID,
		PrivateKeyData: keyData,
	}

	return githubClientConfig, nil
}

// getModuleS3BucketKey gets the version's bucket key as either the user defined key
// or the Module's name if the provided S3.Key is nil. The function also removes
// any trailing slashes from the S3.Key if it is a non-nil value.
func getModuleS3BucketKey(version *versionv1alpha1.Version) (*string, error) {
	var bucketKey string
	if version.Spec.ModuleConfigRef.StorageConfig.S3.Key == nil {
		bucketKey = fmt.Sprintf("%s/%s", *version.Spec.ModuleConfigRef.Name, *version.Spec.FileName)
		return &bucketKey, nil
	}

	sanitizedKey, err := storage.RemoveTrailingSlash(version.Spec.ModuleConfigRef.StorageConfig.S3.Key)
	if err != nil {
		return nil, err
	}

	bucketKey = fmt.Sprintf("%s/%s", *sanitizedKey, *version.Spec.FileName)
	return &bucketKey, nil
}

// requeueForDuration is a helper function to return any error received by err, and a reconcile.Result configured to RequeueAfter
// the time.Duration received by duration.
func requeueForDuration(duration time.Duration, err error) (reconcile.Result, error) {
	return reconcile.Result{
		RequeueAfter: time.Duration(duration * time.Second),
	}, err
}
