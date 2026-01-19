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

// PolicySelector defines which resources to watch
type PolicySelector struct {
	// MatchLabels is a map of {key,value} pairs to match Deployments
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// Namespaces is a list of namespaces to watch. Empty means all namespaces.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// NodeSelector defines which nodes to pre-pull images to
type NodeSelector struct {
	// MatchLabels is a map of {key,value} pairs to match nodes
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// RetryPolicy defines retry behavior for failed pulls
type RetryPolicy struct {
	// MaxAttempts is the maximum number of retry attempts
	// +kubebuilder:default=3
	// +optional
	MaxAttempts int32 `json:"maxAttempts,omitempty"`

	// BackoffSeconds is the number of seconds to wait between retries
	// +kubebuilder:default=30
	// +optional
	BackoffSeconds int32 `json:"backoffSeconds,omitempty"`
}

// ImagePullSecretsConfig defines how to handle imagePullSecrets
type ImagePullSecretsConfig struct {
	// Inherit specifies whether to inherit imagePullSecrets from the source Deployment
	// +kubebuilder:default=true
	// +optional
	Inherit bool `json:"inherit,omitempty"`

	// Additional specifies additional imagePullSecrets to use
	// +optional
	Additional []corev1.LocalObjectReference `json:"additional,omitempty"`
}

// PrePullPolicySpec defines the desired state of PrePullPolicy
type PrePullPolicySpec struct {
	// Selector defines which Deployments to watch
	// +optional
	Selector PolicySelector `json:"selector,omitempty"`

	// NodeSelector defines which nodes to pre-pull images to
	// +optional
	NodeSelector NodeSelector `json:"nodeSelector,omitempty"`

	// Resources defines resource limits for pull jobs
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Retry defines retry policy for failed pulls
	// +optional
	Retry RetryPolicy `json:"retry,omitempty"`

	// Timeout is the timeout for each image pull operation
	// +kubebuilder:default="10m"
	// +optional
	Timeout string `json:"timeout,omitempty"`

	// ImagePullSecrets defines how to handle imagePullSecrets
	// +optional
	ImagePullSecrets ImagePullSecretsConfig `json:"imagePullSecrets,omitempty"`
}

// PrePullPolicyStatus defines the observed state of PrePullPolicy
type PrePullPolicyStatus struct {
	// WatchedDeployments is the number of Deployments being watched by this policy
	// +optional
	WatchedDeployments int32 `json:"watchedDeployments,omitempty"`

	// LastUpdated is the timestamp of the last status update
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// Conditions represent the current state of the PrePullPolicy
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Watched",type="integer",JSONPath=".status.watchedDeployments",description="Number of watched deployments"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PrePullPolicy is the Schema for the prepullpolicies API
type PrePullPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PrePullPolicySpec   `json:"spec,omitempty"`
	Status PrePullPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PrePullPolicyList contains a list of PrePullPolicy
type PrePullPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PrePullPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PrePullPolicy{}, &PrePullPolicyList{})
}
