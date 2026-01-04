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
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kerrareg/api/types"
	modulev1alpha1 "kerrareg/services/module/api/v1alpha1"
	versionv1alpha1 "kerrareg/services/version/api/v1alpha1"
)

const (
	kerraregControllerName = "kerrareg-modules-controller"
	versionType            = "Module"
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

// +kubebuilder:rbac:groups=kerrareg.io,resources=modules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules/finalizers,verbs=update
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions/finalizers,verbs=update

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KerraregReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	module := &modulev1alpha1.Module{}
	err := r.Get(ctx, req.NamespacedName, module)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Module resource not found. Ignoring since object must be deleted", "module", req.Name)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get Module", "module", req.Name)
		return ctrl.Result{}, err
	}

	// If ForceSync is false and module is already synced do not requeue
	if !module.Spec.ForceSync && module.Status.Synced {
		r.Log.V(5).Info("Module is already synced", "module", module.Name)
		return ctrl.Result{}, nil
	}

	totalExpectedVersions := len(module.Spec.Versions)
	moduleVersionRefs := make(map[string]types.ModuleVersion)
	moduleName := GetModuleName(module)
	for _, version := range module.Spec.Versions {
		r.Log.V(5).Info("Processing version", "moduleVersion", version.Version, "module", module.Name)
		sanitizedModuleVersion := SanitizeModuleVersion(version.Version)
		moduleVersionName := GetModuleVersionName(module, sanitizedModuleVersion)

		object := client.ObjectKey{
			Name:      moduleVersionName,
			Namespace: module.Namespace,
		}

		moduleVersion := &versionv1alpha1.Version{}
		err = r.Get(ctx, object, moduleVersion)
		// The module version was not found so create it
		if err != nil {
			r.Log.Info(
				"Module version not found: creating module version",
				"moduleVersion", version.Version,
				"module", module.Name,
			)

			moduleVersionFileName, err := GenerateFileName(module)
			if err != nil {
				r.Log.Error(err, "Unable to generate filename",
					"moduleVersion", version.Version,
					"module", module.Name,
				)
				return ctrl.Result{}, err
			}

			moduleVersion := &versionv1alpha1.Version{
				ObjectMeta: v1.ObjectMeta{
					Name:      object.Name,
					Namespace: object.Namespace,
				},
				Spec: versionv1alpha1.VersionSpec{
					FileName:        moduleVersionFileName,
					Type:            versionType,
					Version:         version.Version,
					ModuleConfigRef: module.Spec.ModuleConfig,
				},
			}

			moduleVersion.Spec.ModuleConfigRef.Name = moduleName

			if module.Spec.ModuleConfig.GithubClientConfig.UseAuthenticatedClient {
				moduleVersion.Spec.ModuleConfigRef.GithubClientConfig.UseAuthenticatedClient = true
			}

			if module.Spec.ModuleConfig.StorageConfig.S3 != nil {
				moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3 = &types.AmazonS3Config{
					Bucket: module.Spec.ModuleConfig.StorageConfig.S3.Bucket,
					Region: module.Spec.ModuleConfig.StorageConfig.S3.Region,
				}
			}

			err = r.Create(ctx, moduleVersion, &client.CreateOptions{
				FieldManager: kerraregControllerName,
			})
			if err != nil {
				r.Log.Error(err, "Unable to create new module version", "module", module.Name, "moduleVersion", version.Version)
				return ctrl.Result{}, err
			}

			moduleVersionRef := types.ModuleVersion{
				Name:     moduleVersionName,
				FileName: *moduleVersion.Spec.FileName,
			}
			moduleVersionRefs[version.Version] = moduleVersionRef

			r.Log.Info("Successfully reconciled module version",
				"moduleVersion", moduleVersion.Spec.Version,
				"module", module.Name,
			)
		} else {
			// The module version already exists so reconcile it
			r.Log.Info(
				"Module version found: reconciling based on its config",
				"moduleVersion", version.Version,
				"module", module.Name,
			)

			moduleVersion.Spec.ModuleConfigRef.Name = moduleName

			if module.Spec.ModuleConfig.GithubClientConfig != nil {
				moduleVersion.Spec.ModuleConfigRef.GithubClientConfig = module.Spec.ModuleConfig.GithubClientConfig
			}

			if module.Spec.ModuleConfig.StorageConfig.S3 != nil {
				moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3 = module.Spec.ModuleConfig.StorageConfig.S3

				err = r.Update(ctx, moduleVersion)
				if err != nil {
					r.Log.Error(err, "Failed to update module version",
						"moduleVersion", moduleVersion.Spec.Version,
						"module", module.Name,
					)
					continue
				}

				moduleVersionRef := types.ModuleVersion{
					Name:     moduleVersionName,
					FileName: *moduleVersion.Spec.FileName,
				}
				moduleVersionRefs[version.Version] = moduleVersionRef

				r.Log.Info("Successfully reconciled module version",
					"moduleVersion", moduleVersion.Spec.Version,
					"module", module.Name,
				)
			}
		}
	}

	err = r.Update(ctx, module, &client.SubResourceUpdateOptions{
		UpdateOptions: client.UpdateOptions{
			FieldManager: kerraregControllerName,
		},
	})

	// If the total number of expected Versions have not been updated
	// requeue after 30s
	actualVersionsTotal := len(moduleVersionRefs)
	if actualVersionsTotal != totalExpectedVersions {
		module.Status.Synced = false
		module.Status.SyncStatus = fmt.Sprintf("Expected number of Version resources were not created: got '%d': expected '%d': requeue in 30s", actualVersionsTotal, totalExpectedVersions)

		err = r.Status().Update(ctx, module, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{
				FieldManager: kerraregControllerName,
			},
		})

		r.Log.Info("Expected number of Version resources were not created: requeueing in 30s",
			"module", module.Name,
			"expected", totalExpectedVersions,
			"generated", actualVersionsTotal,
		)

		return ctrl.Result{
			RequeueAfter: time.Duration(30 * time.Second),
		}, nil
	}

	// If ForceSync is true set it to false
	// now that we have successfully reconciled
	if module.Spec.ForceSync {
		module.Spec.ForceSync = false
	}

	err = r.Update(ctx, module, &client.UpdateOptions{
		FieldManager: kerraregControllerName,
	})
	if err != nil {
		r.Log.Error(err, "Failed to update module",
			"module", module.Name,
		)
		return ctrl.Result{}, err
	}

	module.Status.ModuleVersionRefs = moduleVersionRefs
	module.Status.Synced = true
	module.Status.SyncStatus = "Successfully synced module"

	err = r.Status().Update(ctx, module, &client.SubResourceUpdateOptions{
		UpdateOptions: client.UpdateOptions{
			FieldManager: kerraregControllerName,
		},
	})

	r.Log.Info("Successfully reconciled module",
		"module", module.Name,
	)

	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *KerraregReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&modulev1alpha1.Module{}).
		Owns(&versionv1alpha1.Version{}).
		Named(kerraregControllerName).
		Complete(r)
}

