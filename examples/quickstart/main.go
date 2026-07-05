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

// Command quickstart runs a one-shot agent offline with the fake model (no API
// key and no network), showing the durable loop from goal to answer.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/drover/model"
	"github.com/sylvester-francis/drover/provider"
	"github.com/sylvester-francis/drover/runner"
	"github.com/sylvester-francis/rerun/sqlite"
)

func main() {
	ctx := context.Background()

	db := filepath.Join(os.TempDir(), "drover-quickstart.db")
	defer os.Remove(db)
	store := sqlite.New(db)
	defer store.Close()

	loop := &agent.Loop{
		Agent: agent.Agent{Model: "fake", System: "be brief"},
		Client: &provider.Fake{Reply: func(model.Request) model.Response {
			return model.Response{Content: "hello from drover"}
		}},
		Tools: agent.NewToolset(),
	}

	r := runner.New(store, loop)
	if err := r.Start(ctx, "run-1", "say hello"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := r.Wait(ctx, "run-1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	res, err := r.Result(ctx, "run-1")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("run done in %d step(s): %s\n", res.Steps, res.Output)
}
