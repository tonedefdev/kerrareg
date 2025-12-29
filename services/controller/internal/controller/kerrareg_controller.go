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
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	go_github "github.com/google/go-github/v50/github"
	"github.com/google/martian/log"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kerrareg.io,resources=moduleversions/finalizers,verbs=update

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

		sha256Sum := sha256.Sum256(moduleBytes)
		checkSumSha256 := base64.StdEncoding.EncodeToString(sha256Sum[:])
		sanitizedModuleVersion := sanitizeModuleVersion(moduleVersion.Version)

		fileUUID, err := uuid.NewV7()
		if err != nil {
			log.Errorf("failed to generate UUID for file name: %v", err)
			return ctrl.Result{
				RequeueAfter: time.Duration(30 * time.Second),
			}, nil
		}

		kerraregVersion := &modulev1alpha1.ModuleVersion{
			ObjectMeta: v1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", module.Name, sanitizedModuleVersion),
				Namespace: module.Namespace,
			},
			Spec: modulev1alpha1.ModuleVersionSpec{
				Checksum: checkSumSha256,
				FileName: fmt.Sprintf("%s.zip", fileUUID.String()),
				Version:  sanitizedModuleVersion,
			},
		}

		if module.Spec.StorageConfig.S3.Bucket != "" {
			log.Errorf("uploading module version '%s' to S3 bucket '%s'", sanitizedModuleVersion, module.Spec.StorageConfig.S3.Bucket)
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(module.Spec.StorageConfig.S3.Region))
			if err != nil {
				log.Errorf("unable to load SDK config, %v", err)
			}

			bucketKey := fmt.Sprintf("%s/%s.zip", module.Name, fileUUID.String())

			s3Client := s3.NewFromConfig(cfg)
			_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
				ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
				ChecksumSHA256:    &checkSumSha256,
				Bucket:            &module.Spec.StorageConfig.S3.Bucket,
				Key:               &bucketKey,
				Body:              bytes.NewReader(moduleBytes),
			})
			if err != nil {
				log.Errorf("failed to upload module to S3: %v", err)
				return ctrl.Result{
					RequeueAfter: time.Duration(30 * time.Second),
				}, nil
			}

			s3Storage := modulev1alpha1.ModuleStorage{
				S3: &modulev1alpha1.AmazonS3{
					Key: bucketKey,
					Config: modulev1alpha1.AmazonS3Config{
						Bucket: module.Spec.StorageConfig.S3.Bucket,
						Region: module.Spec.StorageConfig.S3.Region,
					},
				},
			}

			kerraregVersion.Spec.Storage = s3Storage
			log.Errorf("successfully uploaded module version %s to S3 bucket %s", bucketKey, module.Spec.StorageConfig.S3.Bucket)
		}

		err = r.Create(ctx, kerraregVersion, &client.CreateOptions{
			FieldManager: "kerrareg-controller",
		})

		if err != nil {
			if errors.IsAlreadyExists(err) {
				log.Errorf("ModuleVersion %s already exists, skipping creation", sanitizedModuleVersion)
				continue
			}
			log.Errorf("failed to create ModuleVersion resource: %v", err)
			return ctrl.Result{
				RequeueAfter: time.Duration(30 * time.Second),
			}, nil
		}
	}

	return ctrl.Result{}, nil
}

// sanitizeModuleVersion removes leading 'v' from version strings for terraform/tofu version compatibility
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
		Owns(&modulev1alpha1.ModuleVersion{}).
		Named("kerrareg-controller").
		Complete(r)
}
