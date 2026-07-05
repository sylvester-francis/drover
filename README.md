<div align="center">

<pre>
██████╗ ██████╗  ██████╗ ██╗   ██╗███████╗██████╗ 
██╔══██╗██╔══██╗██╔═══██╗██║   ██║██╔════╝██╔══██╗
██║  ██║██████╔╝██║   ██║██║   ██║█████╗  ██████╔╝
██║  ██║██╔══██╗██║   ██║╚██╗ ██╔╝██╔══╝  ██╔══██╗
██████╔╝██║  ██║╚██████╔╝ ╚████╔╝ ███████╗██║  ██║
╚═════╝ ╚═╝  ╚═╝ ╚═════╝   ╚═══╝  ╚══════╝╚═╝  ╚═╝
</pre>

### Durable, budgeted agent runner &nbsp;·&nbsp; plan · act · observe · survive

**An agent is a loop. drover makes that loop survive a crash and stay under budget — by running it as a [rerun](https://github.com/sylvester-francis/rerun) workflow with every model call governed by the [leash](https://github.com/sylvester-francis/leash) proxy.**

[![CI](https://github.com/sylvester-francis/drover/actions/workflows/ci.yml/badge.svg)](https://github.com/sylvester-francis/drover/actions/workflows/ci.yml)
[![Go Reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/sylvester-francis/drover)
[![Go Version](https://img.shields.io/badge/go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
[![Status](https://img.shields.io/badge/status-v0.x%20unstable-orange)](#guarantees--non-goals)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

[**Design**](DESIGN.md) · [**rerun**](https://github.com/sylvester-francis/rerun) · [**leash**](https://github.com/sylvester-francis/leash)

*drover orchestrates. rerun makes it durable. leash makes it affordable.*

</div>

---

## The pitch

An agent is a loop: plan, act, observe, repeat. Two things go wrong with that loop in production, and each already has an answer:

- **It isn't durable.** A crash halfway through a twelve-step job restarts the whole job — repeating work and paying for it twice.
- **It isn't bounded.** Nothing stops a stuck loop from spending forever.

drover is the *consumer that composes both answers*. It runs the loop as a rerun workflow — `Do(plan) → Do(act) → Do(observe) → …` — and points every model call at a leash proxy. **rerun makes the run survive a crash; leash makes it survive the invoice.** drover adds nothing but orchestration: no persistence of its own, no governance of its own.

```
        drover  — orchestrates the durable agent loop
        /                                          \
     rerun                                        leash
     durable execution                          spend governance
     crash · replay · resume                    meter · refuse · stop
```

## How it works

The loop *is* a rerun workflow. Every nondeterministic input — the model's reply, a tool's result — is captured inside a `Do`, so a crash resumes at the step that was in flight instead of restarting the job:

```go
for step := 0; step < maxSteps; step++ {
    resp := Do("model-N", callModel)   // one completion, through the leash proxy
    if resp.Stopped != "" { break }    // leash tripped a budget → stop, cleanly
    if !resp.Acting()    { break }     // a final answer with no tool calls → done
    for each tool call in resp:
        Do("tool-N", invokeTool)       // a side effect, journaled once
}
```

drover holds **no state of its own**: on recovery the conversation history is refolded from the journal — rerun is the source of truth for where a job is. A step that completed replays from the journal without re-running; a tool that was mid-flight when the crash hit re-runs (rerun is at-least-once for side effects), which is why **every tool must be idempotent**.

> The durability isn't asserted, it's tested: a crash is injected at the journal-write boundary mid-run, a fresh runner recovers over the same store, and the suite checks that the completed tool step **replays without re-running** while the un-journaled model step **re-executes at-least-once**.

## Install

```sh
go install github.com/sylvester-francis/drover/cmd/drover@latest
```

## Quick start

Run an agent with **no API key and no network** — the offline `fake` model exercises the whole durable loop:

```sh
drover run --provider fake --goal "say hello"
```

Point it at a real model **through a leash proxy** to get governance for free:

```sh
# leash is governing spend on :8080, fronting OpenAI
drover run \
  --provider openai --model gpt-4o \
  --leash-url http://127.0.0.1:8080 \
  --goal "fetch example.com and summarize it"
```

If the process dies mid-run, **resume it** — completed steps replay from the journal and the agent picks up where it left off:

```sh
drover resume --run <run-id>
```

The run id doubles as leash's `X-Loop-Id`, so a job and its budget are the same thing across a crash.

## Define your own agent (in Go)

drover ships extensible interfaces, not a config DSL. An agent is a little Go:

```go
import (
	"github.com/sylvester-francis/drover/agent"
	"github.com/sylvester-francis/drover/provider"
	"github.com/sylvester-francis/drover/runner"
	"github.com/sylvester-francis/rerun/sqlite"
)

loop := &agent.Loop{
	Agent:  agent.Agent{Model: "gpt-4o", System: "…", Tools: []agent.Tool{myTool}},
	Client: provider.NewOpenAI(provider.Config{BaseURL: leashURL, RunID: id}),
	Tools:  agent.NewToolset(myTool),
}

r := runner.New(sqlite.New("drover.db"), loop)
r.Recover(ctx)         // resume anything a restart left mid-flight
r.Start(ctx, id, goal) // launch a new job (returns immediately; use r.Wait to block)
```

A `Tool` is one small interface — and it must be idempotent, because rerun may re-run it on recovery:

```go
type Tool interface {
	Schema() model.ToolSchema
	Invoke(ctx context.Context, args json.RawMessage) (string, error)
}
```

## The governor seam

drover speaks to whatever the leash proxy fronts (OpenAI or Anthropic) and reads leash's verdict straight off the wire:

| leash responds | drover does |
|---|---|
| `200` | decode the reply — act or answer |
| `429` **with** `Retry-After` | rate-limit backpressure → a durable `Sleep`, then retry |
| `429` **without** `Retry-After` | a budget boundary → **stop the run, cleanly** (`Done`, not failed) |
| `5xx` / network | transient → retry with durable backoff |

A budget stop is a *clean* termination: the run finishes `Done` with the reason recorded, because the governor doing its job is not a failure of the agent.

## What building this taught us about rerun

drover is rerun's flagship consumer, and a real agent loop surfaced one sharp edge worth knowing: **rerun preserves a step's return _value_ across replay, but not its error's concrete _type_** — a replayed error comes back as a generic `*StepError`. So branching on `errors.As(err, &SomeType)` inside a workflow silently diverges on recovery. drover encodes every decision — a governor stop, a rate-limit — as a **value** on the response, and branches only on error *presence*. If you build on rerun's engine: keep control flow branching on journaled values, never on error types.

## Repository layout

```
drover/
├── cmd/drover/     the CLI: run · resume · version
├── agent/          the plan/act/observe loop as a rerun workflow; Tool, Agent, Toolset
├── model/          provider-agnostic chat + tool types; the Client interface
├── provider/       OpenAI + Anthropic clients + an offline fake; the governor seam
├── runner/         engine wiring: Start, Recover, Wait
├── tools/          built-in idempotent tools (http_get)
└── DESIGN.md       the boundaries: drover orchestrates, rerun persists, leash governs
```

## Testing

```sh
go test -race ./...   # the whole suite, race-clean
go vet ./...
```

## Guarantees & non-goals

**drover is `v0.x` and unstable** — the API may change between minor versions.

**What it is:** an orchestrator that composes rerun (durability) and leash (governance) into a durable, budgeted agent runner.

**Non-goals** — deliberately not provided, because each belongs to a layer below:

- **No persistence of its own.** rerun's journal is the source of truth for where a job is.
- **No governance of its own.** leash meters and caps spend; drover just routes calls through the proxy. This keeps leash's "governs *any* agent" property intact — drover is one durable agent among many, not a special case.
- **No provider SDK lock-in.** drover speaks to whatever endpoint the leash proxy fronts.
- **No config DSL.** Agents are defined in Go.

## Family

- **[rerun](https://github.com/sylvester-francis/rerun)** — the durable-execution engine (crash-safe, replay-on-recovery workflows).
- **[leash](https://github.com/sylvester-francis/leash)** — the spend governor (a reverse proxy that stops a run when it trips a budget).
- **drover** — the durable agent runner that composes both.

## License

[Apache License 2.0](LICENSE) © 2026 Sylvester Francis
