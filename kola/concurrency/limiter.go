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
	"runtime"
	"sync"
)

type waiter struct {
	weight int
	ready  chan struct{}
}

// Limiter bounds the aggregate scheduling weight of concurrent tests.
type Limiter struct {
	mu       sync.Mutex
	capacity int
	used     int
	waiters  []*waiter
}

// New returns a weighted limiter with the specified capacity.
func New(capacity int) *Limiter {
	if capacity < 1 {
		capacity = runtime.GOMAXPROCS(0)
	}
	return &Limiter{capacity: capacity}
}

// Capacity returns the maximum number of resource units.
func (l *Limiter) Capacity() int {
	return l.capacity
}

// Acquire waits for weight resource units and returns the number reserved.
// A test heavier than the full budget runs alone instead of deadlocking.
func (l *Limiter) Acquire(weight int) int {
	if weight < 1 {
		weight = 1
	}
	if weight > l.capacity {
		weight = l.capacity
	}

	l.mu.Lock()
	if len(l.waiters) == 0 && l.used+weight <= l.capacity {
		l.used += weight
		l.mu.Unlock()
		return weight
	}

	w := &waiter{weight: weight, ready: make(chan struct{})}
	l.waiters = append(l.waiters, w)
	l.mu.Unlock()
	<-w.ready
	return weight
}

// Release returns resource units previously reserved by Acquire.
func (l *Limiter) Release(weight int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.used -= weight
	if l.used < 0 {
		panic("kola: resource limiter released more units than acquired")
	}

	for len(l.waiters) > 0 {
		w := l.waiters[0]
		if l.used+w.weight > l.capacity {
			break
		}
		l.waiters = l.waiters[1:]
		l.used += w.weight
		close(w.ready)
	}
}

// WeightForPlatform enables weighted scheduling only where tests share one
// local machine's compute resources.
func WeightForPlatform(testWeight int, platform string) int {
	switch platform {
	case "qemu", "qemu-unpriv":
		return testWeight
	default:
		return 1
	}
}
