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

package register

import "testing"

func TestEffectiveParallelismWeight(t *testing.T) {
	tests := []struct {
		name string
		test Test
		want int
	}{
		{name: "single machine default", test: Test{}, want: 1},
		{name: "static cluster", test: Test{ClusterSize: 3}, want: 3},
		{name: "explicit dynamic weight", test: Test{ParallelismWeight: 3}, want: 3},
		{name: "explicit weight overrides cluster", test: Test{ClusterSize: 2, ParallelismWeight: 4}, want: 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.test.EffectiveParallelismWeight(); got != tc.want {
				t.Fatalf("EffectiveParallelismWeight() = %d, want %d", got, tc.want)
			}
		})
	}
}
