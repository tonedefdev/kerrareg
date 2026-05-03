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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
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
})
