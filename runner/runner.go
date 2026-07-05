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

// Package runner wires an agent Loop onto a rerun Engine: it registers the
// workflow, starts and recovers runs, and lets a caller wait for one to finish.
// It owns no durability of its own (the Engine's store is the source of truth)
// and no governance (that is the leash proxy the model client points at).
package runner

import (
	"context"
	"sync"

	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/rerun"
)

// workflowName is the single workflow every drover run is registered under.
const workflowName = "agent"

// Runner drives durable agent jobs on one store. It doubles as the Engine's
// Observer so a caller can Wait for a specific run to reach a terminal status.
type Runner struct {
	eng    *rerun.Engine
	onStep func(runID string, l rerun.Log)

	mu       sync.Mutex
	finished map[string]rerun.Status
	waiters  map[string]chan rerun.Status
}

// Option configures a Runner at construction.
type Option func(*Runner)

// WithStepLogger installs a callback invoked as each live step is journaled,
// which the CLI uses to surface progress. It fires only for live execution, not
// replay (a resumed run does not re-announce steps it already completed).
func WithStepLogger(f func(runID string, l rerun.Log)) Option {
	return func(r *Runner) { r.onStep = f }
}

// New builds a Runner over store, registering loop as the agent workflow. The
// Runner installs itself as the Engine's Observer to track completions.
func New(store rerun.Store, loop *agent.Loop, opts ...Option) *Runner {
	r := &Runner{
		finished: make(map[string]rerun.Status),
		waiters:  make(map[string]chan rerun.Status),
	}
	for _, o := range opts {
		o(r)
	}
	r.eng = rerun.New(store, rerun.WithObserver(r))
	r.eng.Handle(workflowName, loop.Run)
	return r
}

// Start launches a new job for goal under runID. It returns as soon as the run is
// durably created; use Wait to block until it finishes. runID doubles as the
// leash X-Loop-Id the model client sends, so the job is governed as one run.
func (r *Runner) Start(ctx context.Context, runID, goal string) error {
	return r.eng.Start(ctx, workflowName, runID, agent.StartInput{Goal: goal})
}

// Recover relaunches every job a restart left in flight, resuming each from its
// journal. Call it once on startup.
func (r *Runner) Recover(ctx context.Context) error { return r.eng.Recover(ctx) }

// Shutdown parks in-flight jobs (they resume on the next Recover) and waits for
// their goroutines to return or ctx to expire.
func (r *Runner) Shutdown(ctx context.Context) error { return r.eng.Shutdown(ctx) }

// Result reads the terminal Result a finished run recorded.
func (r *Runner) Result(ctx context.Context, runID string) (agent.Result, error) {
	return rerun.Result[agent.Result](ctx, r.eng, runID)
}

// Wait blocks until runID reaches a terminal status, or ctx expires. It is
// race-free against a run that finishes before Wait is called: OnFinish records
// the status, and Wait checks that record before parking on a channel.
func (r *Runner) Wait(ctx context.Context, runID string) (rerun.Status, error) {
	r.mu.Lock()
	if s, ok := r.finished[runID]; ok {
		r.mu.Unlock()
		return s, nil
	}
	ch := make(chan rerun.Status, 1)
	r.waiters[runID] = ch
	r.mu.Unlock()

	select {
	case s := <-ch:
		return s, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// OnStart satisfies rerun.Observer; drover tracks completion, not starts.
func (r *Runner) OnStart(rerun.Run) {}

// OnStep satisfies rerun.Observer, forwarding to the optional step logger.
func (r *Runner) OnStep(runID string, l rerun.Log) {
	if r.onStep != nil {
		r.onStep(runID, l)
	}
}

// OnFinish records a run's terminal status and wakes any waiter.
func (r *Runner) OnFinish(runID string, s rerun.Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished[runID] = s
	if ch, ok := r.waiters[runID]; ok {
		ch <- s // buffered (cap 1); never blocks
	}
}