// GenerateFileName returns a randomly generated UUID7 string that includes the module's file extension.
func GenerateFileName(module *modulev1alpha1.Module) (*string, error) {
	moduleVersionFileUUID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	moduleVersionFileName := fmt.Sprintf("%s.%s", moduleVersionFileUUID, module.Spec.ModuleConfig.FileFormat)
	return &moduleVersionFileName, nil
}

// GetModuleName returns the module name as the Module resource's name if
// the configuration field for ModuleConfig.Name is nil.
func GetModuleName(module *modulev1alpha1.Module) *string {
	var moduleName string
	if module.Spec.ModuleConfig.Name != nil {
		moduleName = *module.Spec.ModuleConfig.Name
	}

	moduleName = module.Name
	return &moduleName
}

// GetModuleName returns the module name as either the namespace of the Module object or
// from the ModuleConfig field if it's non-nil.
func GetModuleVersionName(module *modulev1alpha1.Module, sanitizedModuleVersion string) string {
	var moduleVersionName string
	if module.Spec.ModuleConfig.Name == nil {
		moduleVersionName = fmt.Sprintf("%s-%s", module.Name, sanitizedModuleVersion)
		return moduleVersionName
	}

	moduleVersionName = fmt.Sprintf("%s-%s", *module.Spec.ModuleConfig.Name, sanitizedModuleVersion)
	return moduleVersionName

}

// SanitizeModuleVersion removes leading 'v' from version strings for terraform/tofu version compatibility.
func SanitizeModuleVersion(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	return version
}
