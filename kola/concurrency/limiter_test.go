// Copyright 2026 Microsoft Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package concurrency

import (
	"testing"
	"time"
)

func TestResourceLimiterAccountsForWeight(t *testing.T) {
	limiter := New(4)
	heavy := limiter.Acquire(3)
	light := limiter.Acquire(1)

	attempting := make(chan struct{})
	started := make(chan int, 1)
	go func() {
		close(attempting)
		started <- limiter.Acquire(3)
	}()
	<-attempting

	select {
	case <-started:
		t.Fatal("second heavy test started while the budget was exhausted")
	case <-time.After(20 * time.Millisecond):
	}

	limiter.Release(heavy)
	var secondHeavy int
	select {
	case secondHeavy = <-started:
	case <-time.After(time.Second):
		t.Fatal("second heavy test did not start after capacity was released")
	}

	limiter.Release(secondHeavy)
	limiter.Release(light)
}

func TestResourceLimiterCapsOversizedWeight(t *testing.T) {
	limiter := New(4)
	if got := limiter.Acquire(10); got != 4 {
		t.Fatalf("Acquire(10) reserved %d units, want 4", got)
	} else {
		limiter.Release(got)
	}
}

func TestResourceLimiterUsesDefaultCapacity(t *testing.T) {
	limiter := New(0)
	if limiter.Capacity() < 1 {
		t.Fatalf("New(0) capacity = %d, want at least 1", limiter.Capacity())
	}
}

func TestWeightForPlatform(t *testing.T) {
	for _, platform := range []string{"qemu", "qemu-unpriv"} {
		if got := WeightForPlatform(3, platform); got != 3 {
			t.Errorf("WeightForPlatform(3, %q) = %d, want 3", platform, got)
		}
	}
	for _, platform := range []string{"azure", "aws", "gce"} {
		if got := WeightForPlatform(3, platform); got != 1 {
			t.Errorf("WeightForPlatform(3, %q) = %d, want 1", platform, got)
		}
	}
}
