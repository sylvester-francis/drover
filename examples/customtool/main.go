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

// Command customtool defines a Tool and runs an agent that calls it, showing how
// to extend drover in a little Go. The model is faked and scripted to call the
// tool once, then answer with its result.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/drover/model"
	"github.com/sylvester-francis/drover/provider"
	"github.com/sylvester-francis/drover/runner"
	"github.com/sylvester-francis/rerun/sqlite"
)

func main() {
	ctx := context.Background()

	// A tool is one small, idempotent method. FuncTool adapts a plain function.
	upper := agent.FuncTool{
		Def: model.ToolSchema{
			Name:        "uppercase",
			Description: "Uppercase the given text.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		},
		Fn: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			return strings.ToUpper(a.Text), nil
		},
	}

	// A scripted fake: the first call asks for the tool, the second answers with
	// the folded-back result.
	var calls int
	fake := &provider.Fake{Reply: func(req model.Request) model.Response {
		calls++
		if calls == 1 {
			return model.Response{ToolCalls: []model.ToolCall{
				{ID: "c1", Name: "uppercase", Args: json.RawMessage(`{"text":"drover"}`)},
			}}
		}
		return model.Response{Content: "the tool said: " + req.Messages[len(req.Messages)-1].Content}
	}}

	db := filepath.Join(os.TempDir(), "drover-customtool.db")
	defer os.Remove(db)
	store := sqlite.New(db)
	defer store.Close()

	loop := &agent.Loop{
		Agent:  agent.Agent{Model: "fake", System: "use tools when helpful", Tools: []agent.Tool{upper}},
		Client: fake,
		Tools:  agent.NewToolset(upper),
	}
	r := runner.New(store, loop)
	if err := r.Start(ctx, "run-1", "uppercase the word drover"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := r.Wait(ctx, "run-1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	res, _ := r.Result(ctx, "run-1")
	fmt.Printf("run done in %d step(s): %s\n", res.Steps, res.Output)
}
