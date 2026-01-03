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
	storageInterface       storage.ModuleStorage
)

// KerraregReconciler reconciles a Kerrareg object
type KerraregReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kerrareg.io,resources=modules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules/finalizers,verbs=update
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KerraregReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	moduleVersion := &versionv1alpha1.ModuleVersion{}
	err := r.Get(ctx, req.NamespacedName, moduleVersion)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("ModuleVersion resource not found. Ignoring since object must be deleted", "module", req.Name)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "failed to get ModuleVersion", "moduleVersion", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.Info(
		"module version found: reconciling based on its config",
		"moduleVersion", moduleVersion.Spec.Version,
		"module", moduleVersion.Spec.ModuleConfig.Name,
	)

	var githubClient *github.Client
	if moduleVersion.Spec.ModuleConfig.GithubClientConfig.UseAuthenticatedClient {
		githubClientConfig, err := r.GetGithubApplicationSecret(ctx, moduleVersion)
		if err != nil {
			r.Log.Error(err, "unable to retrieve Github Application secret",
				"module", moduleVersion.Spec.ModuleConfig.Name,
				"moduleVersion", moduleVersion.Spec.Version,
			)
		}

		authGithubClient, err := kerraregGithub.CreateGithubClient(ctx, moduleVersion, githubClientConfig)
		if err != nil {
			r.Log.Error(err, "unable to create authenticated Github client",
				"module", moduleVersion.Spec.ModuleConfig.Name,
				"moduleVersion", moduleVersion.Spec.Version,
			)
		}

		githubClient = authGithubClient
	}

	var fileFormat github.ArchiveFormat
	if strings.Contains(*moduleVersion.Spec.FileName, "zip") {
		fileFormat = github.Zipball
	} else {
		fileFormat = github.Tarball
	}

	moduleBytes, checksum, err := kerraregGithub.GetModuleArchiveFromRef(ctx, githubClient, moduleVersion, fileFormat)
	if err != nil {
		moduleVersion.Status.Synced = false
		moduleVersion.Status.SyncStatus = fmt.Sprintf("failed to retrieve Github archive: %v", err)
		err = r.Status().Update(ctx, moduleVersion)
		return ctrl.Result{
			RequeueAfter: time.Duration(30 * time.Second),
		}, err
	}

	// if module is immutable, the checksum field is non-nil, and the calculated checksum between
	// the Github archive and the resource are not a match then stop processing, update status, and requeue
	if moduleVersion.Spec.ModuleConfig.Immutable && moduleVersion.Spec.Checksum != nil && *moduleVersion.Spec.Checksum != *checksum {
		statusMsg := fmt.Errorf("module is marked immutable: file checksum doesn't match spec: got '%s'", *checksum)
		r.Log.Error(statusMsg, "checksum mismatch", "module", moduleVersion.Spec.ModuleConfig.Name, "moduleVersion", moduleVersion.Spec.Version)

		moduleVersion.Status.SyncStatus = statusMsg.Error()
		moduleVersion.Status.Synced = false
		err = r.Status().Update(ctx, moduleVersion)
		return ctrl.Result{
			RequeueAfter: time.Duration(30 * time.Second),
		}, err
	}

	moduleVersion.Spec.Checksum = checksum

	if moduleVersion.Spec.ModuleConfig.StorageConfig.S3 != nil {
		amazonS3Storage := &storage.AmazonS3Storage{}
		err := amazonS3Storage.New(ctx, moduleVersion)
		if err != nil {
			r.Log.Error(err, "unable to create S3 client",
				"module", moduleVersion.Spec.ModuleConfig.Name,
				"moduleVersion", moduleVersion.Spec.Version,
				"bucket", moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Bucket,
			)
		}

		storageInterface = amazonS3Storage
		moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Key = fmt.Sprintf("%s/%s", moduleVersion.Spec.ModuleConfig.Name, *moduleVersion.Spec.FileName)

		r.Log.Info("updating module version in S3 bucket",
			"module", moduleVersion.Spec.ModuleConfig.Name,
			"moduleVersion", moduleVersion.Spec.Version,
			"bucketKey", moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Key,
		)

		err = storageInterface.PutObject(ctx, moduleBytes, moduleVersion)
		if err != nil {
			r.Log.Error(err, "failed to put module version in S3 bucket",
				"module", moduleVersion.Spec.ModuleConfig.Name,
				"moduleVersion", moduleVersion.Spec.Version,
				"bucketKey", moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Key,
			)

			return ctrl.Result{
				RequeueAfter: time.Duration(30 * time.Second),
			}, nil
		}

		r.Log.Info("successfully put module version to S3 bucket",
			"module", moduleVersion.Spec.ModuleConfig.Name,
			"moduleVersion", moduleVersion.Spec.Version,
			"bucketKey", moduleVersion.Spec.ModuleConfig.StorageConfig.S3.Key,
		)

		moduleVersion.Status.Synced = true
		moduleVersion.Status.SyncStatus = "successfully synced module version"

		err = r.Status().Update(ctx, moduleVersion)
		if err != nil {
			r.Log.Error(err, "failed to update module version status",
				"module", moduleVersion.Spec.ModuleConfig.Name,
				"moduleVersion", moduleVersion.Spec.Version,
			)

			return ctrl.Result{}, err
		}
	}

	moduleVersion.Status.Synced = true
	moduleVersion.Status.SyncStatus = "successfully synced module"
	err = r.Status().Update(ctx, moduleVersion, &client.SubResourceUpdateOptions{
		UpdateOptions: client.UpdateOptions{
			FieldManager: kerraregControllerName,
		},
	})

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KerraregReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&versionv1alpha1.ModuleVersion{}).
		Named(kerraregControllerName).
		Complete(r)
}

func (r *KerraregReconciler) GetGithubApplicationSecret(ctx context.Context, moduleVersion *versionv1alpha1.ModuleVersion) (*kerraregGithub.GithubClientConfig, error) {
	object := client.ObjectKey{
		Name:      kerraregGithubSecretName,
		Namespace: moduleVersion.Namespace,
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
