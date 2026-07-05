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

	"github.com/sylvester-francis/drover/model"
)

// The Anthropic client sets version + key headers, extracts the system prompt to
// the top level, and decodes a tool_use block from the reply.
func TestAnthropic_HeadersSystemAndToolUse(t *testing.T) {
	var gotVersion, gotKey, gotLoopID string
	var gotReq antRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("anthropic-version")
		gotKey = r.Header.Get("x-api-key")
		gotLoopID = r.Header.Get("X-Loop-Id")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotReq)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"content":[`+
			`{"type":"text","text":"let me check"},`+
			`{"type":"tool_use","id":"toolu_1","name":"echo","input":{"text":"hi"}}`+
			`],"stop_reason":"tool_use"}`)
	}))
	defer srv.Close()

	c := NewAnthropic(Config{BaseURL: srv.URL, APIKey: "sk-test", RunID: "run-9"})
	resp, err := c.Complete(context.Background(), model.Request{
		Model: "claude-x",
		Messages: []model.Message{
			{Role: model.System, Content: "be terse"},
			{Role: model.User, Content: "hi"},
		},
		Tools: []model.ToolSchema{{Name: "echo", Description: "echoes", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if gotVersion != anthropicVersion || gotKey != "sk-test" || gotLoopID != "run-9" {
		t.Fatalf("headers: version=%q key=%q loop=%q", gotVersion, gotKey, gotLoopID)
	}
	if gotReq.System != "be terse" {
		t.Fatalf("system = %q, want it lifted to the top level", gotReq.System)
	}
	if gotReq.MaxTokens != defaultMaxTokens {
		t.Fatalf("max_tokens = %d, want default %d", gotReq.MaxTokens, defaultMaxTokens)
	}
	if resp.Content != "let me check" || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "echo" {
		t.Fatalf("resp not decoded: %+v", resp)
	}
	if string(resp.ToolCalls[0].Args) != `{"text":"hi"}` {
		t.Fatalf("args = %s", resp.ToolCalls[0].Args)
	}
}

// Consecutive tool results coalesce into a single user message (Anthropic forbids
// two consecutive user messages), and the system prompt is not left in messages.
func TestAnthropic_CoalescesToolResults(t *testing.T) {
	req := model.Request{
		Model: "claude-x",
		Messages: []model.Message{
			{Role: model.System, Content: "sys"},
			{Role: model.User, Content: "goal"},
			{Role: model.Assistant, ToolCalls: []model.ToolCall{
				{ID: "a", Name: "t", Args: json.RawMessage(`{}`)},
				{ID: "b", Name: "t", Args: json.RawMessage(`{}`)},
			}},
			{Role: model.Tool, ToolCallID: "a", Content: "ra"},
			{Role: model.Tool, ToolCallID: "b", Content: "rb"},
		},
	}
	out := anthropicRequest(req, 100)

	if out.System != "sys" {
		t.Fatalf("system = %q, want sys", out.System)
	}
	// Expect: user(goal), assistant(2 tool_use), user(2 tool_result) — three msgs.
	if len(out.Messages) != 3 {
		t.Fatalf("messages = %d, want 3: %+v", len(out.Messages), out.Messages)
	}
	if out.Messages[0].Role != "user" || out.Messages[1].Role != "assistant" || out.Messages[2].Role != "user" {
		t.Fatalf("roles = %q/%q/%q, want user/assistant/user",
			out.Messages[0].Role, out.Messages[1].Role, out.Messages[2].Role)
	}
	last := out.Messages[2].Content
	if len(last) != 2 || last[0].Type != "tool_result" || last[1].Type != "tool_result" {
		t.Fatalf("final user message should hold 2 tool_result blocks, got %+v", last)
	}
	if last[0].ToolUseID != "a" || last[1].ToolUseID != "b" {
		t.Fatalf("tool_result ids = %q/%q, want a/b", last[0].ToolUseID, last[1].ToolUseID)
	}
}
