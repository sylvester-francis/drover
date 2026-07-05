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

package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sylvester-francis/drover/model"
	"github.com/sylvester-francis/drover/provider"
)

// Through a leash proxy, each provider keeps its own chat path, so a governed call
// routes to the right upstream path when leash joins it onto its --upstream. Gemini's
// OpenAI-compatible path differs from OpenAI's; that difference is what makes
// governed Gemini route correctly instead of doubling the version segment.
func TestBuildClientRoutesProviderPaths(t *testing.T) {
	for _, tc := range []struct {
		provider string
		wantPath string
	}{
		{"openai", "/v1/chat/completions"},
		{"gemini", "/chat/completions"},
		{"ollama", "/v1/chat/completions"},
		{"anthropic", "/v1/messages"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"content":[{"type":"text","text":"ok"}]}`)
			}))
			defer srv.Close()
			// srv.URL stands in for a leash proxy URL.
			c, err := buildClient(tc.provider, srv.URL, provider.Config{})
			if err != nil {
				t.Fatalf("buildClient(%s): %v", tc.provider, err)
			}
			if _, err := c.Complete(context.Background(), model.Request{Model: "m"}); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Fatalf("provider %s routed to %q, want %q", tc.provider, gotPath, tc.wantPath)
			}
		})
	}
}

func TestBuildClientFakeAndUnknown(t *testing.T) {
	if _, err := buildClient("fake", "", provider.Config{}); err != nil {
		t.Fatalf("fake: %v", err)
	}
	if _, err := buildClient("bogus", "", provider.Config{}); err == nil {
		t.Fatal("unknown provider should error")
	}
}

func TestPickKey(t *testing.T) {
	if got := pickKey("flag", "openai"); got != "flag" {
		t.Errorf("flag should win, got %q", got)
	}
	t.Setenv("OPENAI_API_KEY", "ok")
	t.Setenv("ANTHROPIC_API_KEY", "ak")
	t.Setenv("GEMINI_API_KEY", "gk")
	for _, tc := range []struct{ prov, want string }{
		{"openai", "ok"},
		{"anthropic", "ak"},
		{"gemini", "gk"},
		{"ollama", ""},
	} {
		if got := pickKey("", tc.prov); got != tc.want {
			t.Errorf("pickKey(%q) = %q, want %q", tc.prov, got, tc.want)
		}
	}
}
