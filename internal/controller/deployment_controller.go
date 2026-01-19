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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kwarmv1alpha1 "github.com/bcfmtolgahan/kwarm/api/v1alpha1"
	"github.com/bcfmtolgahan/kwarm/internal/metrics"
)

const (
	// AnnotationLastImages stores the last known images for a deployment
	AnnotationLastImages = "kwarm.io/last-images"

	// LabelDeployment is the label key for the source deployment
	LabelDeployment = "kwarm.io/deployment"

	// LabelPolicy is the label key for the policy
	LabelPolicy = "kwarm.io/policy"

	// LabelEnabled is the label to enable pre-pulling for a deployment
	LabelEnabled = "kwarm.io/enabled"
)

// DeploymentReconciler watches Deployments and creates PrePullImage resources
type DeploymentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kwarm.io,resources=prepullimages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kwarm.io,resources=prepullpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile watches Deployments for image changes and creates PrePullImage resources
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.RecordReconcile("deployment", "success", duration)
	}()

	// Fetch the Deployment
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to fetch Deployment")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Find matching policy
	policy, err := r.getMatchingPolicy(ctx, deployment)
	if err != nil {
		logger.Error(err, "failed to get matching policy")
		return ctrl.Result{}, err
	}

	if policy == nil {
		// No matching policy, nothing to do
		return ctrl.Result{}, nil
	}

	logger.Info("deployment matches policy",
		"deployment", deployment.Name,
		"namespace", deployment.Namespace,
		"policy", policy.Name)

	// Extract current images
	currentImages := r.extractImages(deployment)

	// Get last known images from annotation
	lastImages := r.getLastImages(deployment)

	// Find changed images
	changedImages := r.findChangedImages(lastImages, currentImages)

	if len(changedImages) == 0 {
		// No image changes, update annotation if needed and return
		if !r.imagesEqual(lastImages, currentImages) {
			if err := r.updateLastImagesAnnotation(ctx, deployment, currentImages); err != nil {
				logger.Error(err, "failed to update last-images annotation")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	logger.Info("detected image changes",
		"deployment", deployment.Name,
		"changedImages", changedImages)

	// Get target nodes
	targetNodes, err := r.getTargetNodes(ctx, policy)
	if err != nil {
		logger.Error(err, "failed to get target nodes")
		return ctrl.Result{}, err
	}

	if len(targetNodes) == 0 {
		logger.Info("no target nodes found", "policy", policy.Name)
		return ctrl.Result{}, nil
	}

	// Create PrePullImage for each changed image
	for _, image := range changedImages {
		if err := r.createPrePullImage(ctx, deployment, policy, image, targetNodes); err != nil {
			logger.Error(err, "failed to create PrePullImage", "image", image)
			return ctrl.Result{}, err
		}

		r.Recorder.Event(deployment, corev1.EventTypeNormal, "PrePullTriggered",
			fmt.Sprintf("Image pre-pull initiated for %s", image))
	}

	// Update last-images annotation
	if err := r.updateLastImagesAnnotation(ctx, deployment, currentImages); err != nil {
		logger.Error(err, "failed to update last-images annotation")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// getMatchingPolicy finds a PrePullPolicy that matches the deployment
func (r *DeploymentReconciler) getMatchingPolicy(ctx context.Context, deployment *appsv1.Deployment) (*kwarmv1alpha1.PrePullPolicy, error) {
	policies := &kwarmv1alpha1.PrePullPolicyList{}
	if err := r.List(ctx, policies); err != nil {
		return nil, err
	}

	for i := range policies.Items {
		policy := &policies.Items[i]
		if r.deploymentMatchesPolicy(deployment, policy) {
			return policy, nil
		}
	}

	return nil, nil
}

// deploymentMatchesPolicy checks if a deployment matches a policy
func (r *DeploymentReconciler) deploymentMatchesPolicy(deployment *appsv1.Deployment, policy *kwarmv1alpha1.PrePullPolicy) bool {
	selector := policy.Spec.Selector

	// Check namespace filter
	if len(selector.Namespaces) > 0 {
		found := false
		for _, ns := range selector.Namespaces {
			if ns == deployment.Namespace {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check label selector
	if len(selector.MatchLabels) == 0 {
		return false
	}

	labelSelector := labels.SelectorFromSet(selector.MatchLabels)
	return labelSelector.Matches(labels.Set(deployment.Labels))
}

// extractImages extracts all container images from a deployment
func (r *DeploymentReconciler) extractImages(deployment *appsv1.Deployment) map[string]string {
	images := make(map[string]string)

	for _, container := range deployment.Spec.Template.Spec.Containers {
		images[container.Name] = container.Image
	}

	for _, container := range deployment.Spec.Template.Spec.InitContainers {
		images["init-"+container.Name] = container.Image
	}

	return images
}

// getLastImages retrieves the last known images from the annotation
func (r *DeploymentReconciler) getLastImages(deployment *appsv1.Deployment) map[string]string {
	if deployment.Annotations == nil {
		return nil
	}

	annotation := deployment.Annotations[AnnotationLastImages]
	if annotation == "" {
		return nil
	}

	var images map[string]string
	if err := json.Unmarshal([]byte(annotation), &images); err != nil {
		return nil
	}

	return images
}

// findChangedImages returns images that have changed
func (r *DeploymentReconciler) findChangedImages(lastImages, currentImages map[string]string) []string {
	var changed []string

	for containerName, currentImage := range currentImages {
		if lastImages == nil {
			// First time seeing this deployment, pre-pull all images
			changed = append(changed, currentImage)
			continue
		}

		lastImage, exists := lastImages[containerName]
		if !exists || lastImage != currentImage {
			changed = append(changed, currentImage)
		}
	}

	return changed
}

// imagesEqual checks if two image maps are equal
func (r *DeploymentReconciler) imagesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// updateLastImagesAnnotation updates the last-images annotation
func (r *DeploymentReconciler) updateLastImagesAnnotation(ctx context.Context, deployment *appsv1.Deployment, images map[string]string) error {
	data, err := json.Marshal(images)
	if err != nil {
		return err
	}

	if deployment.Annotations == nil {
		deployment.Annotations = make(map[string]string)
	}
	deployment.Annotations[AnnotationLastImages] = string(data)

	return r.Update(ctx, deployment)
}

// getTargetNodes returns the list of nodes to pre-pull images to
func (r *DeploymentReconciler) getTargetNodes(ctx context.Context, policy *kwarmv1alpha1.PrePullPolicy) ([]string, error) {
	nodes := &corev1.NodeList{}

	listOpts := []client.ListOption{}
	if len(policy.Spec.NodeSelector.MatchLabels) > 0 {
		listOpts = append(listOpts, client.MatchingLabels(policy.Spec.NodeSelector.MatchLabels))
	}

	if err := r.List(ctx, nodes, listOpts...); err != nil {
		return nil, err
	}

	var nodeNames []string
	for _, node := range nodes.Items {
		// Skip nodes that are not ready or are unschedulable
		if !isNodeReady(&node) || node.Spec.Unschedulable {
			continue
		}
		nodeNames = append(nodeNames, node.Name)
	}

	return nodeNames, nil
}

// isNodeReady checks if a node is in Ready condition
func isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// createPrePullImage creates a PrePullImage resource for an image
func (r *DeploymentReconciler) createPrePullImage(ctx context.Context, deployment *appsv1.Deployment, policy *kwarmv1alpha1.PrePullPolicy, image string, targetNodes []string) error {
	logger := log.FromContext(ctx)

	// Generate deterministic name
	name := generatePrePullImageName(deployment.Name, image)

	// Check if PrePullImage already exists
	existing := &kwarmv1alpha1.PrePullImage{}
	err := r.Get(ctx, client.ObjectKey{Namespace: deployment.Namespace, Name: name}, existing)
	if err == nil {
		// Already exists, check if it's for the same image
		if existing.Spec.Image == image {
			logger.Info("PrePullImage already exists", "name", name)
			return nil
		}
		// Different image, delete the old one
		if err := r.Delete(ctx, existing); err != nil {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	// Build imagePullSecrets list
	var imagePullSecrets []corev1.LocalObjectReference
	if policy.Spec.ImagePullSecrets.Inherit {
		for _, secret := range deployment.Spec.Template.Spec.ImagePullSecrets {
			imagePullSecrets = append(imagePullSecrets, corev1.LocalObjectReference{
				Name: secret.Name,
			})
		}
	}
	for _, secret := range policy.Spec.ImagePullSecrets.Additional {
		imagePullSecrets = append(imagePullSecrets, secret)
	}

	// Build resources
	resources := policy.Spec.Resources
	if resources.Requests == nil {
		resources.Requests = corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("64Mi"),
			corev1.ResourceCPU:    resource.MustParse("100m"),
		}
	}
	if resources.Limits == nil {
		resources.Limits = corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("256Mi"),
			corev1.ResourceCPU:    resource.MustParse("500m"),
		}
	}

	// Build retry policy
	retry := policy.Spec.Retry
	if retry.MaxAttempts == 0 {
		retry.MaxAttempts = 3
	}
	if retry.BackoffSeconds == 0 {
		retry.BackoffSeconds = 30
	}

	// Build timeout
	timeout := policy.Spec.Timeout
	if timeout == "" {
		timeout = "10m"
	}

	// Create PrePullImage
	ppi := &kwarmv1alpha1.PrePullImage{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: deployment.Namespace,
			Labels: map[string]string{
				LabelDeployment: deployment.Name,
				LabelPolicy:     policy.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         appsv1.SchemeGroupVersion.String(),
					Kind:               "Deployment",
					Name:               deployment.Name,
					UID:                deployment.UID,
					Controller:         boolPtr(true),
					BlockOwnerDeletion: boolPtr(true),
				},
			},
		},
		Spec: kwarmv1alpha1.PrePullImageSpec{
			Image: image,
			Nodes: kwarmv1alpha1.NodeTargetSelector{
				Names: targetNodes,
			},
			ImagePullSecrets: imagePullSecrets,
			Resources:        resources,
			Retry:            retry,
			Timeout:          timeout,
		},
	}

	if err := r.Create(ctx, ppi); err != nil {
		return fmt.Errorf("failed to create PrePullImage: %w", err)
	}

	logger.Info("created PrePullImage",
		"name", name,
		"image", image,
		"targetNodes", len(targetNodes))

	return nil
}

// generatePrePullImageName generates a deterministic name for a PrePullImage
func generatePrePullImageName(deploymentName, image string) string {
	hash := sha256.Sum256([]byte(image))
	hashStr := fmt.Sprintf("%x", hash)[:8]
	name := fmt.Sprintf("%s-%s", deploymentName, hashStr)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func boolPtr(b bool) *bool {
	return &b
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Named("deployment").
		Complete(r)
}
