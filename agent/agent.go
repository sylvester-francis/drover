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

// Agent defines a durable job's capability: which model to drive, how to instruct
// it, and the tools it may use. The task itself (the goal) is per-run and passed
// at Start (journaled as the run's seed), not baked into the Agent — one Agent
// definition runs many goals.
//
// An Agent is defined in Go, deliberately: drover ships extensible interfaces
// (Tool, model.Client), not a config DSL. Wiring a job is writing a little Go,
// which keeps the surface small and auditable.
type Agent struct {
	// Model is the provider model id, passed through to whatever endpoint the
	// leash proxy fronts (e.g. "gpt-4o", "claude-sonnet-4").
	Model string

	// System is the instruction preamble sent as the first System message.
	System string

	// Tools is the set the agent may call. It is fixed for the life of a run:
	// the advertised schemas are part of every model request, and replay must
	// reproduce identical requests, so the toolset must not change between the
	// original run and its recovery.
	Tools []Tool

	// MaxSteps bounds the plan/act loop so a model that never finishes cannot
	// spin forever on drover's side (leash bounds it on spend; this bounds it on
	// steps). Zero uses DefaultMaxSteps.
	MaxSteps int
}

// DefaultMaxSteps caps the loop when Agent.MaxSteps is unset. It is a safety
// backstop, not a budget: real spend control is leash's job.
const DefaultMaxSteps = 50
