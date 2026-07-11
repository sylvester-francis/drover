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

package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/sylvester-francis/drover/model"
	"github.com/sylvester-francis/rerun"
)

// StartInput is a run's seed: the task to accomplish. rerun journals it at Start,
// so recovery reads the same goal from the journal rather than from a caller that
// is long gone.
type StartInput struct {
	Goal string `json:"goal"`
}

// Result is the terminal record of a run, written once with rerun.Return and read
// back with rerun.Result. Stopped is set when leash ended the run on a boundary
// (or the step cap tripped); Output holds the agent's final answer otherwise.
// Usage is the token total the run consumed across its model calls.
type Result struct {
	Output  string      `json:"output,omitempty"`
	Stopped string      `json:"stopped,omitempty"`
	Steps   int         `json:"steps"`
	Usage   model.Usage `json:"usage,omitempty"`
}

// Loop is the durable agent loop: an Agent, the model Client that drives it, and
// the Toolset it may call. Its Run method is a rerun workflow; register it with
// Engine.Handle and every model call and tool call becomes a journaled step.
type Loop struct {
	Agent  Agent
	Client model.Client
	Tools  *Toolset
}

// maxModelAttempts bounds transient/rate-limit retries per model call before the
// run fails. leash bounds total spend; this bounds thrash on a single call.
const maxModelAttempts = 6

// Run executes the plan/act/observe loop as a rerun workflow.
//
// Determinism rule: every nondeterministic input (the model's reply, a tool's
// result, whether leash refused the call) is captured inside a Do, so replay
// reproduces the run from the journal without re-calling the model or re-running a
// completed tool. The loop holds no state of its own: on recovery the message
// history below is refolded from the journaled step results, exactly as it was.
func (l *Loop) Run(w *rerun.W) error {
	in, err := rerun.Input[StartInput](w)
	if err != nil {
		return err
	}

	msgs := []model.Message{
		{Role: model.System, Content: l.Agent.System},
		{Role: model.User, Content: in.Goal},
	}
	maxSteps := l.Agent.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	// usage accumulates the tokens each model call reports. It sums journaled
	// Response values, so replay reproduces the same total.
	var usage model.Usage
	for step := 0; step < maxSteps; step++ {
		resp, err := l.callModel(w, step, msgs)
		if err != nil {
			return err
		}
		usage = usage.Add(resp.Usage)
		if resp.Stopped != "" {
			// leash ended the run on a boundary. That is the budget doing its job,
			// not a failure: record it and finish cleanly (Done, not Failed).
			rerun.Return(w, Result{Stopped: resp.Stopped, Steps: step + 1, Usage: usage})
			return nil
		}

		msgs = append(msgs, model.Message{
			Role:      model.Assistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if !resp.Acting() {
			// A final answer with no tool calls: the agent is done.
			rerun.Return(w, Result{Output: resp.Content, Steps: step + 1, Usage: usage})
			return nil
		}

		for i, call := range resp.ToolCalls {
			out, err := l.callTool(w, step, i, call)
			if err != nil {
				return err
			}
			msgs = append(msgs, model.Message{
				Role:       model.Tool,
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    out,
			})
		}
	}

	rerun.Return(w, Result{Stopped: "max_steps", Steps: maxSteps, Usage: usage})
	return nil
}

// callModel runs one model call as a journaled step, retrying transient failures
// and honoring a leash rate-limit with a durable Sleep. A governance stop and a
// rate-limit both arrive as VALUES on the Response (Stopped / RetryAfter), never
// as an error type: rerun preserves a step's value across replay but not its
// error's concrete type, so branching on an error type would diverge on recovery.
// Only the PRESENCE of an error (a transient failure) is branched on here.
func (l *Loop) callModel(w *rerun.W, step int, msgs []model.Message) (model.Response, error) {
	req := model.Request{Model: l.Agent.Model, Messages: msgs, Tools: l.Tools.Schemas()}
	for attempt := 0; attempt < maxModelAttempts; attempt++ {
		tag := fmt.Sprintf("model-%d-%d", step, attempt)
		resp, err := rerun.Do(w, tag, func(ctx context.Context) (model.Response, error) {
			return l.Client.Complete(ctx, req)
		})
		if err != nil {
			// Transient failure (journaled). Back off durably, then retry.
			if serr := rerun.Sleep(w, backoff(attempt)); serr != nil {
				return model.Response{}, serr
			}
			continue
		}
		if resp.RetryAfter > 0 {
			// leash rate-limit: wait the window durably, then retry.
			if serr := rerun.Sleep(w, resp.RetryAfter); serr != nil {
				return model.Response{}, serr
			}
			continue
		}
		return resp, nil
	}
	return model.Response{}, fmt.Errorf("drover: model call failed after %d attempts at step %d", maxModelAttempts, step)
}

// callTool runs one tool call as a journaled step. A tool error (or an unknown
// tool) is folded back to the model as an observation string, not returned as a
// run failure: a tool that fails is part of the agent's world for it to react to,
// not a crash of the runner.
func (l *Loop) callTool(w *rerun.W, step, i int, call model.ToolCall) (string, error) {
	tag := fmt.Sprintf("tool-%d-%d", step, i)
	return rerun.Do(w, tag, func(ctx context.Context) (string, error) {
		t, ok := l.Tools.Lookup(call.Name)
		if !ok {
			return fmt.Sprintf("error: no such tool %q", call.Name), nil
		}
		out, err := t.Invoke(ctx, call.Args)
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		return out, nil
	})
}

// backoff is the durable wait before retrying a transient model failure: doubling
// from one second, capped at thirty. It depends only on the attempt number, so
// replay reproduces the same waits.
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}
