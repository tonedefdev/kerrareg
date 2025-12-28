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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	go_github "github.com/google/go-github/v50/github"
	"github.com/google/martian/log"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	modulev1alpha1 "defdev.io/kerrareg/services/controller/api/v1alpha1"
	"defdev.io/kerrareg/services/controller/internal/github"
)

// KerraregReconciler reconciles a Kerrareg object
type KerraregReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kerrareg.io,resources=modules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=modules/finalizers,verbs=update

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KerraregReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	githubCfg := &github.GithubClientConfig{
		AppID:          2549205,
		InstallationID: 101501597,
		PrivateKeyPath: "/Users/tonedefdev/Desktop/kerrareg.2025-12-27.private-key.pem",
	}

	githubClient, err := github.GenerateGitHubClient(ctx, githubCfg)
	if err != nil {
		return ctrl.Result{
			RequeueAfter: time.Duration(15 * time.Second),
		}, err
	}

	module := &modulev1alpha1.Module{}
	err = r.Get(ctx, req.NamespacedName, module)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof("Module resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Errorf("failed to get Module: %v", err)
		return ctrl.Result{}, err
	}

	log.Infof("Reconciling Module %s/%s", module.Namespace, module.Name)
	for _, moduleVersion := range module.Spec.Versions {
		log.Infof("Processing version: %s", moduleVersion.Version)

		al, alResp, err := githubClient.Repositories.GetArchiveLink(ctx, module.Namespace, module.Name, go_github.Zipball, &go_github.RepositoryContentGetOptions{
			Ref: moduleVersion.Version,
		}, true)

		if alResp.StatusCode != 302 {
			log.Errorf("failed to get GitHub archive link: status code %d", alResp.StatusCode)
			return ctrl.Result{
				RequeueAfter: time.Duration(30 * time.Second),
			}, nil
		}

		log.Errorf("received GitHub archive link: %s", al.String())

		moduleReq, err := http.Get(al.String())
		if err != nil {
			log.Errorf("failed to create HTTP request for archive link: %v", err)
			return ctrl.Result{
				RequeueAfter: time.Duration(30 * time.Second),
			}, nil
		}
		defer moduleReq.Body.Close()

		moduleBytes, err := io.ReadAll(moduleReq.Body)
		if err != nil {
			log.Errorf("failed to read module archive data: %v", err)
			return ctrl.Result{
				RequeueAfter: time.Duration(30 * time.Second),
			}, nil
		}

		sanitizedModuleVersion := sanitizeModuleVersion(moduleVersion.Version)
		if module.Spec.StorageConfig.S3.Bucket != "" {
			log.Errorf("uploading module version '%s' to S3 bucket '%s'", sanitizedModuleVersion, module.Spec.StorageConfig.S3.Bucket)
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(module.Spec.StorageConfig.S3.Region))
			if err != nil {
				log.Errorf("unable to load SDK config, %v", err)
			}

			s3Client := s3.NewFromConfig(cfg)
			_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: &module.Spec.StorageConfig.S3.Bucket,
				Key:    aws.String(fmt.Sprintf("%s-%s.zip", module.Name, sanitizedModuleVersion)),
				Body:   bytes.NewReader(moduleBytes),
			})
			if err != nil {
				log.Errorf("failed to upload module to S3: %v", err)
				return ctrl.Result{
					RequeueAfter: time.Duration(30 * time.Second),
				}, nil
			}
			log.Errorf("successfully uploaded module version %s to S3 bucket %s", sanitizedModuleVersion, module.Spec.StorageConfig.S3.Bucket)
			continue
		}
	}

	return ctrl.Result{}, nil
}

// sanitizeModuleVersion removes leading 'v' from version strings for terraform version compatibility
func sanitizeModuleVersion(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	return version
}

// SetupWithManager sets up the controller with the Manager.
func (r *KerraregReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&modulev1alpha1.Module{}).
		Named("kerrareg").
		Complete(r)
}
