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
	"testing"
)

func TestNewRateLimiter(t *testing.T) {
	tests := []struct {
		name           string
		maxConcurrent  int
		maxPerNode     int
		wantConcurrent int
		wantPerNode    int
	}{
		{
			name:           "valid values",
			maxConcurrent:  10,
			maxPerNode:     2,
			wantConcurrent: 10,
			wantPerNode:    2,
		},
		{
			name:           "zero values use defaults",
			maxConcurrent:  0,
			maxPerNode:     0,
			wantConcurrent: 10,
			wantPerNode:    2,
		},
		{
			name:           "negative values use defaults",
			maxConcurrent:  -1,
			maxPerNode:     -1,
			wantConcurrent: 10,
			wantPerNode:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter(tt.maxConcurrent, tt.maxPerNode)
			if rl.MaxConcurrentPulls() != tt.wantConcurrent {
				t.Errorf("MaxConcurrentPulls() = %d, want %d", rl.MaxConcurrentPulls(), tt.wantConcurrent)
			}
			if rl.MaxPullsPerNode() != tt.wantPerNode {
				t.Errorf("MaxPullsPerNode() = %d, want %d", rl.MaxPullsPerNode(), tt.wantPerNode)
			}
		})
	}
}

func TestRateLimiter_TryAcquire(t *testing.T) {
	t.Run("acquire within cluster limit", func(t *testing.T) {
		rl := NewRateLimiter(3, 2)

		// Should acquire successfully for 3 different nodes
		if !rl.TryAcquire("node-1") {
			t.Error("expected first acquire to succeed")
		}
		if !rl.TryAcquire("node-2") {
			t.Error("expected second acquire to succeed")
		}
		if !rl.TryAcquire("node-3") {
			t.Error("expected third acquire to succeed")
		}

		// Fourth acquire should fail (cluster limit)
		if rl.TryAcquire("node-4") {
			t.Error("expected fourth acquire to fail due to cluster limit")
		}

		if rl.CurrentPulls() != 3 {
			t.Errorf("CurrentPulls() = %d, want 3", rl.CurrentPulls())
		}
	})

	t.Run("acquire within node limit", func(t *testing.T) {
		rl := NewRateLimiter(10, 2)

		// Should acquire successfully twice for same node
		if !rl.TryAcquire("node-1") {
			t.Error("expected first acquire to succeed")
		}
		if !rl.TryAcquire("node-1") {
			t.Error("expected second acquire to succeed")
		}

		// Third acquire for same node should fail
		if rl.TryAcquire("node-1") {
			t.Error("expected third acquire to fail due to node limit")
		}

		// But should succeed for different node
		if !rl.TryAcquire("node-2") {
			t.Error("expected acquire for different node to succeed")
		}
	})
}

func TestRateLimiter_Release(t *testing.T) {
	rl := NewRateLimiter(2, 2)

	// Acquire slots
	rl.TryAcquire("node-1")
	rl.TryAcquire("node-1")

	if rl.CurrentPulls() != 2 {
		t.Errorf("CurrentPulls() = %d, want 2", rl.CurrentPulls())
	}
	if rl.CurrentPullsForNode("node-1") != 2 {
		t.Errorf("CurrentPullsForNode(node-1) = %d, want 2", rl.CurrentPullsForNode("node-1"))
	}

	// Release one slot
	rl.Release("node-1")

	if rl.CurrentPulls() != 1 {
		t.Errorf("CurrentPulls() = %d, want 1", rl.CurrentPulls())
	}
	if rl.CurrentPullsForNode("node-1") != 1 {
		t.Errorf("CurrentPullsForNode(node-1) = %d, want 1", rl.CurrentPullsForNode("node-1"))
	}

	// Should be able to acquire again
	if !rl.TryAcquire("node-1") {
		t.Error("expected acquire after release to succeed")
	}

	// Release all
	rl.Release("node-1")
	rl.Release("node-1")

	if rl.CurrentPulls() != 0 {
		t.Errorf("CurrentPulls() = %d, want 0", rl.CurrentPulls())
	}
}

func TestRateLimiter_ReleaseNonExistent(t *testing.T) {
	rl := NewRateLimiter(2, 2)

	// Release without acquire should not panic or go negative
	rl.Release("node-1")

	if rl.CurrentPulls() != 0 {
		t.Errorf("CurrentPulls() = %d, want 0", rl.CurrentPulls())
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter(100, 10)
	var wg sync.WaitGroup

	// Spawn 50 goroutines, each trying to acquire and release
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(nodeNum int) {
			defer wg.Done()
			nodeName := "node-" + string(rune('0'+nodeNum%5))

			for j := 0; j < 100; j++ {
				if rl.TryAcquire(nodeName) {
					// Simulate some work
					rl.Release(nodeName)
				}
			}
		}(i)
	}

	wg.Wait()

	// After all goroutines complete, should have 0 pulls
	if rl.CurrentPulls() != 0 {
		t.Errorf("CurrentPulls() = %d, want 0 after all releases", rl.CurrentPulls())
	}
}
