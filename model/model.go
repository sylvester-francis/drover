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

// Package model is drover's provider-agnostic view of a chat model: the message,
// tool, request, and response types, and the Client interface every provider
// implements. It deliberately knows nothing about durability or governance:
// rerun journals these values and leash meters the calls; model only describes
// the conversation.
//
// Every type here must round-trip through JSON without loss. The agent loop
// journals each Response as a rerun step, so on recovery the exact conversation
// is replayed from the journal rather than recomputed against the live model.
package model

import (
	"context"
	"encoding/json"
	"time"
)

// Role identifies who produced a Message.
type Role string

const (
	// System is the instruction preamble.
	System Role = "system"
	// User is the human/task turn.
	User Role = "user"
	// Assistant is the model's turn (text and/or tool calls).
	Assistant Role = "assistant"
	// Tool is the result of a tool invocation, fed back to the model.
	Tool Role = "tool"
)

// Message is one turn in the conversation. ToolCalls is set on an Assistant
// message that wants to act; ToolCallID and Name link a Tool message back to the
// call it answers.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is a model's request to invoke a tool. Args is the raw JSON arguments
// the tool must decode; keeping it raw means the loop journals exactly what the
// model asked for, byte for byte.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolSchema advertises a tool to the model: its name, a description the model
// reasons over, and a JSON Schema for its parameters.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request is a single completion call. Model is the provider's model id; Tools
// is the set the model may call this turn.
type Request struct {
	Model    string       `json:"model"`
	Messages []Message    `json:"messages"`
	Tools    []ToolSchema `json:"tools,omitempty"`
}

// Response is the model's reply. The meaningful outcomes are all VALUES here, not
// error types, because rerun preserves a step's value across replay but not its
// error's concrete type, so the agent loop can branch on these deterministically
// on recovery:
//
//   - ToolCalls non-empty ....... the model wants to act.
//   - Content with no ToolCalls . the model produced a final answer ("done").
//   - Stopped non-empty ......... the leash proxy refused the call for a boundary
//     (a terminal budget stop); the run ends deliberately.
//   - RetryAfter > 0 ............ the leash proxy refused for rate; wait this long
//     (a durable Sleep) and retry the call.
//
// A genuine transient failure (network, decode) is returned as an error instead;
// the loop retries on the mere presence of one, never on its type.
type Response struct {
	Content    string        `json:"content,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	Finish     string        `json:"finish,omitempty"`
	Stopped    string        `json:"stopped,omitempty"`
	RetryAfter time.Duration `json:"retry_after,omitempty"`
}

// Acting reports whether the model asked to call at least one tool.
func (r Response) Acting() bool { return len(r.ToolCalls) > 0 }

// Client is a provider-agnostic chat client. drover ships OpenAI- and
// Anthropic-compatible implementations; each points at whatever endpoint the
// leash proxy fronts, so every call is governed. See Response for how a governance
// refusal, a rate-limit, and a transient failure are each surfaced.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}
