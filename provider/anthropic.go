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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sylvester-francis/drover/model"
)

// anthropicVersion is the API version header value the Messages API expects.
const anthropicVersion = "2023-06-01"

// defaultMaxTokens is used when Config.MaxTokens is unset; Anthropic requires the
// field on every request.
const defaultMaxTokens = 4096

// Anthropic is a model.Client for the Anthropic Messages API, pointed at a leash
// proxy URL.
type Anthropic struct {
	base
	apiKey    string
	maxTokens int
}

// NewAnthropic builds an Anthropic client from cfg.
func NewAnthropic(cfg Config) *Anthropic {
	mt := cfg.MaxTokens
	if mt <= 0 {
		mt = defaultMaxTokens
	}
	return &Anthropic{base: newBase(cfg), apiKey: cfg.APIKey, maxTokens: mt}
}

// Complete sends one message-create through leash and decodes the reply. A
// governance refusal returns as Response.Stopped / Response.RetryAfter (a value),
// never an error — see package model.
func (c *Anthropic) Complete(ctx context.Context, req model.Request) (model.Response, error) {
	body, err := json.Marshal(anthropicRequest(req, c.maxTokens))
	if err != nil {
		return model.Response{}, fmt.Errorf("anthropic: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return model.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}
	res, err := c.send(httpReq)
	if err != nil {
		return model.Response{}, err
	}
	if res.governor != nil {
		return *res.governor, nil
	}
	return decodeAnthropic(res.body)
}

// --- wire types -------------------------------------------------------------

type antTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// antBlock is a content block. The tag set it uses depends on Type: text -> Text;
// tool_use -> ID/Name/Input; tool_result -> ToolUseID/Content.
type antBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type antMessage struct {
	Role    string     `json:"role"`
	Content []antBlock `json:"content"`
}

type antRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []antMessage `json:"messages"`
	Tools     []antTool    `json:"tools,omitempty"`
}

type antResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

// anthropicRequest maps a provider-agnostic Request onto the Messages wire shape.
// Two shape differences from OpenAI are handled here: system prompts move to the
// top-level system field, and consecutive tool results are coalesced into one
// user message (Anthropic requires strict user/assistant alternation, so two tool
// results cannot become two consecutive user messages).
func anthropicRequest(req model.Request, maxTokens int) antRequest {
	out := antRequest{Model: req.Model, MaxTokens: maxTokens}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, antTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}

	var systemParts []string
	var pending []antBlock // tool results awaiting a flush into one user message

	flush := func() {
		if len(pending) > 0 {
			out.Messages = append(out.Messages, antMessage{Role: "user", Content: pending})
			pending = nil
		}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case model.System:
			systemParts = append(systemParts, m.Content)
		case model.Tool:
			pending = append(pending, antBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content})
		case model.User:
			flush()
			out.Messages = append(out.Messages, antMessage{Role: "user", Content: []antBlock{{Type: "text", Text: m.Content}}})
		case model.Assistant:
			flush()
			var blocks []antBlock
			if m.Content != "" {
				blocks = append(blocks, antBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, antBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: tc.Args})
			}
			out.Messages = append(out.Messages, antMessage{Role: "assistant", Content: blocks})
		}
	}
	flush()

	out.System = strings.Join(systemParts, "\n")
	return out
}

// decodeAnthropic folds the response content blocks into a provider-agnostic
// Response: text blocks concatenate into Content, tool_use blocks become ToolCalls.
func decodeAnthropic(body []byte) (model.Response, error) {
	var r antResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return model.Response{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	out := model.Response{Finish: r.StopReason}
	var text strings.Builder
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, model.ToolCall{ID: b.ID, Name: b.Name, Args: b.Input})
		}
	}
	out.Content = text.String()
	return out, nil
}
