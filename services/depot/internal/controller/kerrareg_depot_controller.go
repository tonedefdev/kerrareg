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

	"github.com/go-logr/logr"
	"github.com/google/go-github/v50/github"
	"github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kerraregGithub "kerrareg/pkg/github"
	"kerrareg/services/depot/api/v1alpha1"
	depotv1alpha1 "kerrareg/services/depot/api/v1alpha1"
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
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Depot object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *DepotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var depot v1alpha1.Depot
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
		for _, module := range depot.Spec.ModuleConfigs {
			var githubClient *github.Client
			githubConfig, err := kerraregGithub.GetGithubApplicationSecret(ctx, r.Client, depot.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}

			r.Log.Info("Github client config", "config", githubConfig)

			authGithubClient, err := kerraregGithub.CreateGithubClient(ctx, module.GithubClientConfig.UseAuthenticatedClient, githubConfig)
			if err != nil {
				return ctrl.Result{}, err
			}

			r.Log.Info("Github client", "client", authGithubClient)

			githubClient = authGithubClient
			releases, resp, err := githubClient.Repositories.ListReleases(ctx, module.RepoOwner, *module.Name, &github.ListOptions{
				PerPage: 100,
			})

			if releases == nil || resp == nil {
				return ctrl.Result{}, fmt.Errorf("releases was nil")
			}

			constraints, err := version.NewConstraint(module.VersionConstraints)
			if err != nil {
				return ctrl.Result{}, err
			}

			var matchedVersions []string
			for _, release := range releases {
				r.Log.Info("Release tag name", "tagName", release.TagName)

				for _, constraint := range constraints {
					version, err := version.NewVersion(*release.TagName)
					if err != nil {
						r.Log.Error(err, "Unable to create new go-version")
						return ctrl.Result{}, err
					}

					if constraint.Check(version) {
						matchedVersions = append(matchedVersions, version.String())
					}
				}
			}

			r.Log.Info("Matched versions", "versions", matchedVersions)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DepotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&depotv1alpha1.Depot{}).
		Named("depot").
		Complete(r)
}
