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

package ratelimiter

import (
	"sync"
)

// RateLimiter controls the rate of concurrent image pulls
type RateLimiter struct {
	maxConcurrentPulls int
	maxPullsPerNode    int

	mu               sync.Mutex
	currentPulls     int
	nodeCurrentPulls map[string]int
}

// NewRateLimiter creates a new RateLimiter with the specified limits
func NewRateLimiter(maxConcurrent, maxPerNode int) *RateLimiter {
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	if maxPerNode <= 0 {
		maxPerNode = 2
	}
	return &RateLimiter{
		maxConcurrentPulls: maxConcurrent,
		maxPullsPerNode:    maxPerNode,
		nodeCurrentPulls:   make(map[string]int),
	}
}

// TryAcquire attempts to acquire a pull slot for the given node
// Returns true if slot acquired, false if rate limited
func (r *RateLimiter) TryAcquire(nodeName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.currentPulls >= r.maxConcurrentPulls {
		return false
	}
	if r.nodeCurrentPulls[nodeName] >= r.maxPullsPerNode {
		return false
	}

	r.currentPulls++
	r.nodeCurrentPulls[nodeName]++
	return true
}

// Release releases a pull slot for the given node
func (r *RateLimiter) Release(nodeName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.currentPulls > 0 {
		r.currentPulls--
	}
	if r.nodeCurrentPulls[nodeName] > 0 {
		r.nodeCurrentPulls[nodeName]--
	}
	if r.nodeCurrentPulls[nodeName] <= 0 {
		delete(r.nodeCurrentPulls, nodeName)
	}
}

// CurrentPulls returns the current number of active pulls
func (r *RateLimiter) CurrentPulls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentPulls
}

// CurrentPullsForNode returns the current number of active pulls for a specific node
func (r *RateLimiter) CurrentPullsForNode(nodeName string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nodeCurrentPulls[nodeName]
}

// MaxConcurrentPulls returns the maximum number of concurrent pulls
func (r *RateLimiter) MaxConcurrentPulls() int {
	return r.maxConcurrentPulls
}

// MaxPullsPerNode returns the maximum number of pulls per node
func (r *RateLimiter) MaxPullsPerNode() int {
	return r.maxPullsPerNode
}
