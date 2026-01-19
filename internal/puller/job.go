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

package puller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kwarmv1alpha1 "github.com/bcfmtolgahan/kwarm/api/v1alpha1"
)

const (
	// JobNamePrefix is the prefix for all pull jobs
	JobNamePrefix = "kwarm-pull"

	// LabelPrePullImage is the label key for the PrePullImage name
	LabelPrePullImage = "kwarm.io/prepullimage"

	// LabelNode is the label key for the target node
	LabelNode = "kwarm.io/node"

	// LabelManagedBy is the label key for the manager
	LabelManagedBy = "app.kubernetes.io/managed-by"

	// OperatorNamespace is the namespace where pull jobs are created
	OperatorNamespace = "kwarm-system"

	// DefaultTTLSecondsAfterFinished is the default TTL for completed jobs
	DefaultTTLSecondsAfterFinished = 300
)

// hashNodeName generates a short hash for a node name
func hashNodeName(nodeName string) string {
	h := sha256.New()
	h.Write([]byte(nodeName))
	return fmt.Sprintf("%x", h.Sum(nil))[:8]
}

// GenerateJobName generates a deterministic job name for a PrePullImage and node
func GenerateJobName(ppiName, nodeName string) string {
	hash := hashNodeName(nodeName)
	name := fmt.Sprintf("%s-%s-%s", JobNamePrefix, ppiName, hash)
	// Kubernetes names must be <= 63 characters
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// CreatePullJob creates a Kubernetes Job to pull an image on a specific node
func CreatePullJob(ctx context.Context, c client.Client, ppi *kwarmv1alpha1.PrePullImage, nodeName string) (*batchv1.Job, error) {
	jobName := GenerateJobName(ppi.Name, nodeName)

	// Parse timeout
	timeout, err := time.ParseDuration(ppi.Spec.Timeout)
	if err != nil {
		timeout = 10 * time.Minute
	}
	activeDeadlineSeconds := int64(timeout.Seconds())

	// Convert imagePullSecrets to the right type
	var imagePullSecrets []corev1.LocalObjectReference
	for _, secret := range ppi.Spec.ImagePullSecrets {
		imagePullSecrets = append(imagePullSecrets, corev1.LocalObjectReference{
			Name: secret.Name,
		})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: OperatorNamespace,
			Labels: map[string]string{
				LabelPrePullImage: ppi.Name,
				LabelNode:         nodeName,
				LabelManagedBy:    "kwarm",
			},
			Annotations: map[string]string{
				// Store owner info in annotations since cross-namespace OwnerReferences are not allowed
				"kwarm.io/prepullimage-namespace": ppi.Namespace,
				"kwarm.io/prepullimage-name":      ppi.Name,
				"kwarm.io/prepullimage-uid":       string(ppi.UID),
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To(int32(DefaultTTLSecondsAfterFinished)),
			BackoffLimit:            ptr.To(int32(0)), // Retry is managed by the controller
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelPrePullImage: ppi.Name,
						LabelNode:         nodeName,
						LabelManagedBy:    "kwarm",
					},
				},
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": nodeName,
					},
					Tolerations: []corev1.Toleration{
						{Operator: corev1.TolerationOpExists}, // Tolerate all taints
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "pull",
							Image:   ppi.Spec.Image,
							Command: []string{"sh", "-c", "echo 'Image pulled successfully' && exit 0"},
							Resources: corev1.ResourceRequirements{
								Requests: ppi.Spec.Resources.Requests,
								Limits:   ppi.Spec.Resources.Limits,
							},
						},
					},
					ImagePullSecrets: imagePullSecrets,
				},
			},
		},
	}

	err = c.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return job, nil
}

// GetJob retrieves a pull job by name
func GetJob(ctx context.Context, c client.Client, jobName string) (*batchv1.Job, error) {
	job := &batchv1.Job{}
	err := c.Get(ctx, client.ObjectKey{
		Namespace: OperatorNamespace,
		Name:      jobName,
	}, job)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// DeleteJob deletes a pull job
func DeleteJob(ctx context.Context, c client.Client, jobName string) error {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: OperatorNamespace,
		},
	}
	propagationPolicy := metav1.DeletePropagationBackground
	return c.Delete(ctx, job, &client.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
}

// JobStatus represents the status of a pull job
type JobStatus int

const (
	// JobStatusPending indicates the job is pending
	JobStatusPending JobStatus = iota
	// JobStatusRunning indicates the job is running
	JobStatusRunning
	// JobStatusSucceeded indicates the job succeeded
	JobStatusSucceeded
	// JobStatusFailed indicates the job failed
	JobStatusFailed
)

// GetJobStatus returns the status of a pull job
func GetJobStatus(job *batchv1.Job) JobStatus {
	if job == nil {
		return JobStatusPending
	}

	if job.Status.Succeeded > 0 {
		return JobStatusSucceeded
	}
	if job.Status.Failed > 0 {
		return JobStatusFailed
	}
	if job.Status.Active > 0 {
		return JobStatusRunning
	}

	return JobStatusPending
}

// GetJobFailureMessage extracts failure message from a job
func GetJobFailureMessage(job *batchv1.Job) string {
	if job == nil {
		return ""
	}

	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return condition.Message
		}
	}

	return "unknown failure"
}

// ListJobsForPrePullImage lists all jobs created for a PrePullImage
func ListJobsForPrePullImage(ctx context.Context, c client.Client, ppiName string) (*batchv1.JobList, error) {
	jobList := &batchv1.JobList{}
	err := c.List(ctx, jobList,
		client.InNamespace(OperatorNamespace),
		client.MatchingLabels{
			LabelPrePullImage: ppiName,
		},
	)
	if err != nil {
		return nil, err
	}
	return jobList, nil
}

// CleanupJobsForPrePullImage deletes all jobs for a PrePullImage
func CleanupJobsForPrePullImage(ctx context.Context, c client.Client, ppiName string) error {
	jobs, err := ListJobsForPrePullImage(ctx, c, ppiName)
	if err != nil {
		return err
	}

	for i := range jobs.Items {
		if err := DeleteJob(ctx, c, jobs.Items[i].Name); err != nil {
			return err
		}
	}

	return nil
}
