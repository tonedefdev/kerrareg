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

	"github.com/go-logr/logr"
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

	kerraregv1alpha1 "github.com/tonedefdev/kerrareg/api/v1alpha1"
	"github.com/tonedefdev/kerrareg/pkg/utils"
)

const (
	kerraregControllerName = "kerrareg-providers-controller"
)

// KerraregReconciler reconciles a Provider object.
type KerraregReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kerrareg.io,resources=providers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=providers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=providers/finalizers,verbs=update
// +kubebuilder:rbac:groups=kerrareg.io,resources=versions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=versions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=versions/finalizers,verbs=update

func (r *KerraregReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	provider := &kerraregv1alpha1.Provider{}
	if err := r.Get(ctx, req.NamespacedName, provider); err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(5).Info("Provider resource not found. Ignoring since object must be deleted", "provider", req.Name)
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get Provider", "provider", req.Name)
		return ctrl.Result{}, err
	}

	providerName, err := utils.GetName(nil, provider)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(provider.Spec.ProviderConfig.OperatingSystems) == 0 {
		return ctrl.Result{}, fmt.Errorf("the provider operatingSystems field cannot be empty")
	}

	if len(provider.Spec.ProviderConfig.Architectures) == 0 {
		return ctrl.Result{}, fmt.Errorf("the provider architectures field cannot be empty")
	}

	desiredVersionNames := map[string]struct{}{}
	providerVersionRefs := make(map[string]*kerraregv1alpha1.ProviderVersion)

	for _, providerVersion := range provider.Spec.Versions {
		sanitizedVersion := utils.SanitizeVersion(providerVersion.Version)

		for _, osName := range provider.Spec.ProviderConfig.OperatingSystems {
			for _, arch := range provider.Spec.ProviderConfig.Architectures {
				resourceName := providerVersionResourceName(*providerName, sanitizedVersion, osName, arch)
				desiredVersionNames[resourceName] = struct{}{}

				refKey := fmt.Sprintf("%s-%s-%s", sanitizedVersion, osName, arch)
				providerVersionRefs[refKey] = &kerraregv1alpha1.ProviderVersion{
					Name:            resourceName,
					Version:         sanitizedVersion,
					OperatingSystem: osName,
					Architecture:    arch,
				}

				objectKey := client.ObjectKey{Name: resourceName, Namespace: provider.Namespace}
				existingVersion := &kerraregv1alpha1.Version{}

				if err := r.Get(ctx, objectKey, existingVersion); err != nil {
					if !errors.IsNotFound(err) {
						return ctrl.Result{}, err
					}

					newVersion, err := r.versionForProvider(provider, providerName, objectKey, sanitizedVersion, osName, arch)
					if err != nil {
						return ctrl.Result{}, err
					}

					if err = r.Create(ctx, newVersion, &client.CreateOptions{FieldManager: kerraregControllerName}); err != nil {
						return ctrl.Result{}, err
					}
					continue
				}

				updatedVersion, err := r.versionForProvider(provider, providerName, objectKey, sanitizedVersion, osName, arch)
				if err != nil {
					return ctrl.Result{}, err
				}

				updatedVersion.ObjectMeta.ResourceVersion = existingVersion.ObjectMeta.ResourceVersion
				updatedVersion.Spec.FileName = existingVersion.Spec.FileName
				if err = r.Update(ctx, updatedVersion, &client.UpdateOptions{FieldManager: kerraregControllerName}); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	}

	if provider.Spec.ForceSync {
		var currentProvider kerraregv1alpha1.Provider
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err := r.Get(ctx, req.NamespacedName, &currentProvider); err != nil {
				return err
			}

			currentProvider.Spec.ForceSync = false
			if err := r.Update(ctx, &currentProvider, &client.UpdateOptions{FieldManager: kerraregControllerName}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			r.Log.Error(err, "Failed to update Provider", "provider", provider.Name)
			return ctrl.Result{}, err
		}
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, req.NamespacedName, provider); err != nil {
			return err
		}

		provider.Status.ProviderVersionRefs = providerVersionRefs
		provider.Status.Synced = true
		provider.Status.SyncStatus = "Successfully synced provider"
		if err := r.Status().Update(ctx, provider); err != nil {
			return err
		}
		return nil
	}); err != nil {
		r.Log.Error(err, "Failed to update Provider status", "provider", provider.Name)
		return ctrl.Result{}, err
	}

	if err := r.ReconcileVersionRemovals(ctx, *provider, desiredVersionNames); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("Successfully reconciled Provider", "provider", provider.Name)
	return ctrl.Result{}, nil
}

// ReconcileVersionRemovals removes stale Version resources for a provider.
func (r *KerraregReconciler) ReconcileVersionRemovals(ctx context.Context, provider kerraregv1alpha1.Provider, desiredVersionNames map[string]struct{}) error {
	versionList := kerraregv1alpha1.VersionList{}
	labelsMap := map[string]string{
		"kerrareg.io/provider":  provider.Name,
		"kerrareg.io/namespace": provider.Namespace,
	}

	selectorString := labels.FormatLabels(labelsMap)
	labelSelector, err := labels.Parse(selectorString)
	if err != nil {
		return err
	}

	if err = r.List(ctx, &versionList, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
		return err
	}

	for i := range versionList.Items {
		version := &versionList.Items[i]
		if _, exists := desiredVersionNames[version.Name]; exists {
			continue
		}
		r.Log.Info("Deleting stale provider version", "provider", provider.ObjectMeta.Name, "version", version.Spec.Version, "name", version.Name)
		if err = r.Delete(ctx, version); err != nil {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KerraregReconciler) SetupWithManager(mgr ctrl.Manager) error {
	versionPredicates := predicate.Funcs{
		UpdateFunc:  func(e event.UpdateEvent) bool { return false },
		CreateFunc:  func(e event.CreateEvent) bool { return false },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kerraregv1alpha1.Provider{}).
		Owns(&kerraregv1alpha1.Version{}, builder.WithPredicates(versionPredicates)).
		Named(kerraregControllerName).
		Complete(r)
}

func providerVersionResourceName(providerName, version, osName, arch string) string {
	raw := fmt.Sprintf("%s-%s-%s-%s", providerName, version, osName, arch)
	raw = strings.ToLower(raw)
	raw = strings.ReplaceAll(raw, ".", "-")
	raw = strings.ReplaceAll(raw, "_", "-")
	raw = strings.ReplaceAll(raw, "/", "-")
	return raw
}

// versionForProvider creates the desired Version resource for a specific provider version/os/arch tuple.
func (r *KerraregReconciler) versionForProvider(provider *kerraregv1alpha1.Provider, providerName *string, object client.ObjectKey, version, osName, arch string) (*kerraregv1alpha1.Version, error) {
	providerVersion := &kerraregv1alpha1.Version{
		ObjectMeta: v1.ObjectMeta{
			Name:      object.Name,
			Namespace: object.Namespace,
			Labels: map[string]string{
				"kerrareg.io/provider":      *providerName,
				"kerrareg.io/namespace":     object.Namespace,
				"kerrareg.io/provider-os":   osName,
				"kerrareg.io/provider-arch": arch,
			},
		},
		Spec: kerraregv1alpha1.VersionSpec{
			Architecture:      arch,
			OperatingSystem:   osName,
			Type:              kerraregv1alpha1.KerraregProvider,
			Version:           version,
			ProviderConfigRef: &provider.Spec.ProviderConfig,
		},
	}

	if err := controllerutil.SetControllerReference(provider, providerVersion, r.Scheme); err != nil {
		return nil, err
	}

	return providerVersion, nil
}
