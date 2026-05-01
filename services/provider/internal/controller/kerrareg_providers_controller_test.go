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
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kerraregv1alpha1 "github.com/tonedefdev/kerrareg/api/v1alpha1"
)

var _ = Describe("Provider Controller", func() {
	const testNamespace = "default"

	var reconciler *KerraregReconciler

	BeforeEach(func() {
		reconciler = &KerraregReconciler{
			Client: k8sClient,
			Log:    logr.Discard(),
			Scheme: k8sClient.Scheme(),
		}
	})

	AfterEach(func() {
		// Clean up all Version resources created during the test.
		versionList := &kerraregv1alpha1.VersionList{}
		_ = k8sClient.List(ctx, versionList, client.InNamespace(testNamespace))
		for i := range versionList.Items {
			_ = k8sClient.Delete(ctx, &versionList.Items[i])
		}
	})

	Context("When reconciling a provider resource", func() {
		It("creates Version CRs for each version/os/arch combination", func() {
			providerName := "happy-path-provider"
			namespacedName := types.NamespacedName{Name: providerName, Namespace: testNamespace}

			provider := &kerraregv1alpha1.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      providerName,
					Namespace: testNamespace,
				},
				Spec: kerraregv1alpha1.ProviderSpec{
					ProviderConfig: kerraregv1alpha1.ProviderConfig{
						OperatingSystems: []string{"linux", "darwin"},
						Architectures:    []string{"amd64", "arm64"},
					},
					Versions: []kerraregv1alpha1.ProviderVersion{
						{Version: "1.0.0"},
						{Version: "2.0.0"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, provider)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, provider)
			})

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			// 2 versions × 2 OS × 2 arch = 8 Version CRs
			versionList := &kerraregv1alpha1.VersionList{}
			Expect(k8sClient.List(ctx, versionList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{"kerrareg.io/provider": providerName},
			)).To(Succeed())
			Expect(versionList.Items).To(HaveLen(8))

			// Verify labels on a specific version
			expectedName := "happy-path-provider-1-0-0-linux-amd64"
			var found *kerraregv1alpha1.Version
			for i := range versionList.Items {
				if versionList.Items[i].Name == expectedName {
					found = &versionList.Items[i]
					break
				}
			}
			Expect(found).NotTo(BeNil(), "expected Version CR %q to exist", expectedName)
			Expect(found.Labels["kerrareg.io/provider-os"]).To(Equal("linux"))
			Expect(found.Labels["kerrareg.io/provider-arch"]).To(Equal("amd64"))
			Expect(found.Spec.Version).To(Equal("1.0.0"))
			Expect(found.Spec.OperatingSystem).To(Equal("linux"))
			Expect(found.Spec.Architecture).To(Equal("amd64"))
			Expect(found.Spec.Type).To(Equal(kerraregv1alpha1.KerraregProvider))

			// Verify status was updated
			updated := &kerraregv1alpha1.Provider{}
			Expect(k8sClient.Get(ctx, namespacedName, updated)).To(Succeed())
			Expect(updated.Status.Synced).To(BeTrue())
			Expect(updated.Status.SyncStatus).To(Equal("Successfully synced provider"))
			Expect(updated.Status.ProviderVersionRefs).To(HaveLen(8))
			Expect(updated.Status.ProviderVersionRefs["1.0.0/linux/amd64"]).NotTo(BeNil())
			Expect(updated.Status.ProviderVersionRefs["2.0.0/darwin/arm64"]).NotTo(BeNil())
		})

		It("returns an error when operatingSystems is empty", func() {
			providerName := "no-os-provider"
			namespacedName := types.NamespacedName{Name: providerName, Namespace: testNamespace}

			provider := &kerraregv1alpha1.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      providerName,
					Namespace: testNamespace,
				},
				Spec: kerraregv1alpha1.ProviderSpec{
					ProviderConfig: kerraregv1alpha1.ProviderConfig{
						OperatingSystems: []string{},
						Architectures:    []string{"amd64"},
					},
					Versions: []kerraregv1alpha1.ProviderVersion{
						{Version: "1.0.0"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, provider)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, provider)
			})

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("operatingSystems"))
		})

		It("returns an error when architectures is empty", func() {
			providerName := "no-arch-provider"
			namespacedName := types.NamespacedName{Name: providerName, Namespace: testNamespace}

			provider := &kerraregv1alpha1.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      providerName,
					Namespace: testNamespace,
				},
				Spec: kerraregv1alpha1.ProviderSpec{
					ProviderConfig: kerraregv1alpha1.ProviderConfig{
						OperatingSystems: []string{"linux"},
						Architectures:    []string{},
					},
					Versions: []kerraregv1alpha1.ProviderVersion{
						{Version: "1.0.0"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, provider)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, provider)
			})

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("architectures"))
		})

		It("resets ForceSync to false after reconciliation", func() {
			providerName := "force-sync-provider"
			namespacedName := types.NamespacedName{Name: providerName, Namespace: testNamespace}

			provider := &kerraregv1alpha1.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      providerName,
					Namespace: testNamespace,
				},
				Spec: kerraregv1alpha1.ProviderSpec{
					ForceSync: true,
					ProviderConfig: kerraregv1alpha1.ProviderConfig{
						OperatingSystems: []string{"linux"},
						Architectures:    []string{"amd64"},
					},
					Versions: []kerraregv1alpha1.ProviderVersion{
						{Version: "1.0.0"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, provider)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, provider)
			})

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			updated := &kerraregv1alpha1.Provider{}
			Expect(k8sClient.Get(ctx, namespacedName, updated)).To(Succeed())
			Expect(updated.Spec.ForceSync).To(BeFalse())
		})

		It("removes stale Version CRs when a version is removed from the spec", func() {
			providerName := "orphan-cleanup-provider"
			namespacedName := types.NamespacedName{Name: providerName, Namespace: testNamespace}

			provider := &kerraregv1alpha1.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      providerName,
					Namespace: testNamespace,
				},
				Spec: kerraregv1alpha1.ProviderSpec{
					ProviderConfig: kerraregv1alpha1.ProviderConfig{
						OperatingSystems: []string{"linux"},
						Architectures:    []string{"amd64"},
					},
					Versions: []kerraregv1alpha1.ProviderVersion{
						{Version: "1.0.0"},
						{Version: "2.0.0"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, provider)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, provider)
			})

			// First reconcile creates 2 Version CRs (1 per version)
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			versionList := &kerraregv1alpha1.VersionList{}
			Expect(k8sClient.List(ctx, versionList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{"kerrareg.io/provider": providerName},
			)).To(Succeed())
			Expect(versionList.Items).To(HaveLen(2))

			// Remove version 2.0.0 from spec
			current := &kerraregv1alpha1.Provider{}
			Expect(k8sClient.Get(ctx, namespacedName, current)).To(Succeed())
			current.Spec.Versions = []kerraregv1alpha1.ProviderVersion{{Version: "1.0.0"}}
			Expect(k8sClient.Update(ctx, current)).To(Succeed())

			// Second reconcile should delete the stale 2.0.0 Version CR
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify that version 2.0.0 Version CR was deleted
			staleName := "orphan-cleanup-provider-2-0-0-linux-amd64"
			staleVersion := &kerraregv1alpha1.Version{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: staleName, Namespace: testNamespace}, staleVersion)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "expected stale Version CR %q to be deleted", staleName)

			// Verify remaining version 1.0.0 Version CR still exists
			remainingList := &kerraregv1alpha1.VersionList{}
			Expect(k8sClient.List(ctx, remainingList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{"kerrareg.io/provider": providerName},
			)).To(Succeed())
			Expect(remainingList.Items).To(HaveLen(1))
			Expect(remainingList.Items[0].Name).To(Equal("orphan-cleanup-provider-1-0-0-linux-amd64"))
		})
	})
})
