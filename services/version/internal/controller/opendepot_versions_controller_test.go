/*
Copyright 2026 Tony Owens.

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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/registry"
)

var _ = Describe("Version Controller", func() {
	ctx := context.Background()

	Context("Reconcile", func() {
		It("should return an error when the Version type is not recognized", func() {
			const resourceName = "test-unrecognized-type"
			namespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

			resource := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  "default",
					Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
				},
				Spec: opendepotv1alpha1.VersionSpec{
					Type:    "UnrecognizedType",
					Version: "1.0.0",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				current := &opendepotv1alpha1.Version{}
				if err := k8sClient.Get(ctx, namespacedName, current); err == nil {
					current.Finalizers = nil
					_ = k8sClient.Update(ctx, current)
					_ = k8sClient.Delete(ctx, current)
				}
			})

			reconciler := &VersionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Log:    logr.Discard(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no usable type provided"))
		})

		It("should return an error when a Provider Version is missing providerConfigRef", func() {
			const resourceName = "test-provider-no-ref"
			namespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

			resource := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  "default",
					Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
				},
				Spec: opendepotv1alpha1.VersionSpec{
					Type:    opendepotv1alpha1.OpenDepotProvider,
					Version: "1.0.0",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				current := &opendepotv1alpha1.Version{}
				if err := k8sClient.Get(ctx, namespacedName, current); err == nil {
					current.Finalizers = nil
					_ = k8sClient.Update(ctx, current)
					_ = k8sClient.Delete(ctx, current)
				}
			})

			reconciler := &VersionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Log:    logr.Discard(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("providerConfigRef is required"))
		})

		It("should return an error when a Module Version is missing moduleConfigRef name", func() {
			const resourceName = "test-module-no-ref"
			namespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

			resource := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  "default",
					Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
				},
				Spec: opendepotv1alpha1.VersionSpec{
					Type:    opendepotv1alpha1.OpenDepotModule,
					Version: "1.0.0",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				current := &opendepotv1alpha1.Version{}
				if err := k8sClient.Get(ctx, namespacedName, current); err == nil {
					current.Finalizers = nil
					_ = k8sClient.Update(ctx, current)
					_ = k8sClient.Delete(ctx, current)
				}
			})

			reconciler := &VersionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Log:    logr.Discard(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("moduleConfigRef is required"))
		})
	})

	Context("Provider origins", func() {
		It("redacts credentials from provider download errors", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}))
			DeferCleanup(server.Close)

			requestURL := server.URL + "/provider.zip?X-Amz-Credential=secret#fragment"
			_, _, cleanup, err := httpStreamToFile(ctx, requestURL)
			DeferCleanup(cleanup)
			Expect(err).To(MatchError(fmt.Sprintf("request to '%s/provider.zip' failed with status 403", server.URL)))
			Expect(err.Error()).NotTo(ContainSubstring("secret"))
			Expect(err.Error()).NotTo(ContainSubstring("X-Amz-Credential"))
		})

		It("redacts URL userinfo, query parameters, and fragments", func() {
			Expect(redactedURL("https://user:password@example.com/provider.zip?token=secret#fragment")).To(
				Equal("https://example.com/provider.zip"),
			)
			Expect(redactedURL("https://%zz")).To(Equal("<invalid URL>"))
		})

		It("uses the configured registry for provider archive lookup", func() {
			archive := []byte("provider archive")
			archiveSum := sha256.Sum256(archive)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(archive)
			}))
			DeferCleanup(server.Close)

			lookupProviderDownload = func(_ context.Context, registryHost, namespace, name, version, operatingSystem, architecture string) (*registry.ProviderDownload, error) {
				Expect(registryHost).To(Equal(opendepotv1alpha1.TerraformRegistryHost))
				Expect(namespace).To(Equal("hashicorp"))
				Expect(name).To(Equal("null"))
				Expect(version).To(Equal("3.2.4"))
				Expect(operatingSystem).To(Equal("linux"))
				Expect(architecture).To(Equal("arm64"))

				return &registry.ProviderDownload{
					DownloadURL: server.URL + "/terraform-provider-null.zip",
					Filename:    "terraform-provider-null.zip",
					Shasum:      hex.EncodeToString(archiveSum[:]),
				}, nil
			}
			DeferCleanup(func() {
				lookupProviderDownload = registry.LookupProviderDownload
			})

			providerName := "null"
			upstreamRegistry := opendepotv1alpha1.TerraformRegistryHost
			version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				Version:         "3.2.4",
				OperatingSystem: "linux",
				Architecture:    "arm64",
				ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{
					Name:             &providerName,
					UpstreamRegistry: &upstreamRegistry,
				},
			}}
			reconciler := &VersionReconciler{Log: logr.Discard()}
			archivePath, cleanup, _, fileName, err := reconciler.fetchProviderArchive(ctx, version)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(cleanup)
			Expect(fileName).NotTo(BeNil())
			Expect(*fileName).To(Equal("terraform-provider-null.zip"))
			Expect(os.ReadFile(archivePath)).To(Equal(archive))
		})

		It("skips the OpenTofu docs lookup for Terraform providers", func() {
			lookupCalled := false
			lookupProviderRepo = func(context.Context, string, string) (string, error) {
				lookupCalled = true

				return "https://github.com/wrong/repository", nil
			}
			DeferCleanup(func() {
				lookupProviderRepo = registry.LookupProviderRepo
			})

			upstreamRegistry := opendepotv1alpha1.TerraformRegistryHost
			repository := resolveProviderSourceRepository(ctx, "hashicorp", "null", &opendepotv1alpha1.ProviderConfig{
				UpstreamRegistry: &upstreamRegistry,
			})
			Expect(lookupCalled).To(BeFalse())
			Expect(repository).To(Equal("https://github.com/hashicorp/terraform-provider-null"))
		})
	})
})
