/*
Copyright 2026 Anthony Owens.

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
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	"github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kerraregv1alpha1 "github.com/tonedefdev/kerrareg/api/v1alpha1"
	kerraregGithub "github.com/tonedefdev/kerrareg/pkg/github"
)

// Depot reconciles a Depot object
type DepotReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kerrareg.io,resources=depots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=depots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=depots/finalizers,verbs=update
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *DepotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var depot kerraregv1alpha1.Depot
	err := r.Get(ctx, req.NamespacedName, &depot)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(5).Info("Depot resource not found. Ignoring since object must be deleted", "depot", req.Name)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get Depot", "depot", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info(
		"Depot found: starting reconciliation",
		"depot", depot.ObjectMeta.Name,
	)

	if len(depot.Spec.ModuleConfigs) > 0 {
		for _, moduleConfig := range depot.Spec.ModuleConfigs {
			// Set global configs if not set on module config
			if moduleConfig.StorageConfig == nil {
				moduleConfig.StorageConfig = depot.Spec.GlobalConfig.StorageConfig
			}

			if moduleConfig.GithubClientConfig == nil {
				moduleConfig.GithubClientConfig = depot.Spec.GlobalConfig.GithubClientConfig
			}

			if moduleConfig.FileFormat == nil {
				moduleConfig.FileFormat = depot.Spec.GlobalConfig.ModuleConfig.FileFormat
			}

			if moduleConfig.Immutable == nil {
				moduleConfig.Immutable = depot.Spec.GlobalConfig.ModuleConfig.Immutable
			}

			if moduleConfig.RepoUrl == nil {
				repoUrl := fmt.Sprintf("https://github.com/%s/%s", moduleConfig.RepoOwner, *moduleConfig.Name)
				moduleConfig.RepoUrl = &repoUrl
			}

			module := kerraregv1alpha1.Module{
				ObjectMeta: v1.ObjectMeta{
					Name:      *moduleConfig.Name,
					Namespace: req.Namespace,
				},
				Spec: kerraregv1alpha1.ModuleSpec{
					ModuleConfig: moduleConfig,
				},
			}

			moduleObject := client.ObjectKey{
				Name:      module.ObjectMeta.Name,
				Namespace: module.ObjectMeta.Namespace,
			}

			var githubClient *github.Client
			githubConfig, err := kerraregGithub.GetGithubApplicationSecret(ctx, r.Client, depot.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}

			authGithubClient, err := kerraregGithub.CreateGithubClient(ctx, module.Spec.ModuleConfig.GithubClientConfig.UseAuthenticatedClient, githubConfig)
			if err != nil {
				return ctrl.Result{}, err
			}

			githubClient = authGithubClient
			opt := &github.ListOptions{
				Page:    1,
				PerPage: 100,
			}

			constraints, err := version.NewConstraint(moduleConfig.VersionConstraints)
			if err != nil {
				return ctrl.Result{}, err
			}

			var matchedVersions []string
			for {
				releases, resp, err := githubClient.Repositories.ListReleases(ctx, moduleConfig.RepoOwner, *moduleConfig.Name, opt)
				if err != nil {
					return ctrl.Result{}, err
				}

				if releases == nil || resp == nil {
					return ctrl.Result{}, fmt.Errorf("releases was nil")
				}

				for _, constraint := range constraints {
					for _, release := range releases {
						version, err := version.NewVersion(*release.TagName)
						if err != nil {
							r.Log.Error(err, "Unable to create new go-version")
							return ctrl.Result{}, err
						}

						constraintString := strings.TrimSpace(constraint.String())
						if constraint.Check(version) {
							if strings.HasPrefix(constraintString, `>=`) || strings.HasPrefix(constraintString, `<=`) || strings.HasPrefix(constraintString, `~>`) {
								if slices.Contains(matchedVersions, version.String()) {
									continue
								}

								matchedVersions = append(matchedVersions, version.String())
							}
						} else {
							if strings.HasPrefix(constraintString, `!=`) {
								if !slices.Contains(matchedVersions, version.String()) {
									continue
								}

								i := slices.Index(matchedVersions, version.String())
								if i == -1 {
									continue
								}

								matchedVersions = slices.Delete(matchedVersions, i, i+1)
								break
							}
						}
					}
				}

				if resp.NextPage == 0 {
					break
				}

				opt.Page = resp.NextPage
			}

			r.Log.Info("Matched versions for module", "module", moduleConfig.Name, "versions", matchedVersions)

			var versions []kerraregv1alpha1.ModuleVersion
			for _, version := range matchedVersions {
				moduleVersion := kerraregv1alpha1.ModuleVersion{
					Version: version,
				}
				versions = append(versions, moduleVersion)
			}

			module.Spec.Versions = versions

			var currentModule kerraregv1alpha1.Module
			err = r.Get(ctx, moduleObject, &currentModule)
			if err != nil {
				if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					if err := r.Create(ctx, &module); err != nil {
						return err
					}
					return nil
				}); err != nil {
					return ctrl.Result{}, err
				}
			}

			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				currentModule.Spec.ModuleConfig = moduleConfig
				currentModule.Spec.Versions = module.Spec.Versions
				if err := r.Update(ctx, &currentModule); err != nil {
					return err
				}
				return nil
			}); err != nil {
				return ctrl.Result{}, err
			}

		}
	}

	if depot.Spec.PollingIntervalMinutes != nil {
		return ctrl.Result{RequeueAfter: time.Duration(*depot.Spec.PollingIntervalMinutes) * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DepotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kerraregv1alpha1.Depot{}).
		Named("depot").
		Complete(r)
}
