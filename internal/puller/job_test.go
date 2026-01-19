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
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestHashNodeName(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		wantLen  int
	}{
		{
			name:     "short node name",
			nodeName: "node-1",
			wantLen:  8,
		},
		{
			name:     "long node name",
			nodeName: "very-long-node-name-that-exceeds-normal-length",
			wantLen:  8,
		},
		{
			name:     "empty node name",
			nodeName: "",
			wantLen:  8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hashNodeName(tt.nodeName)
			if len(hash) != tt.wantLen {
				t.Errorf("hashNodeName() length = %d, want %d", len(hash), tt.wantLen)
			}
		})
	}

	// Test determinism
	t.Run("deterministic", func(t *testing.T) {
		hash1 := hashNodeName("node-1")
		hash2 := hashNodeName("node-1")
		if hash1 != hash2 {
			t.Error("hashNodeName() should be deterministic")
		}
	})

	// Test uniqueness
	t.Run("unique", func(t *testing.T) {
		hash1 := hashNodeName("node-1")
		hash2 := hashNodeName("node-2")
		if hash1 == hash2 {
			t.Error("hashNodeName() should produce different hashes for different inputs")
		}
	})
}

func TestGenerateJobName(t *testing.T) {
	tests := []struct {
		name     string
		ppiName  string
		nodeName string
		wantMax  int
	}{
		{
			name:     "normal names",
			ppiName:  "myapp-v2",
			nodeName: "node-1",
			wantMax:  63,
		},
		{
			name:     "long ppi name",
			ppiName:  "very-long-prepullimage-name-that-is-really-quite-long",
			nodeName: "node-1",
			wantMax:  63,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobName := GenerateJobName(tt.ppiName, tt.nodeName)
			if len(jobName) > tt.wantMax {
				t.Errorf("GenerateJobName() length = %d, want <= %d", len(jobName), tt.wantMax)
			}
			// Should start with prefix
			if len(jobName) < len(JobNamePrefix) || jobName[:len(JobNamePrefix)] != JobNamePrefix {
				t.Errorf("GenerateJobName() should start with %s", JobNamePrefix)
			}
		})
	}

	// Test determinism
	t.Run("deterministic", func(t *testing.T) {
		name1 := GenerateJobName("myapp", "node-1")
		name2 := GenerateJobName("myapp", "node-1")
		if name1 != name2 {
			t.Error("GenerateJobName() should be deterministic")
		}
	})
}

func TestGetJobStatus(t *testing.T) {
	tests := []struct {
		name string
		job  *batchv1.Job
		want JobStatus
	}{
		{
			name: "nil job",
			job:  nil,
			want: JobStatusPending,
		},
		{
			name: "succeeded job",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Succeeded: 1,
				},
			},
			want: JobStatusSucceeded,
		},
		{
			name: "failed job",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 1,
				},
			},
			want: JobStatusFailed,
		},
		{
			name: "active job",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Active: 1,
				},
			},
			want: JobStatusRunning,
		},
		{
			name: "pending job",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{},
			},
			want: JobStatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetJobStatus(tt.job)
			if got != tt.want {
				t.Errorf("GetJobStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetJobFailureMessage(t *testing.T) {
	tests := []struct {
		name string
		job  *batchv1.Job
		want string
	}{
		{
			name: "nil job",
			job:  nil,
			want: "",
		},
		{
			name: "job with failure condition",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Message: "BackoffLimitExceeded",
						},
					},
				},
			},
			want: "BackoffLimitExceeded",
		},
		{
			name: "job without failure condition",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 1,
				},
			},
			want: "unknown failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetJobFailureMessage(tt.job)
			if got != tt.want {
				t.Errorf("GetJobFailureMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
