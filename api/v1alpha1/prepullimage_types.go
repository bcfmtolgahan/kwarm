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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrePullImagePhase represents the current phase of the PrePullImage
// +kubebuilder:validation:Enum=Pending;InProgress;Completed;Failed
type PrePullImagePhase string

const (
	PrePullImagePhasePending    PrePullImagePhase = "Pending"
	PrePullImagePhaseInProgress PrePullImagePhase = "InProgress"
	PrePullImagePhaseCompleted  PrePullImagePhase = "Completed"
	PrePullImagePhaseFailed     PrePullImagePhase = "Failed"
)

// NodePullStatus represents the status of a pull operation on a specific node
// +kubebuilder:validation:Enum=Pending;Pulling;Completed;Failed
type NodePullStatus string

const (
	NodePullStatusPending   NodePullStatus = "Pending"
	NodePullStatusPulling   NodePullStatus = "Pulling"
	NodePullStatusCompleted NodePullStatus = "Completed"
	NodePullStatusFailed    NodePullStatus = "Failed"
)

// NodeTargetSelector defines how to select target nodes
type NodeTargetSelector struct {
	// Selector is a label selector to match nodes
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Names is a list of specific node names to target
	// +optional
	Names []string `json:"names,omitempty"`
}

// PrePullImageSpec defines the desired state of PrePullImage
type PrePullImageSpec struct {
	// Image is the container image to pull
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImageDigest is the optional digest of the image for verification
	// +optional
	ImageDigest string `json:"imageDigest,omitempty"`

	// Nodes defines which nodes to pull the image to
	// +optional
	Nodes NodeTargetSelector `json:"nodes,omitempty"`

	// ImagePullSecrets is a list of secret references for pulling the image
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Resources defines resource requirements for pull jobs
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Retry defines retry policy for failed pulls
	// +optional
	Retry RetryPolicy `json:"retry,omitempty"`

	// Timeout is the timeout for the pull operation
	// +kubebuilder:default="10m"
	// +optional
	Timeout string `json:"timeout,omitempty"`
}

// NodeStatus represents the pull status for a specific node
type NodeStatus struct {
	// Node is the name of the node
	Node string `json:"node"`

	// Status is the current status of the pull on this node
	Status NodePullStatus `json:"status"`

	// StartedAt is when the pull started
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is when the pull completed (successfully or with failure)
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Duration is the time taken to complete the pull
	// +optional
	Duration string `json:"duration,omitempty"`

	// Error contains error message if the pull failed
	// +optional
	Error string `json:"error,omitempty"`

	// RetryCount is the number of retry attempts
	// +optional
	RetryCount int32 `json:"retryCount,omitempty"`

	// JobName is the name of the Job resource for this node
	// +optional
	JobName string `json:"jobName,omitempty"`
}

// PullSummary contains summary statistics for the pull operation
type PullSummary struct {
	// Total is the total number of target nodes
	Total int32 `json:"total"`

	// Pending is the number of nodes waiting to start
	Pending int32 `json:"pending"`

	// Pulling is the number of nodes currently pulling
	Pulling int32 `json:"pulling"`

	// Completed is the number of nodes that completed successfully
	Completed int32 `json:"completed"`

	// Failed is the number of nodes that failed
	Failed int32 `json:"failed"`
}

// PrePullImageStatus defines the observed state of PrePullImage
type PrePullImageStatus struct {
	// Phase is the current phase of the PrePullImage
	// +kubebuilder:default="Pending"
	// +optional
	Phase PrePullImagePhase `json:"phase,omitempty"`

	// NodeStatus contains the status for each target node
	// +optional
	NodeStatus []NodeStatus `json:"nodeStatus,omitempty"`

	// Summary contains summary statistics
	// +optional
	Summary PullSummary `json:"summary,omitempty"`

	// Conditions represent the current state of the PrePullImage
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// StartTime is when the pull operation started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the pull operation completed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Image",type="string",JSONPath=".spec.image",description="Container image"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current phase"
// +kubebuilder:printcolumn:name="Completed",type="integer",JSONPath=".status.summary.completed",description="Completed nodes"
// +kubebuilder:printcolumn:name="Total",type="integer",JSONPath=".status.summary.total",description="Total nodes"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PrePullImage is the Schema for the prepullimages API
type PrePullImage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PrePullImageSpec   `json:"spec,omitempty"`
	Status PrePullImageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PrePullImageList contains a list of PrePullImage
type PrePullImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrePullImage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PrePullImage{}, &PrePullImageList{})
}
