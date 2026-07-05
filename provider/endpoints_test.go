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

package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sylvester-francis/drover/model"
)

// The OpenAI-compatible constructors default to the right provider endpoints.
func TestOpenAICompatibleDefaultEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(Config) *OpenAI
		want string
	}{
		{"openai", NewOpenAI, "https://api.openai.com/v1/chat/completions"},
		{"gemini", NewGemini, "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		{"ollama", NewOllama, "http://localhost:11434/v1/chat/completions"},
	} {
		if got := tc.make(Config{}).chatURL; got != tc.want {
			t.Errorf("%s default endpoint = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Each constructor posts to its provider's chat-completions path, and cfg.BaseURL
// overrides the host (this is how a leash proxy URL is threaded in).
func TestOpenAICompatiblePostsToRightPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(Config) *OpenAI
		want string
	}{
		{"openai", NewOpenAI, "/v1/chat/completions"},
		{"gemini", NewGemini, "/chat/completions"},
		{"ollama", NewOllama, "/v1/chat/completions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
			}))
			defer srv.Close()
			c := tc.make(Config{BaseURL: srv.URL})
			if _, err := c.Complete(context.Background(), model.Request{Model: "m"}); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if gotPath != tc.want {
				t.Fatalf("%s posted to %q, want %q", tc.name, gotPath, tc.want)
			}
		})
	}
}
