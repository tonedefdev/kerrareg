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
	"slices"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kerraregv1alpha1 "github.com/tonedefdev/kerrareg/api/v1alpha1"
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
	module := &kerraregv1alpha1.Module{}
	err := r.Get(ctx, req.NamespacedName, module)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(5).Info("Module resource not found. Ignoring since object must be deleted", "module", req.Name)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get Module", "module", req.Name)
		return ctrl.Result{}, err
	}

	moduleName := getModuleName(module)
	moduleVersionRefs := make(map[string]*kerraregv1alpha1.ModuleVersion)

	for _, version := range module.Spec.Versions {
		r.Log.V(5).Info("Processing version", "moduleVersion", version.Version, "module", module.Name)
		sanitizedModuleVersion := sanitizeModuleVersion(version.Version)
		moduleVersionName := getModuleVersionName(module, sanitizedModuleVersion)

		object := client.ObjectKey{
			Name:      moduleVersionName,
			Namespace: module.Namespace,
		}

		moduleVersion := &kerraregv1alpha1.Version{}
		err = r.Get(ctx, object, moduleVersion)
		// The module version was not found so create it
		if err != nil {
			r.Log.V(5).Info(
				"Module version not found: creating module version",
				"moduleVersion", version.Version,
				"module", module.Name,
			)

			moduleVersionFileName, err := generateFileName(module)
			if err != nil {
				r.Log.Error(err, "Unable to generate filename",
					"moduleVersion", version.Version,
					"module", module.Name,
				)
				return ctrl.Result{
					RequeueAfter: time.Duration(30 * time.Second),
				}, err
			}

			moduleVersionRef := &kerraregv1alpha1.ModuleVersion{
				Name:     moduleVersionName,
				FileName: moduleVersionFileName,
			}
			moduleVersionRefs[version.Version] = moduleVersionRef

			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err = r.Get(ctx, req.NamespacedName, module); err != nil {
					return err
				}

				module.Status.ModuleVersionRefs = moduleVersionRefs
				module.Status.Synced = true
				module.Status.SyncStatus = "Successfully synced module"

				if err = r.Status().Update(ctx, module); err != nil {
					return err
				}
				return nil
			}); err != nil {
				r.Log.Error(err, "Failed to update Module status",
					"module", module.Name,
				)
				return ctrl.Result{}, err
			}

			moduleVersion, err := r.versionForModule(module, moduleName, moduleVersionFileName, object, version)
			if err != nil {
				r.Log.Error(err, "Unable to register Module as owner of Version",
					"moduleVersion", version.Version,
					"module", module.Name,
				)
				return ctrl.Result{}, err
			}

			var currentModuleVersion kerraregv1alpha1.Version
			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err = r.Get(ctx, object, &currentModuleVersion); err != nil {
					return err
				}

				// There are instances where the module Version has been created already
				// so this will catch the current Version's resource version and if it's
				// not an empty string return
				if currentModuleVersion.ObjectMeta.ResourceVersion != "" {
					return nil
				}

				err = r.Create(ctx, moduleVersion, &client.CreateOptions{
					FieldManager: kerraregControllerName,
				})
				if err != nil {
					return err
				}

				return nil
			}); err != nil {
				r.Log.Error(err, "unable to create the new module Version", "moduleVersion", version.Version)
			}

			r.Log.V(5).Info("Successfully created module version",
				"moduleVersion", moduleVersion.Spec.Version,
				"module", module.Name,
			)
		} else {
			// The module version already exists so reconcile it
			r.Log.V(5).Info(
				"Module version found: reconciling based on its config",
				"moduleVersion", version.Version,
				"module", module.Name,
			)

			moduleVersionRef := &kerraregv1alpha1.ModuleVersion{
				Name:     moduleVersionName,
				FileName: moduleVersion.Spec.FileName,
			}

			updateModuleVersion, err := r.versionForModule(module, moduleName, moduleVersion.Spec.FileName, object, version)
			if err != nil {
				return reconcile.Result{}, err
			}

			updateModuleVersion.ObjectMeta.ResourceVersion = moduleVersion.ObjectMeta.ResourceVersion
			var currentModuleVersion kerraregv1alpha1.Version
			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err = r.Get(ctx, object, &currentModuleVersion); err != nil {
					return err
				}

				currentModuleVersion = *updateModuleVersion
				if err = r.Update(ctx, &currentModuleVersion); err != nil {
					return err
				}

				return nil
			}); err != nil {
				r.Log.Error(err, "Failed to update module Version",
					"moduleVersion", updateModuleVersion.Spec.Version,
					"module", module.Name,
				)
				return reconcile.Result{}, err
			}

			moduleVersionRefs[version.Version] = moduleVersionRef

			r.Log.V(5).Info("Successfully reconciled module Version",
				"moduleVersion", version.Version,
				"module", module.Name,
			)
		}
	}

	latestVersion := getLatestVersion(*module)
	if latestVersion == nil {
		return ctrl.Result{}, fmt.Errorf("latestVersion is nil: %v", module.Spec)
	}

	if module.Spec.ModuleConfig.VersionHistoryLimit != nil {
		versions := versionsToKeep(*module)
		moduleVersionsToKeep := make([]kerraregv1alpha1.ModuleVersion, len(versions))
		for _, version := range versions {
			moduleVersion := kerraregv1alpha1.ModuleVersion{
				Version: version,
			}
			moduleVersionsToKeep = append(moduleVersionsToKeep, moduleVersion)
		}
		module.Spec.Versions = moduleVersionsToKeep
	}

	// If ForceSync is true set it to false
	// now that we have successfully reconciled
	var currentModule kerraregv1alpha1.Module
	if module.Spec.ForceSync {
		currentModule.Spec.ForceSync = false

		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, &currentModule); err != nil {
				return err
			}

			if err = r.Update(ctx, &currentModule, &client.UpdateOptions{
				FieldManager: kerraregControllerName,
			}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			r.Log.Error(err, "Failed to update Module",
				"module", module.Name,
			)
			return ctrl.Result{}, err
		}
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err = r.Get(ctx, req.NamespacedName, module); err != nil {
			return err
		}

		module.Status.ModuleVersionRefs = moduleVersionRefs
		module.Status.LatestVersion = latestVersion
		module.Status.Synced = true
		module.Status.SyncStatus = "Successfully synced module"

		if err = r.Status().Update(ctx, module); err != nil {
			return err
		}
		return nil
	}); err != nil {
		r.Log.Error(err, "Failed to update Module status",
			"module", module.Name,
		)
		return ctrl.Result{}, err
	}

	if err = r.ReconcileVersionRemovals(ctx, *module); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("Successfully reconciled Module",
		"module", module.Name,
	)

	return ctrl.Result{}, nil
}

