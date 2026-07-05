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

// Command durable shows why an agent run survives a crash: every model and tool
// call is recorded in the journal, and a fresh runner over the same store sees
// the finished run. Had the first runner crashed mid-run, Recover() would replay
// these exact journaled steps instead of re-running them.
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

	db := filepath.Join(os.TempDir(), "drover-durable.db")
	_ = os.Remove(db)
	defer os.Remove(db)

	fake := &provider.Fake{Reply: func(model.Request) model.Response {
		return model.Response{Content: "done"}
	}}
	newLoop := func() *agent.Loop {
		return &agent.Loop{Agent: agent.Agent{Model: "fake"}, Client: fake, Tools: agent.NewToolset()}
	}

	// First process: run the job to completion.
	store1 := sqlite.New(db)
	r1 := runner.New(store1, newLoop())
	if err := r1.Start(ctx, "job-1", "do the thing"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := r1.Wait(ctx, "job-1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The journal it left behind — a restart replays these, never re-runs them.
	logs, _ := store1.LoadLogs(ctx, "job-1")
	fmt.Println("journal for job-1:")
	for _, l := range logs {
		fmt.Printf("  seq=%d tag=%q\n", l.Seq, l.Tag)
	}
	store1.Close()

	// A second process over the same store sees the finished run — the journal is
	// the source of truth, not any in-memory state. On a crash mid-run, its
	// Recover() would resume the job from exactly where the journal ends.
	store2 := sqlite.New(db)
	defer store2.Close()
	r2 := runner.New(store2, newLoop())
	if err := r2.Recover(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	res, err := r2.Result(ctx, "job-1")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("\na fresh runner read the result: %q (%d step(s))\n", res.Output, res.Steps)
}
