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
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kwarmv1alpha1 "github.com/kwarm/kwarm/api/v1alpha1"
	"github.com/kwarm/kwarm/internal/metrics"
	"github.com/kwarm/kwarm/internal/puller"
	"github.com/kwarm/kwarm/internal/ratelimiter"
)

const (
	// FinalizerName is the finalizer for PrePullImage
	FinalizerName = "kwarm.io/finalizer"

	// DefaultRequeueDelay is the default delay for requeuing
	DefaultRequeueDelay = 5 * time.Second

	// CompletedTTL is the default TTL for completed PrePullImages
	CompletedTTL = 1 * time.Hour
)

// PrePullImageReconciler reconciles a PrePullImage object
type PrePullImageReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	RateLimiter *ratelimiter.RateLimiter
}

// +kubebuilder:rbac:groups=kwarm.io,resources=prepullimages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kwarm.io,resources=prepullimages/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kwarm.io,resources=prepullimages/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

// Reconcile manages the lifecycle of PrePullImage resources
func (r *PrePullImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.RecordReconcile("prepullimage", "success", duration)
	}()

	// Fetch the PrePullImage
	ppi := &kwarmv1alpha1.PrePullImage{}
	if err := r.Get(ctx, req.NamespacedName, ppi); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to fetch PrePullImage")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !ppi.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, ppi)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(ppi, FinalizerName) {
		controllerutil.AddFinalizer(ppi, FinalizerName)
		if err := r.Update(ctx, ppi); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle based on phase
	switch ppi.Status.Phase {
	case "", kwarmv1alpha1.PrePullImagePhasePending:
		return r.handlePending(ctx, ppi)
	case kwarmv1alpha1.PrePullImagePhaseInProgress:
		return r.handleInProgress(ctx, ppi)
	case kwarmv1alpha1.PrePullImagePhaseCompleted:
		return r.handleCompleted(ctx, ppi)
	case kwarmv1alpha1.PrePullImagePhaseFailed:
		return r.handleFailed(ctx, ppi)
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles the deletion of a PrePullImage
func (r *PrePullImageReconciler) handleDeletion(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(ppi, FinalizerName) {
		// Release rate limiter slots for any active pulls
		for _, nodeStatus := range ppi.Status.NodeStatus {
			if nodeStatus.Status == kwarmv1alpha1.NodePullStatusPulling {
				r.RateLimiter.Release(nodeStatus.Node)
				metrics.DecrementActivePulls()
			}
		}

		// Cleanup jobs
		if err := puller.CleanupJobsForPrePullImage(ctx, r.Client, ppi.Name); err != nil {
			logger.Error(err, "failed to cleanup jobs")
			// Continue with finalizer removal even if cleanup fails
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(ppi, FinalizerName)
		if err := r.Update(ctx, ppi); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// handlePending initializes the PrePullImage and transitions to InProgress
func (r *PrePullImageReconciler) handlePending(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Get target nodes
	targetNodes, err := r.getTargetNodes(ctx, ppi)
	if err != nil {
		logger.Error(err, "failed to get target nodes")
		return ctrl.Result{}, err
	}

	if len(targetNodes) == 0 {
		logger.Info("no target nodes found, marking as completed")
		ppi.Status.Phase = kwarmv1alpha1.PrePullImagePhaseCompleted
		r.setCondition(ppi, ConditionTypeReady, metav1.ConditionTrue, "NoNodes", "No target nodes found")
		if err := r.Status().Update(ctx, ppi); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Initialize node status
	now := metav1.Now()
	ppi.Status.NodeStatus = make([]kwarmv1alpha1.NodeStatus, len(targetNodes))
	for i, nodeName := range targetNodes {
		ppi.Status.NodeStatus[i] = kwarmv1alpha1.NodeStatus{
			Node:   nodeName,
			Status: kwarmv1alpha1.NodePullStatusPending,
		}
	}

	// Update summary
	ppi.Status.Summary = kwarmv1alpha1.PullSummary{
		Total:   int32(len(targetNodes)),
		Pending: int32(len(targetNodes)),
	}

	// Transition to InProgress
	ppi.Status.Phase = kwarmv1alpha1.PrePullImagePhaseInProgress
	ppi.Status.StartTime = &now
	r.setCondition(ppi, ConditionTypeReady, metav1.ConditionFalse, "InProgress", "Pull operation in progress")

	if err := r.Status().Update(ctx, ppi); err != nil {
		return ctrl.Result{}, err
	}

	r.Recorder.Event(ppi, corev1.EventTypeNormal, "PullStarted",
		fmt.Sprintf("Started pulling image to %d nodes", len(targetNodes)))

	logger.Info("initialized PrePullImage",
		"name", ppi.Name,
		"targetNodes", len(targetNodes))

	return ctrl.Result{Requeue: true}, nil
}

// handleInProgress manages the pull jobs for each node
func (r *PrePullImageReconciler) handleInProgress(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pending, pulling, completed, failed int32
	needsRequeue := false

	for i := range ppi.Status.NodeStatus {
		nodeStatus := &ppi.Status.NodeStatus[i]

		switch nodeStatus.Status {
		case kwarmv1alpha1.NodePullStatusPending:
			// Try to start a pull job
			if r.RateLimiter.TryAcquire(nodeStatus.Node) {
				if err := r.startPullJob(ctx, ppi, nodeStatus); err != nil {
					logger.Error(err, "failed to start pull job", "node", nodeStatus.Node)
					r.RateLimiter.Release(nodeStatus.Node)
					needsRequeue = true
					pending++
				} else {
					pulling++
					metrics.IncrementActivePulls()
				}
			} else {
				// Rate limited, will retry later
				pending++
				needsRequeue = true
			}

		case kwarmv1alpha1.NodePullStatusPulling:
			// Check job status
			jobStatus, err := r.checkJobStatus(ctx, ppi, nodeStatus)
			if err != nil {
				logger.Error(err, "failed to check job status", "node", nodeStatus.Node)
				pulling++
				needsRequeue = true
				continue
			}

			switch jobStatus {
			case puller.JobStatusSucceeded:
				nodeStatus.Status = kwarmv1alpha1.NodePullStatusCompleted
				now := metav1.Now()
				nodeStatus.CompletedAt = &now
				if nodeStatus.StartedAt != nil {
					duration := now.Sub(nodeStatus.StartedAt.Time)
					nodeStatus.Duration = duration.String()
					metrics.RecordPullDuration(ppi.Spec.Image, nodeStatus.Node, "success", duration.Seconds())
				}
				r.RateLimiter.Release(nodeStatus.Node)
				metrics.DecrementActivePulls()
				metrics.IncrementPullsTotal("success")
				completed++

			case puller.JobStatusFailed:
				// Check retry count
				if nodeStatus.RetryCount < ppi.Spec.Retry.MaxAttempts {
					// Retry
					nodeStatus.RetryCount++
					nodeStatus.Status = kwarmv1alpha1.NodePullStatusPending
					nodeStatus.Error = ""
					// Delete the failed job
					if err := puller.DeleteJob(ctx, r.Client, nodeStatus.JobName); err != nil {
						logger.Error(err, "failed to delete job", "job", nodeStatus.JobName)
					}
					r.RateLimiter.Release(nodeStatus.Node)
					metrics.DecrementActivePulls()
					pending++
					needsRequeue = true
				} else {
					// Max retries exceeded
					nodeStatus.Status = kwarmv1alpha1.NodePullStatusFailed
					now := metav1.Now()
					nodeStatus.CompletedAt = &now
					nodeStatus.Error = "max retries exceeded"
					r.RateLimiter.Release(nodeStatus.Node)
					metrics.DecrementActivePulls()
					metrics.IncrementPullsTotal("failed")
					r.Recorder.Event(ppi, corev1.EventTypeWarning, "PullFailed",
						fmt.Sprintf("Failed to pull image on node %s: %s", nodeStatus.Node, nodeStatus.Error))
					failed++
				}

			default:
				// Still running or pending
				pulling++
				needsRequeue = true
			}

		case kwarmv1alpha1.NodePullStatusCompleted:
			completed++

		case kwarmv1alpha1.NodePullStatusFailed:
			failed++
		}
	}

	// Update summary
	ppi.Status.Summary = kwarmv1alpha1.PullSummary{
		Total:     int32(len(ppi.Status.NodeStatus)),
		Pending:   pending,
		Pulling:   pulling,
		Completed: completed,
		Failed:    failed,
	}

	// Update metrics
	metrics.SetQueueDepth(float64(pending))

	// Check if all nodes are done
	if pending == 0 && pulling == 0 {
		now := metav1.Now()
		ppi.Status.CompletionTime = &now

		if failed > 0 {
			ppi.Status.Phase = kwarmv1alpha1.PrePullImagePhaseFailed
			r.setCondition(ppi, ConditionTypeReady, metav1.ConditionFalse, "Failed",
				fmt.Sprintf("Pull failed on %d/%d nodes", failed, ppi.Status.Summary.Total))
			r.Recorder.Event(ppi, corev1.EventTypeWarning, "PullPartiallyFailed",
				fmt.Sprintf("Image pulled to %d/%d nodes, %d failed", completed, ppi.Status.Summary.Total, failed))
		} else {
			ppi.Status.Phase = kwarmv1alpha1.PrePullImagePhaseCompleted
			r.setCondition(ppi, ConditionTypeReady, metav1.ConditionTrue, "Completed",
				fmt.Sprintf("Image pulled to all %d nodes successfully", completed))
			r.Recorder.Event(ppi, corev1.EventTypeNormal, "PullCompleted",
				fmt.Sprintf("Image pulled to all %d nodes successfully", completed))
		}
	} else {
		r.setCondition(ppi, ConditionTypeReady, metav1.ConditionFalse, "InProgress",
			fmt.Sprintf("%d/%d nodes completed", completed, ppi.Status.Summary.Total))
	}

	if err := r.Status().Update(ctx, ppi); err != nil {
		return ctrl.Result{}, err
	}

	// Update phase metrics
	r.updatePhaseMetrics(ctx)

	if needsRequeue {
		return ctrl.Result{RequeueAfter: DefaultRequeueDelay}, nil
	}

	return ctrl.Result{}, nil
}

// handleCompleted handles completed PrePullImages (TTL cleanup)
func (r *PrePullImageReconciler) handleCompleted(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check TTL
	if ppi.Status.CompletionTime != nil {
		age := time.Since(ppi.Status.CompletionTime.Time)
		if age > CompletedTTL {
			logger.Info("deleting completed PrePullImage due to TTL", "name", ppi.Name, "age", age)
			if err := r.Delete(ctx, ppi); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		// Requeue for TTL check
		remainingTTL := CompletedTTL - age
		return ctrl.Result{RequeueAfter: remainingTTL}, nil
	}

	return ctrl.Result{RequeueAfter: CompletedTTL}, nil
}

// handleFailed handles failed PrePullImages
func (r *PrePullImageReconciler) handleFailed(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage) (ctrl.Result, error) {
	// Similar to completed, apply TTL
	return r.handleCompleted(ctx, ppi)
}

// getTargetNodes returns the list of target nodes for the PrePullImage
func (r *PrePullImageReconciler) getTargetNodes(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage) ([]string, error) {
	// If specific nodes are listed, use them
	if len(ppi.Spec.Nodes.Names) > 0 {
		return ppi.Spec.Nodes.Names, nil
	}

	// Otherwise, use label selector
	nodes := &corev1.NodeList{}
	listOpts := []client.ListOption{}

	if ppi.Spec.Nodes.Selector != nil {
		selector, err := metav1.LabelSelectorAsSelector(ppi.Spec.Nodes.Selector)
		if err != nil {
			return nil, err
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})
	}

	if err := r.List(ctx, nodes, listOpts...); err != nil {
		return nil, err
	}

	var nodeNames []string
	for _, node := range nodes.Items {
		if isNodeReady(&node) && !node.Spec.Unschedulable {
			nodeNames = append(nodeNames, node.Name)
		}
	}

	return nodeNames, nil
}

// startPullJob creates a pull job for a node
func (r *PrePullImageReconciler) startPullJob(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage, nodeStatus *kwarmv1alpha1.NodeStatus) error {
	job, err := puller.CreatePullJob(ctx, r.Client, ppi, nodeStatus.Node)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Job already exists, just update status
			nodeStatus.JobName = puller.GenerateJobName(ppi.Name, nodeStatus.Node)
			nodeStatus.Status = kwarmv1alpha1.NodePullStatusPulling
			now := metav1.Now()
			nodeStatus.StartedAt = &now
			return nil
		}
		return err
	}

	nodeStatus.JobName = job.Name
	nodeStatus.Status = kwarmv1alpha1.NodePullStatusPulling
	now := metav1.Now()
	nodeStatus.StartedAt = &now

	return nil
}

// checkJobStatus checks the status of a pull job
func (r *PrePullImageReconciler) checkJobStatus(ctx context.Context, ppi *kwarmv1alpha1.PrePullImage, nodeStatus *kwarmv1alpha1.NodeStatus) (puller.JobStatus, error) {
	job, err := puller.GetJob(ctx, r.Client, nodeStatus.JobName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Job doesn't exist, consider it failed
			return puller.JobStatusFailed, nil
		}
		return puller.JobStatusPending, err
	}

	status := puller.GetJobStatus(job)
	if status == puller.JobStatusFailed {
		nodeStatus.Error = puller.GetJobFailureMessage(job)
	}

	return status, nil
}

// setCondition sets a condition on the PrePullImage status
func (r *PrePullImageReconciler) setCondition(ppi *kwarmv1alpha1.PrePullImage, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: ppi.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	meta.SetStatusCondition(&ppi.Status.Conditions, condition)
}

// updatePhaseMetrics updates the phase metrics
func (r *PrePullImageReconciler) updatePhaseMetrics(ctx context.Context) {
	ppiList := &kwarmv1alpha1.PrePullImageList{}
	if err := r.List(ctx, ppiList); err != nil {
		return
	}

	phases := map[string]float64{
		string(kwarmv1alpha1.PrePullImagePhasePending):    0,
		string(kwarmv1alpha1.PrePullImagePhaseInProgress): 0,
		string(kwarmv1alpha1.PrePullImagePhaseCompleted):  0,
		string(kwarmv1alpha1.PrePullImagePhaseFailed):     0,
	}

	for _, ppi := range ppiList.Items {
		phase := string(ppi.Status.Phase)
		if phase == "" {
			phase = string(kwarmv1alpha1.PrePullImagePhasePending)
		}
		phases[phase]++
	}

	for phase, count := range phases {
		metrics.SetPrePullImagesByPhase(phase, count)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PrePullImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kwarmv1alpha1.PrePullImage{}).
		Owns(&batchv1.Job{}).
		Named("prepullimage").
		Complete(r)
}

// unused but keeping for reference
var _ = labels.SelectorFromSet
