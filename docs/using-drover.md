<!--
Copyright 2026 Sylvester Francis
Licensed under the Apache License, Version 2.0. See the LICENSE file.
-->

# Using drover

A practical guide to running durable, budgeted agents with drover. For the *why*
and the internals, read [`architecture.md`](architecture.md); this guide is the
*how*.

The whole model in one sentence: **an agent is a plan/act/observe loop run as a
rerun workflow, with every model call routed through a leash proxy, so the job
survives a crash and stays under a budget.**

---

## 1. Install

```sh
go install github.com/sylvester-francis/drover/cmd/drover@latest
```

The core packages need Go 1.25 (the bundled rerun SQLite backend sets the floor).

---

## 2. Run your first agent

The offline `fake` model exercises the whole durable loop with no API key and no
network:

```sh
drover run --provider fake --goal "say hello"
```

drover prints each step as it is journaled and then the result. To govern a real
model, put a leash proxy in front of it and point drover at the proxy:

```sh
# leash is metering spend on :8080, fronting OpenAI
drover run \
  --provider openai --model gpt-5.5 \
  --leash-url http://127.0.0.1:8080 \
  --goal "fetch example.com and summarize it"
```

The run id doubles as leash's `X-Loop-Id`, so a job and its budget are the same
thing. See [`examples/e2e`](../examples/e2e) for the whole stack running offline.

---

## 3. Define an agent in Go

drover ships extensible interfaces, not a config language. An `Agent` is the model,
a system prompt, and the tools it may call; the `runner` wires the loop onto a rerun
`Store`:

```go
import (
	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/drover/provider"
	"github.com/sylvester-francis/drover/runner"
	"github.com/sylvester-francis/rerun/sqlite"
)

loop := &agent.Loop{
	Agent:  agent.Agent{Model: "gpt-5.5", System: "be concise", Tools: []agent.Tool{myTool}},
	Client: provider.NewOpenAI(provider.Config{BaseURL: leashURL, RunID: id}),
	Tools:  agent.NewToolset(myTool),
}

r := runner.New(sqlite.New("drover.db"), loop)
r.Recover(ctx)                     // resume anything a restart left in flight
r.Start(ctx, id, "the task goal")  // launch a new job (returns immediately)
status, _ := r.Wait(ctx, id)       // block until it finishes
result, _ := r.Result(ctx, id)     // read the final Result
```

`Agent.MaxSteps` bounds the loop (default 50) so a model that never finishes cannot
spin forever on drover's side; leash bounds it on spend, this bounds it on steps.

---

## 4. Tools are idempotent

A `Tool` is one small interface:

```go
type Tool interface {
	Schema() model.ToolSchema
	Invoke(ctx context.Context, args json.RawMessage) (string, error)
}
```

`Schema` advertises the tool to the model; `Invoke` runs it and returns a string
that is folded back into the conversation. A tool **must be idempotent**: rerun is
at-least-once for side effects, so a tool caught mid-flight by a crash runs again on
recovery. Write tools so a second identical call is harmless (natural keys, upserts,
"create if absent"), exactly as a production rerun step is written. `FuncTool`
adapts a plain function when you do not need a new type:

```go
tool := agent.FuncTool{
	Def: model.ToolSchema{Name: "uppercase", Description: "Uppercase text.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)},
	Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
		var a struct{ Text string `json:"text"` }
		_ = json.Unmarshal(args, &a)
		return strings.ToUpper(a.Text), nil
	},
}
```

An error from a tool is folded back to the model as an observation, not returned as
a run failure: a tool that fails is part of the agent's world for it to react to,
not a crash of the runner.

---

## 5. Providers and the governor seam

drover speaks to several providers behind one `model.Client` interface. OpenAI,
Google Gemini, and a local Ollama share the OpenAI chat-completions wire (Gemini and
Ollama expose OpenAI-compatible endpoints); Anthropic has its own; `fake` is offline.
Model ids move fast, so use a current one:

| `--provider` | Example model | Key |
|---|---|---|
| `openai` | `gpt-5.5` | `OPENAI_API_KEY` |
| `anthropic` | `claude-sonnet-5` | `ANTHROPIC_API_KEY` |
| `gemini` | `gemini-3-pro` | `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) |
| `ollama` | `llama3.2` | none (local) |

With `--leash-url` set, calls route through a leash proxy for governance; without it,
drover talks to the provider's own endpoint directly (ungoverned). Either way, drover
reads the verdict off the wire:

| leash responds | drover does |
|---|---|
| `200` | decode the reply, then act or answer |
| `429` **with** `Retry-After` | rate-limit backpressure: a durable `Sleep`, then retry |
| `429` **without** `Retry-After` | a budget boundary: **stop the run cleanly** (`Done`, not failed) |
| `5xx` / network | transient: retry with durable backoff |

A budget stop is a clean termination: the run finishes `Done` with the reason in
`Result.Stopped` (for example `cost_budget`), so you can tell "finished the task"
from "stopped on budget".

---

## 6. The CLI

```
drover run     [flags]   start a new agent job and run it to completion
drover resume  [flags]   resume jobs a restart left in flight
drover version           print the version
```

Common flags (each also reads a `DROVER_`-prefixed environment variable):

| Flag | Meaning |
|---|---|
| `--goal` | the task for the agent (required for `run`) |
| `--provider` | `openai`, `anthropic`, `gemini`, `ollama`, or `fake` |
| `--model` | the model id (required for openai/anthropic/gemini/ollama) |
| `--leash-url` | route calls through a leash proxy for governance (empty talks to the provider directly) |
| `--db` | the durable store path (SQLite) |
| `--run` | the run id; generated if empty, and used as leash's `X-Loop-Id` |
| `--api-key` | the provider key, or set `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` |
| `--leash-token` | `X-Leash-Token`, if the proxy requires auth |
| `--max-steps` | loop bound (0 uses the default) |

---

## 7. Durability and resume

Every model and tool call is a journaled `Do`, so a crash resumes at the step that
was in flight. If a job is interrupted, resume it by run id: completed steps replay
from the journal without re-running, and the agent continues where it left off.

```sh
drover resume --run <run-id> --db drover.db --provider openai --model gpt-5.5 --leash-url http://127.0.0.1:8080
```

Resume rebuilds the same runner (pass the same provider, model, and tools), then
calls `Recover`. An already-finished run just has its result read back.

---

## 8. The one rule: branch on values, not error types

The workflow must be deterministic: it must issue the same steps with the same tags
every run. drover keeps to that, and there is one trap worth knowing if you write
custom steps. rerun preserves a step's return *value* across replay, but a replayed
error comes back as a generic `*rerun.StepError`, not its original type. So never
branch on an error's type or sentinel inside the loop (`errors.As` / `errors.Is`);
that check passes live and fails on replay. Branch on *whether* a step errored, and
capture any "why it failed" decision as a value. drover follows this by carrying a
governor stop and a rate limit as values on `model.Response`.

---

## 9. Choosing a store

Persistence is rerun's `Store` seam; the loop is identical against any of them:

| Backend | Import | Use it for |
|---|---|---|
| SQLite | `sqlite.New("drover.db")` | one node; pure Go, no CGO, a single file |
| Postgres | `postgres.New(dsn)` | multiple processes or machines sharing runs |

drover's store is separate from anything leash keeps: drover is just another durable
agent that leash governs.

---

## See also

- [`architecture.md`](architecture.md): the layers, data flow, and recovery model.
- [`docs/adr/`](adr): the decisions behind the design.
- [`examples/`](../examples): runnable examples, including the offline end-to-end
  stack.
