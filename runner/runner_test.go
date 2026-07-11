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

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/drover/model"
	"github.com/sylvester-francis/rerun"
	"github.com/sylvester-francis/rerun/sqlite"
)

// fakeModel returns scripted responses, one per Complete call, and counts calls.
type fakeModel struct {
	mu    sync.Mutex
	steps []model.Response
	calls int
}

func (f *fakeModel) Complete(_ context.Context, _ model.Request) (model.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	if i >= len(f.steps) {
		return model.Response{}, fmt.Errorf("fakeModel: no scripted response for call %d", i)
	}
	return f.steps[i], nil
}

func (f *fakeModel) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// echoTool returns its "text" argument, so a scripted tool call has a checkable
// result folded back into the conversation.
func echoTool() agent.Tool {
	return agent.FuncTool{
		Def: model.ToolSchema{
			Name:        "echo",
			Description: "echoes its text argument",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		},
		Fn: func(_ context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			return a.Text, nil
		},
	}
}

func newLoop(client model.Client, tool agent.Tool) *agent.Loop {
	return &agent.Loop{
		Agent:  agent.Agent{Model: "fake", System: "be brief", Tools: []agent.Tool{tool}},
		Client: client,
		Tools:  agent.NewToolset(tool),
	}
}

// A job that calls one tool, folds the result, then answers, runs to Done with
// the expected result and exactly two model calls.
func TestRunner_ToolThenAnswer(t *testing.T) {
	store := sqlite.New(filepath.Join(t.TempDir(), "drover.db"))
	defer store.Close()

	fake := &fakeModel{steps: []model.Response{
		{
			ToolCalls: []model.ToolCall{{ID: "c1", Name: "echo", Args: json.RawMessage(`{"text":"hi there"}`)}},
			Usage:     model.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		},
		{
			Content: "the tool said: hi there",
			Usage:   model.Usage{InputTokens: 20, OutputTokens: 8, TotalTokens: 28},
		},
	}}
	r := New(store, newLoop(fake, echoTool()))

	ctx := context.Background()
	if err := r.Start(ctx, "run1", "say hi"); err != nil {
		t.Fatalf("start: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st, err := r.Wait(waitCtx, "run1")
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if st != rerun.Done {
		t.Fatalf("status = %v, want Done", st)
	}

	res, err := r.Result(ctx, "run1")
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.Output != "the tool said: hi there" {
		t.Fatalf("output = %q, want the tool answer", res.Output)
	}
	if res.Steps != 2 {
		t.Fatalf("steps = %d, want 2", res.Steps)
	}
	// The run total is the sum of both model calls' usage.
	if want := (model.Usage{InputTokens: 30, OutputTokens: 13, TotalTokens: 43}); res.Usage != want {
		t.Fatalf("usage = %+v, want %+v (summed across both model calls)", res.Usage, want)
	}
	if got := fake.callCount(); got != 2 {
		t.Fatalf("model calls = %d, want 2", got)
	}
}

// A run leash refuses on a boundary ends Done (not Failed) with the stop reason
// recorded; the budget doing its job is a clean termination.
func TestRunner_GovernorStopIsCleanDone(t *testing.T) {
	store := sqlite.New(filepath.Join(t.TempDir(), "drover.db"))
	defer store.Close()

	fake := &fakeModel{steps: []model.Response{
		{Stopped: "cost_budget"},
	}}
	r := New(store, newLoop(fake, echoTool()))

	ctx := context.Background()
	if err := r.Start(ctx, "run2", "spend a lot"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st, err := r.Wait(waitCtx, "run2")
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if st != rerun.Done {
		t.Fatalf("status = %v, want Done (a budget stop is clean)", st)
	}
	res, err := r.Result(ctx, "run2")
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.Stopped != "cost_budget" {
		t.Fatalf("stopped = %q, want cost_budget", res.Stopped)
	}
}

// flakyStore wraps a Store and fails one Append at a chosen seq, simulating a
// crash at the journal-write boundary. It closes injected when the failure fires,
// so a test can wait for it deterministically instead of racing a goroutine.
type flakyStore struct {
	rerun.Store
	failAt   int
	injected chan struct{}
	mu       sync.Mutex
	tripped  bool
}

func (s *flakyStore) Append(ctx context.Context, runID string, l rerun.Log) error {
	s.mu.Lock()
	if l.Seq == s.failAt && !s.tripped {
		s.tripped = true
		s.mu.Unlock()
		close(s.injected)
		return fmt.Errorf("flakyStore: injected append failure at seq %d", l.Seq)
	}
	s.mu.Unlock()
	return s.Store.Append(ctx, runID, l)
}

// A crash (a failed journal write) parks the run; a fresh runner over the same
// store recovers it, replaying completed steps from the journal without
// re-executing them. The tool runs exactly once across the crash (replay skips
// the completed step), while the un-journaled model step re-executes
// (at-least-once). These are the two durability properties, proven deterministically.
func TestRunner_ResumesAfterCrashSkippingCompletedSteps(t *testing.T) {
	base := sqlite.New(filepath.Join(t.TempDir(), "drover.db"))
	defer base.Close()
	flaky := &flakyStore{Store: base, failAt: 2, injected: make(chan struct{})}

	var echoCalls int64
	tool := agent.FuncTool{
		Def: model.ToolSchema{Name: "echo", Description: "echo", Parameters: json.RawMessage(`{"type":"object"}`)},
		Fn: func(_ context.Context, _ json.RawMessage) (string, error) {
			atomic.AddInt64(&echoCalls, 1)
			return "did the work", nil
		},
	}
	// One fake reused across the crash so its call cursor continues: seq 0 (tool
	// call), seq 2 first attempt (answer, not journaled), seq 2 on resume (answer,
	// journaled).
	fake := &fakeModel{steps: []model.Response{
		{ToolCalls: []model.ToolCall{{ID: "c1", Name: "echo", Args: json.RawMessage(`{}`)}}},
		{Content: "final answer"},
		{Content: "final answer"},
	}}

	// Phase 1: run until the injected failure at seq 2 parks the run.
	r1 := New(flaky, newLoop(fake, tool))
	if err := r1.Start(context.Background(), "job", "do it"); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-flaky.injected:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the injected crash")
	}
	if err := r1.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Phase 2: a fresh runner over the same store recovers and finishes the run.
	r2 := New(flaky, newLoop(fake, tool))
	if err := r2.Recover(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st, err := r2.Wait(waitCtx, "job")
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if st != rerun.Done {
		t.Fatalf("status = %v, want Done after resume", st)
	}
	res, err := r2.Result(context.Background(), "job")
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.Output != "final answer" {
		t.Fatalf("output = %q, want final answer", res.Output)
	}
	if got := atomic.LoadInt64(&echoCalls); got != 1 {
		t.Fatalf("tool ran %d times, want exactly 1 (replay must skip the completed tool step)", got)
	}
	if got := fake.callCount(); got != 3 {
		t.Fatalf("model called %d times, want 3 (seq0 once; seq2 re-executed at-least-once)", got)
	}
}
