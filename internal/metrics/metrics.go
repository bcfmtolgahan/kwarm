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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// PullDuration is a histogram of image pull durations
	PullDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kwarm_pull_duration_seconds",
			Help:    "Duration of image pulls in seconds",
			Buckets: []float64{5, 10, 30, 60, 120, 300, 600},
		},
		[]string{"image", "node", "status"},
	)

	// ActivePulls is a gauge of currently active image pulls
	ActivePulls = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kwarm_active_pulls",
			Help: "Number of currently active image pulls",
		},
	)

	// QueueDepth is a gauge of pending image pulls in queue
	QueueDepth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kwarm_queue_depth",
			Help: "Number of pending image pulls in queue",
		},
	)

	// PullsTotal is a counter of total image pulls by status
	PullsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kwarm_pulls_total",
			Help: "Total number of image pulls",
		},
		[]string{"status"}, // success, failed
	)

	// PrePullImagesByPhase is a gauge of PrePullImage resources by phase
	PrePullImagesByPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kwarm_prepullimages_by_phase",
			Help: "Number of PrePullImage resources by phase",
		},
		[]string{"phase"}, // Pending, InProgress, Completed, Failed
	)

	// WatchedDeployments is a gauge of deployments being watched
	WatchedDeployments = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kwarm_watched_deployments",
			Help: "Number of deployments being watched",
		},
	)

	// ReconcileTotal is a counter of reconciliation operations
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kwarm_reconcile_total",
			Help: "Total number of reconciliation operations",
		},
		[]string{"controller", "result"}, // result: success, error, requeue
	)

	// ReconcileDuration is a histogram of reconciliation durations
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kwarm_reconcile_duration_seconds",
			Help:    "Duration of reconciliation operations in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"controller"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		PullDuration,
		ActivePulls,
		QueueDepth,
		PullsTotal,
		PrePullImagesByPhase,
		WatchedDeployments,
		ReconcileTotal,
		ReconcileDuration,
	)
}

// RecordPullDuration records the duration of an image pull
func RecordPullDuration(image, node, status string, durationSeconds float64) {
	PullDuration.WithLabelValues(image, node, status).Observe(durationSeconds)
}

// IncrementActivePulls increments the active pulls gauge
func IncrementActivePulls() {
	ActivePulls.Inc()
}

// DecrementActivePulls decrements the active pulls gauge
func DecrementActivePulls() {
	ActivePulls.Dec()
}

// SetQueueDepth sets the queue depth gauge
func SetQueueDepth(depth float64) {
	QueueDepth.Set(depth)
}

// IncrementPullsTotal increments the total pulls counter
func IncrementPullsTotal(status string) {
	PullsTotal.WithLabelValues(status).Inc()
}

// SetPrePullImagesByPhase sets the count for a specific phase
func SetPrePullImagesByPhase(phase string, count float64) {
	PrePullImagesByPhase.WithLabelValues(phase).Set(count)
}

// SetWatchedDeployments sets the watched deployments gauge
func SetWatchedDeployments(count float64) {
	WatchedDeployments.Set(count)
}

// RecordReconcile records a reconciliation operation
func RecordReconcile(controller, result string, durationSeconds float64) {
	ReconcileTotal.WithLabelValues(controller, result).Inc()
	ReconcileDuration.WithLabelValues(controller).Observe(durationSeconds)
}
