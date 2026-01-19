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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kwarmv1alpha1 "github.com/kwarm/kwarm/api/v1alpha1"
	"github.com/kwarm/kwarm/internal/metrics"
)

const (
	// ConditionTypeReady is the condition type for ready status
	ConditionTypeReady = "Ready"
)

// PrePullPolicyReconciler reconciles a PrePullPolicy object
type PrePullPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kwarm.io,resources=prepullpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kwarm.io,resources=prepullpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kwarm.io,resources=prepullpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *PrePullPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.RecordReconcile("prepullpolicy", "success", duration)
	}()

	// Fetch the PrePullPolicy
	policy := &kwarmv1alpha1.PrePullPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to fetch PrePullPolicy")
			return ctrl.Result{}, err
		}
		// Policy was deleted, nothing to do
		return ctrl.Result{}, nil
	}

	logger.Info("reconciling PrePullPolicy", "name", policy.Name)

	// Count matching deployments
	watchedCount, err := r.countMatchingDeployments(ctx, policy)
	if err != nil {
		logger.Error(err, "failed to count matching deployments")
		r.setCondition(policy, ConditionTypeReady, metav1.ConditionFalse, "Error", err.Error())
		if statusErr := r.Status().Update(ctx, policy); statusErr != nil {
			logger.Error(statusErr, "failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Update status
	now := metav1.Now()
	policy.Status.WatchedDeployments = int32(watchedCount)
	policy.Status.LastUpdated = &now

	// Set ready condition
	r.setCondition(policy, ConditionTypeReady, metav1.ConditionTrue, "Reconciled", "Policy is active")

	if err := r.Status().Update(ctx, policy); err != nil {
		logger.Error(err, "failed to update status")
		return ctrl.Result{}, err
	}

	// Update global metric
	metrics.SetWatchedDeployments(float64(watchedCount))

	logger.Info("reconciled PrePullPolicy",
		"name", policy.Name,
		"watchedDeployments", watchedCount)

	// Requeue periodically to update watched deployment count
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// countMatchingDeployments counts deployments that match the policy selector
func (r *PrePullPolicyReconciler) countMatchingDeployments(ctx context.Context, policy *kwarmv1alpha1.PrePullPolicy) (int, error) {
	count := 0

	// Get namespaces to search
	namespaces := policy.Spec.Selector.Namespaces
	if len(namespaces) == 0 {
		// Search all namespaces
		namespaces = []string{""}
	}

	for _, ns := range namespaces {
		deployments := &appsv1.DeploymentList{}

		listOpts := []client.ListOption{}
		if ns != "" {
			listOpts = append(listOpts, client.InNamespace(ns))
		}

		if err := r.List(ctx, deployments, listOpts...); err != nil {
			return 0, err
		}

		for _, deployment := range deployments.Items {
			if r.deploymentMatchesSelector(&deployment, policy.Spec.Selector) {
				count++
			}
		}
	}

	return count, nil
}

// deploymentMatchesSelector checks if a deployment matches the policy selector
func (r *PrePullPolicyReconciler) deploymentMatchesSelector(deployment *appsv1.Deployment, selector kwarmv1alpha1.PolicySelector) bool {
	if len(selector.MatchLabels) == 0 {
		return false
	}

	labelSelector := labels.SelectorFromSet(selector.MatchLabels)
	return labelSelector.Matches(labels.Set(deployment.Labels))
}

// setCondition sets a condition on the policy status
func (r *PrePullPolicyReconciler) setCondition(policy *kwarmv1alpha1.PrePullPolicy, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	meta.SetStatusCondition(&policy.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PrePullPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kwarmv1alpha1.PrePullPolicy{}).
		Named("prepullpolicy").
		Complete(r)
}
