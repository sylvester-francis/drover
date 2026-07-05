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

// Package agent turns an agent definition into a durable rerun workflow: the
// plan/act/observe loop, the tools it can call, and the folding of a run's
// journal back into conversation state on recovery. It owns orchestration only;
// rerun owns durability, leash owns governance.
package agent

import (
	"context"
	"encoding/json"

	"github.com/sylvester-francis/drover/model"
)

// Tool is one action an agent can take. Schema advertises it to the model;
// Invoke runs it and returns a result string that is fed back into the
// conversation.
//
// A Tool MUST be idempotent. rerun is at-least-once for side effects: if drover
// crashes in the narrow window after Invoke runs and before its journal entry
// commits, the tool runs again on recovery. Write tools so a second identical
// call is harmless (natural keys, upserts, "create if absent"), exactly as a
// production rerun step is written.
type Tool interface {
	Schema() model.ToolSchema
	Invoke(ctx context.Context, args json.RawMessage) (string, error)
}

// FuncTool adapts a plain function to Tool, so a simple tool needs no new type.
type FuncTool struct {
	Def model.ToolSchema
	Fn  func(ctx context.Context, args json.RawMessage) (string, error)
}

// Schema returns the tool's advertised schema.
func (t FuncTool) Schema() model.ToolSchema { return t.Def }

// Invoke runs the underlying function.
func (t FuncTool) Invoke(ctx context.Context, args json.RawMessage) (string, error) {
	return t.Fn(ctx, args)
}

// Toolset indexes tools by name for dispatch, and exposes their schemas to the
// model in a stable order. A stable order matters for determinism: the schema
// list is part of every model Request, and replay must reproduce identical
// requests.
type Toolset struct {
	order  []string
	byName map[string]Tool
}

// NewToolset builds a Toolset from tools, preserving their given order. A
// duplicate tool name panics: two tools answering to one name is a build-time
// programmer error, not a runtime condition.
func NewToolset(tools ...Tool) *Toolset {
	ts := &Toolset{byName: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		name := t.Schema().Name
		if _, dup := ts.byName[name]; dup {
			panic("drover: duplicate tool name: " + name)
		}
		ts.order = append(ts.order, name)
		ts.byName[name] = t
	}
	return ts
}

// Lookup returns the tool registered under name.
func (ts *Toolset) Lookup(name string) (Tool, bool) {
	t, ok := ts.byName[name]
	return t, ok
}

// Schemas returns every tool's schema in registration order, for the model
// Request.
func (ts *Toolset) Schemas() []model.ToolSchema {
	out := make([]model.ToolSchema, 0, len(ts.order))
	for _, name := range ts.order {
		out = append(out, ts.byName[name].Schema())
	}
	return out
}
