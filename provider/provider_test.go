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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sylvester-francis/drover/model"
)

// The OpenAI client sends X-Loop-Id and the request body, and decodes a tool call
// from the reply.
func TestOpenAI_SendsLoopIDAndParsesToolCall(t *testing.T) {
	var gotLoopID string
	var gotReq oaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLoopID = r.Header.Get("X-Loop-Id")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotReq)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[`+
			`{"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}`+
			`]},"finish_reason":"tool_calls"}]}`)
	}))
	defer srv.Close()

	c := NewOpenAI(Config{BaseURL: srv.URL, RunID: "run-7"})
	resp, err := c.Complete(context.Background(), model.Request{
		Model:    "gpt-x",
		Messages: []model.Message{{Role: model.User, Content: "hi"}},
		Tools:    []model.ToolSchema{{Name: "echo", Description: "echoes"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if gotLoopID != "run-7" {
		t.Fatalf("X-Loop-Id = %q, want run-7", gotLoopID)
	}
	if gotReq.Model != "gpt-x" || len(gotReq.Tools) != 1 || gotReq.ToolChoice != "auto" {
		t.Fatalf("request not encoded as expected: %+v", gotReq)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "echo" {
		t.Fatalf("tool call not parsed: %+v", resp.ToolCalls)
	}
	if string(resp.ToolCalls[0].Args) != `{"text":"hi"}` {
		t.Fatalf("args = %s, want {\"text\":\"hi\"}", resp.ToolCalls[0].Args)
	}
}

// A plain content answer decodes to Response.Content.
func TestOpenAI_ParsesContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"the answer"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	c := NewOpenAI(Config{BaseURL: srv.URL})
	resp, err := c.Complete(context.Background(), model.Request{Model: "gpt-x"})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "the answer" || resp.Acting() {
		t.Fatalf("resp = %+v, want content-only answer", resp)
	}
}

// A 429 without Retry-After is a terminal boundary: Response.Stopped carries the
// leash reason and no error is returned.
func TestGovernor_TerminalStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"type":"leash_boundary","reason":"cost_budget"}}`)
	}))
	defer srv.Close()
	c := NewOpenAI(Config{BaseURL: srv.URL})
	resp, err := c.Complete(context.Background(), model.Request{Model: "gpt-x"})
	if err != nil {
		t.Fatalf("a governance stop must not be an error: %v", err)
	}
	if resp.Stopped != "cost_budget" {
		t.Fatalf("stopped = %q, want cost_budget", resp.Stopped)
	}
}

// A 429 with Retry-After is rate-limit backpressure: Response.RetryAfter is set.
func TestGovernor_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"type":"leash_boundary","reason":"rate"}}`)
	}))
	defer srv.Close()
	c := NewOpenAI(Config{BaseURL: srv.URL})
	resp, err := c.Complete(context.Background(), model.Request{Model: "gpt-x"})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.RetryAfter != 3*time.Second {
		t.Fatalf("retryAfter = %s, want 3s", resp.RetryAfter)
	}
	if resp.Stopped != "" {
		t.Fatalf("rate-limit must not set Stopped, got %q", resp.Stopped)
	}
}

// A 5xx is a transient failure surfaced as an error, so the loop retries it.
func TestGovernor_TransientIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "upstream boom")
	}))
	defer srv.Close()
	c := NewOpenAI(Config{BaseURL: srv.URL})
	if _, err := c.Complete(context.Background(), model.Request{Model: "gpt-x"}); err == nil {
		t.Fatal("a 5xx must surface as an error")
	}
}
