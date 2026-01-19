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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kwarmv1alpha1 "github.com/bcfmtolgahan/kwarm/api/v1alpha1"
)

var _ = Describe("PrePullPolicy Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-policy"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		prepullpolicy := &kwarmv1alpha1.PrePullPolicy{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind PrePullPolicy")
			err := k8sClient.Get(ctx, typeNamespacedName, prepullpolicy)
			if err != nil && errors.IsNotFound(err) {
				resource := &kwarmv1alpha1.PrePullPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: kwarmv1alpha1.PrePullPolicySpec{
						Selector: kwarmv1alpha1.PolicySelector{
							MatchLabels: map[string]string{
								"kwarm.io/enabled": "true",
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
			resource := &kwarmv1alpha1.PrePullPolicy{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance PrePullPolicy")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &PrePullPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify status was updated
			var policy kwarmv1alpha1.PrePullPolicy
			err = k8sClient.Get(ctx, typeNamespacedName, &policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(policy.Status.WatchedDeployments).To(BeNumerically(">=", 0))
		})

		It("should set Ready condition", func() {
			By("Reconciling the resource")
			controllerReconciler := &PrePullPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Check conditions
			var policy kwarmv1alpha1.PrePullPolicy
			err = k8sClient.Get(ctx, typeNamespacedName, &policy)
			Expect(err).NotTo(HaveOccurred())
			Expect(policy.Status.Conditions).NotTo(BeEmpty())

			// Find Ready condition
			var readyCondition *metav1.Condition
			for i := range policy.Status.Conditions {
				if policy.Status.Conditions[i].Type == ConditionTypeReady {
					readyCondition = &policy.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		})
	})
})