// ReconcileVersionRemovals ensures that orphaned Versions are properly removed from the cluster when
// any Version has been removed from module.Spec.Versions field.
func (r *KerraregReconciler) ReconcileVersionRemovals(ctx context.Context, module kerraregv1alpha1.Module) error {
	versionList := kerraregv1alpha1.VersionList{}
	labelsMap := map[string]string{
		"kerrareg.io/module":    module.Name,
		"kerrareg.io/namespace": module.Namespace,
	}

	selectorString := labels.FormatLabels(labelsMap)
	labelSelector, err := labels.Parse(selectorString)
	if err != nil {
		return err
	}

	if err = r.List(ctx, &versionList, &client.ListOptions{
		LabelSelector: labelSelector,
	}); err != nil {
		return err
	}

	for _, version := range versionList.Items {
		moduleVersion := kerraregv1alpha1.ModuleVersion{
			Version: version.Spec.Version,
		}
		if !slices.Contains(module.Spec.Versions, moduleVersion) {
			r.Log.Info("Deleting module version", "module", module.ObjectMeta.Name, "version", version.Spec.Version)
			if err = r.Delete(ctx, &version); err != nil {
				return err
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KerraregReconciler) SetupWithManager(mgr ctrl.Manager) error {
	versionPredicates := predicate.Funcs{
		// Do not reconcile on any events. This controller is only responsible for creating and deleting the Versions
		// when the Module spec has changed.
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},

		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},

		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},

		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kerraregv1alpha1.Module{}).
		Owns(&kerraregv1alpha1.Version{}, builder.WithPredicates(versionPredicates)).
		Named(kerraregControllerName).
		Complete(r)
}

