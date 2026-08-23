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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/registry"
)

var _ = Describe("Depot Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		depot := &opendepotv1alpha1.Depot{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Depot")
			err := k8sClient.Get(ctx, typeNamespacedName, depot)
			if err != nil && errors.IsNotFound(err) {
				resource := &opendepotv1alpha1.Depot{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &opendepotv1alpha1.Depot{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Depot")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &DepotReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})

		It("uses and preserves the configured provider upstream registry", func() {
			providerName := "null"
			providerNamespace := "hashicorp"
			upstreamRegistry := opendepotv1alpha1.TerraformRegistryHost
			listProviderVersions = func(_ context.Context, registryHost, namespace, name string) ([]string, error) {
				Expect(registryHost).To(Equal(opendepotv1alpha1.TerraformRegistryHost))
				Expect(namespace).To(Equal(providerNamespace))
				Expect(name).To(Equal(providerName))

				return []string{"3.2.4"}, nil
			}
			DeferCleanup(func() {
				listProviderVersions = registry.ListProviderVersions
			})

			current := &opendepotv1alpha1.Depot{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, current)).To(Succeed())
			current.Spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{{
				Name:               &providerName,
				Namespace:          &providerNamespace,
				UpstreamRegistry:   &upstreamRegistry,
				OperatingSystems:   []string{"linux"},
				Architectures:      []string{"amd64"},
				VersionConstraints: ">= 3.0.0",
			}}
			Expect(k8sClient.Update(ctx, current)).To(Succeed())

			controllerReconciler := &DepotReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			provider := &opendepotv1alpha1.Provider{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: providerName, Namespace: "default"}, provider)).To(Succeed())
			Expect(provider.Spec.ProviderConfig.UpstreamRegistry).NotTo(BeNil())
			Expect(*provider.Spec.ProviderConfig.UpstreamRegistry).To(Equal(opendepotv1alpha1.TerraformRegistryHost))
			Expect(provider.Spec.Versions).To(Equal([]opendepotv1alpha1.ProviderVersion{{Version: "3.2.4"}}))
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, provider)
			})
		})
	})
})
