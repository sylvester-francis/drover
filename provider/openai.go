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

	"github.com/sylvester-francis/drover/model"
)

// OpenAI is a model.Client for the OpenAI Chat Completions API, pointed at a
// leash proxy URL.
type OpenAI struct {
	base
	apiKey string
}

// NewOpenAI builds an OpenAI client from cfg.
func NewOpenAI(cfg Config) *OpenAI {
	return &OpenAI{base: newBase(cfg), apiKey: cfg.APIKey}
}

// Complete sends one chat completion through leash and decodes the reply. A
// governance refusal returns as Response.Stopped / Response.RetryAfter (a value),
// never an error. See package model.
func (c *OpenAI) Complete(ctx context.Context, req model.Request) (model.Response, error) {
	body, err := json.Marshal(openAIRequest(req))
	if err != nil {
		return model.Response{}, fmt.Errorf("openai: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return model.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	res, err := c.send(httpReq)
	if err != nil {
		return model.Response{}, err
	}
	if res.governor != nil {
		return *res.governor, nil
	}
	return decodeOpenAI(res.body)
}

// --- wire types -------------------------------------------------------------

type oaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // OpenAI serializes tool args as a JSON string
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiCallFunction `json:"function"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Name       string        `json:"name,omitempty"`
}

type oaiRequest struct {
	Model      string       `json:"model"`
	Messages   []oaiMessage `json:"messages"`
	Tools      []oaiTool    `json:"tools,omitempty"`
	ToolChoice string       `json:"tool_choice,omitempty"`
}

type oaiChoice struct {
	Message struct {
		Content   string        `json:"content"`
		ToolCalls []oaiToolCall `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
}

// openAIRequest maps a provider-agnostic Request onto the Chat Completions wire
// shape: tool-call arguments become a JSON string, tools are wrapped in the
// function envelope, and tool_choice=auto is set whenever tools are offered.
func openAIRequest(req model.Request) oaiRequest {
	out := oaiRequest{Model: req.Model}
	for _, m := range req.Messages {
		om := oaiMessage{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
		for _, tc := range m.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, oaiToolCall{
				ID:       tc.ID,
				Type:     "function",
				Function: oaiCallFunction{Name: tc.Name, Arguments: string(tc.Args)},
			})
		}
		out.Messages = append(out.Messages, om)
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, oaiTool{
			Type:     "function",
			Function: oaiFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}
	if len(out.Tools) > 0 {
		out.ToolChoice = "auto"
	}
	return out
}

// decodeOpenAI maps the first choice back onto a provider-agnostic Response.
func decodeOpenAI(body []byte) (model.Response, error) {
	var r oaiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return model.Response{}, fmt.Errorf("openai: decode response: %w", err)
	}
	if len(r.Choices) == 0 {
		return model.Response{}, fmt.Errorf("openai: response carried no choices")
	}
	ch := r.Choices[0]
	out := model.Response{Content: ch.Message.Content, Finish: ch.FinishReason}
	for _, tc := range ch.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, model.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments),
		})
	}
	return out, nil
}