// generateFileName returns a randomly generated UUID7 string that includes the module's file extension.
func generateFileName(module *kerraregv1alpha1.Module) (*string, error) {
	moduleVersionFileUUID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	moduleVersionFileName := fmt.Sprintf("%s.%s", moduleVersionFileUUID, *module.Spec.ModuleConfig.FileFormat)
	return &moduleVersionFileName, nil
}

// getLatestVersion returns the latest semantic version of a Module
func getLatestVersion(module kerraregv1alpha1.Module) *string {
	versions := make([]string, len(module.Spec.Versions))
	for _, version := range module.Spec.Versions {
		var semverString string
		if version.Version[0] != 'v' {
			semverString = fmt.Sprintf("v%s", version.Version)
		} else {
			semverString = version.Version
		}
		semverString = semver.Canonical(semverString)
		versions = append(versions, semverString)
	}

	semver.Sort(versions)
	latestVersion := versions[len(versions)-1]
	return &latestVersion
}

func versionsToKeep(module kerraregv1alpha1.Module) []string {
	versionHistoryLimit := *module.Spec.ModuleConfig.VersionHistoryLimit
	if module.Spec.ModuleConfig.VersionHistoryLimit == nil || versionHistoryLimit <= 0 {
		return nil
	}

	versions := make([]string, len(module.Spec.Versions))
	for _, version := range module.Spec.Versions {
		var semverString string
		if version.Version[0] != 'v' {
			semverString = fmt.Sprintf("v%s", version.Version)
		} else {
			semverString = version.Version
		}
		semverString = semver.Canonical(semverString)
		versions = append(versions, semverString)
	}

	semver.Sort(versions)
	return versions[len(versions)-versionHistoryLimit:]
}

// getModuleName returns the module name as the Module resource's name if
// the configuration field for ModuleConfig.Name is nil.
func getModuleName(module *kerraregv1alpha1.Module) *string {
	var moduleName string
	if module.Spec.ModuleConfig.Name != nil {
		moduleName = *module.Spec.ModuleConfig.Name
	}

	moduleName = module.Name
	return &moduleName
}

// GetModuleName returns the module name as either the namespace of the Module object or
// from the ModuleConfig field if it's non-nil.
func getModuleVersionName(module *kerraregv1alpha1.Module, sanitizedModuleVersion string) string {
	var moduleVersionName string
	if module.Spec.ModuleConfig.Name == nil {
		moduleVersionName = fmt.Sprintf("%s-%s", module.Name, sanitizedModuleVersion)
		return moduleVersionName
	}

	moduleVersionName = fmt.Sprintf("%s-%s", *module.Spec.ModuleConfig.Name, sanitizedModuleVersion)
	return moduleVersionName

}

// SanitizeModuleVersion removes leading 'v' from version strings for terraform/tofu version compatibility.
func sanitizeModuleVersion(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	return version
}

// versionForModule creates a new Version for a Module and sets the controller reference
func (r *KerraregReconciler) versionForModule(module *kerraregv1alpha1.Module, moduleName *string, moduleVersionFileName *string, object client.ObjectKey, version kerraregv1alpha1.ModuleVersion) (*kerraregv1alpha1.Version, error) {
	moduleVersion := &kerraregv1alpha1.Version{
		ObjectMeta: v1.ObjectMeta{
			Name:      object.Name,
			Namespace: object.Namespace,
			Labels: map[string]string{
				"kerrareg.io/module":    *moduleName,
				"kerrareg.io/namespace": object.Namespace,
			},
		},
		Spec: kerraregv1alpha1.VersionSpec{
			FileName: moduleVersionFileName,
			Type:     kerraregv1alpha1.KerraregModule,
			Version:  version.Version,
			ModuleConfigRef: &kerraregv1alpha1.ModuleConfig{
				Name: moduleName,
			},
		},
	}

	// Set the ownerRef for the Version, ensuring that the Version
	// will be reconciled on any relevant update and deletion events.
	err := controllerutil.SetControllerReference(module, moduleVersion, r.Scheme)
	return moduleVersion, err
}
