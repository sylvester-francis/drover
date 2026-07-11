// Copyright 2026 Sylvester Francis
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

package model

import (
	"encoding/json"
	"testing"
)

// Add sums each token field, so a caller can accumulate a run total.
func TestUsageAdd(t *testing.T) {
	got := Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}.
		Add(Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30})
	want := Usage{InputTokens: 11, OutputTokens: 22, TotalTokens: 33}
	if got != want {
		t.Fatalf("Add = %+v, want %+v", got, want)
	}
}

// A Response with usage round-trips through JSON without loss, so the value the
// loop journals is the value replay reads back (ADR-0003).
func TestResponseUsageRoundTrip(t *testing.T) {
	in := Response{Content: "hi", Usage: Usage{InputTokens: 5, OutputTokens: 6, TotalTokens: 11}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Response
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Usage != in.Usage {
		t.Fatalf("usage round-trip = %+v, want %+v", out.Usage, in.Usage)
	}
}
