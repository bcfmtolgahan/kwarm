/*
Copyright 2026.

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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kwarmv1alpha1 "github.com/bcfmtolgahan/kwarm/api/v1alpha1"
	"github.com/bcfmtolgahan/kwarm/internal/ratelimiter"
)

var _ = Describe("PrePullImage Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-prepullimage"
		const namespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind PrePullImage")
			prepullimage := &kwarmv1alpha1.PrePullImage{}
			err := k8sClient.Get(ctx, typeNamespacedName, prepullimage)
			if err != nil && errors.IsNotFound(err) {
				resource := &kwarmv1alpha1.PrePullImage{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: namespace,
					},
					Spec: kwarmv1alpha1.PrePullImageSpec{
						Image: "nginx:1.25",
						Nodes: kwarmv1alpha1.NodeTargetSelector{
							Names: []string{"node-1", "node-2"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
						},
						Retry: kwarmv1alpha1.RetryPolicy{
							MaxAttempts:    3,
							BackoffSeconds: 30,
						},
						Timeout: "10m",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &kwarmv1alpha1.PrePullImage{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance PrePullImage")
				// Remove finalizer if present
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should add finalizer on first reconcile", func() {
			By("Reconciling the created resource")
			rateLimiter := ratelimiter.NewRateLimiter(10, 2)
			controllerReconciler := &PrePullImageReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				RateLimiter: rateLimiter,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer was added
			var ppi kwarmv1alpha1.PrePullImage
			err = k8sClient.Get(ctx, typeNamespacedName, &ppi)
			Expect(err).NotTo(HaveOccurred())
			Expect(ppi.Finalizers).To(ContainElement(FinalizerName))
		})

		It("should transition to InProgress phase", func() {
			By("Reconciling multiple times")
			rateLimiter := ratelimiter.NewRateLimiter(10, 2)
			controllerReconciler := &PrePullImageReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				RateLimiter: rateLimiter,
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile processes Pending phase
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify status
			var ppi kwarmv1alpha1.PrePullImage
			err = k8sClient.Get(ctx, typeNamespacedName, &ppi)
			Expect(err).NotTo(HaveOccurred())
			Expect(ppi.Status.Phase).To(Equal(kwarmv1alpha1.PrePullImagePhaseInProgress))
			Expect(ppi.Status.Summary.Total).To(Equal(int32(2)))
		})

		It("should initialize node status", func() {
			By("Reconciling the resource")
			rateLimiter := ratelimiter.NewRateLimiter(10, 2)
			controllerReconciler := &PrePullImageReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				RateLimiter: rateLimiter,
			}

			// Reconcile twice to get past finalizer addition
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify node status
			var ppi kwarmv1alpha1.PrePullImage
			err = k8sClient.Get(ctx, typeNamespacedName, &ppi)
			Expect(err).NotTo(HaveOccurred())
			Expect(ppi.Status.NodeStatus).To(HaveLen(2))

			// Check node names
			nodeNames := make([]string, len(ppi.Status.NodeStatus))
			for i, ns := range ppi.Status.NodeStatus {
				nodeNames[i] = ns.Node
			}
			Expect(nodeNames).To(ContainElements("node-1", "node-2"))
		})
	})
})
