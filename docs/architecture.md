<!--
Copyright 2026 Sylvester Francis
Licensed under the Apache License, Version 2.0. See the LICENSE file.
-->

# Architecture

drover is a durable, budgeted agent runner. It composes two systems and owns only
the orchestration between them: [rerun](https://github.com/sylvester-francis/rerun)
provides durable execution, [leash](https://github.com/sylvester-francis/leash)
provides spend governance. This document is the map: the layers, the data flow, and
how a job survives a crash and a budget.

## The three layers

```mermaid
flowchart TB
    job["an agent job: goal, tools, model"] --> loop
    subgraph drover["drover: orchestration only"]
      loop["plan, act, observe loop"]
    end
    loop -->|"each step is a Do()"| rerun["rerun<br/>durable execution<br/>crash, replay, resume"]
    loop -->|"every model call"| leash["leash proxy<br/>spend governance<br/>meter, refuse, stop"]
    rerun --> store[("Store<br/>SQLite or Postgres")]
    leash -->|"forwards"| api["model API<br/>OpenAI or Anthropic"]
```

- **rerun** makes the loop *durable*: every model and tool call is journaled, so a
  crash resumes at the in-flight step instead of restarting the job.
- **leash** makes it *bounded*: every model call goes through the proxy, which
  refuses the call when a spend budget trips.
- **drover** is the thin layer that turns an agent definition into a rerun workflow
  and points its model calls at leash. It stores nothing and governs nothing.

## The durable loop

The loop is a rerun workflow. Each iteration is a model call (a `Do`) and, when the
model asks to act, one `Do` per tool call:

```mermaid
flowchart TD
    start(["Start(runID, goal)"]) --> model["Do(model-N):<br/>call the model, via leash"]
    model --> refused{"leash refused<br/>on a budget?"}
    refused -->|"yes"| stopped(["Done: stopped, reason recorded"])
    refused -->|"no"| acting{"model asked<br/>for a tool?"}
    acting -->|"no, final answer"| done(["Done: output"])
    acting -->|"yes"| tool["Do(tool-N):<br/>run each tool, fold the result in"]
    tool --> model
```

drover holds **no state of its own**. The message history is a local variable
rebuilt from the journaled step results every time the workflow runs, so on
recovery it is refolded to exactly what it was. The journal, not drover, is the
source of truth for where a job is.

## Recovery and replay

If the process dies mid-job, the next `Recover()` re-runs the workflow. Completed
steps return their journaled results without executing; the first step past the end
of the journal runs live:

```mermaid
sequenceDiagram
    participant P as process
    participant R as runner
    participant J as journal (Store)
    P->>R: Start(goal)
    R->>J: Do(model-0), append result
    R->>J: Do(tool-0), append result
    Note over P,J: crash before model-1
    P->>R: Recover() on the next boot
    R-->>J: replay model-0, tool-0 (read from journal, not re-run)
    R->>J: Do(model-1), live, append, Done
```

Because rerun is **at-least-once** for side effects, a tool caught mid-flight by a
crash re-runs on recovery, which is why **every tool must be idempotent**. A
completed tool step, already in the journal, is never re-run. This is proven by a
crash-injection test in `runner`: a fault is injected at the journal-write
boundary, a fresh runner recovers over the same store, and the test asserts the
completed tool step replays without re-running while the un-journaled model step
re-executes.

## The governor seam

drover reads leash's verdict straight off the wire and turns it into a value the
loop branches on. This lives in `provider`, shared by every model client:

```mermaid
flowchart LR
    resp["leash response"] --> code{"status"}
    code -->|"2xx"| ok["decode, then act or answer"]
    code -->|"429 + Retry-After"| rate["rate limit:<br/>durable Sleep, then retry"]
    code -->|"429, no Retry-After"| stop["budget boundary:<br/>stop the run (Done)"]
    code -->|"5xx or network"| retry["transient:<br/>retry with backoff"]
```

The `429`-with-`Retry-After` versus `429`-without distinction is how leash
separates transient backpressure from a terminal stop. A terminal stop finishes the
run `Done` with the reason recorded, not `Failed`, because the governor doing its
job is not an agent failure. The `examples/e2e` demo shows both paths end to end.

## Determinism: branch on values, not error types

rerun matches journaled steps to code by position and tag, so the workflow must be
deterministic. One subtle trap, specific to building on rerun: a step's return
*value* survives replay, but its error's concrete *type* does not. A replayed
failure is always a `*rerun.StepError` carrying the message only. So drover encodes
every control-flow decision (a governor stop, a rate limit) as a **value** on
`model.Response` (`Stopped`, `RetryAfter`) and branches on those, never on an error
type. Branching on `errors.As` or `errors.Is` inside the loop would pass live and
diverge on replay.
(See [ADR-0003](adr/0003-branch-on-values-not-error-types.md).)

## Package structure

Small, public packages, each with one job; dependencies point inward toward
`model`:

```mermaid
flowchart LR
    cmd["cmd/drover<br/>CLI"] --> runner
    runner["runner<br/>engine wiring"] --> agent
    tools["tools<br/>built-in tools"] --> agent
    agent["agent<br/>the loop"] --> model
    provider["provider<br/>model clients"] --> model["model<br/>types, Client"]
    runner -. "rerun.Store" .-> rerun[("rerun engine")]
    provider -. "HTTP" .-> leash[("leash proxy")]
```

| Package | Role |
|---|---|
| `model` | provider-agnostic chat and tool types; the `Client` interface. Governance outcomes ride as values here. |
| `agent` | `Agent`, `Tool`, `Toolset`, and the `Loop`: the plan/act/observe workflow. |
| `provider` | OpenAI and Anthropic clients plus an offline `Fake`; the governor seam (`base.send`). |
| `runner` | wires a `Loop` onto a rerun `Engine` over a `Store`: `Start`, `Recover`, `Wait`, `Result`. |
| `tools` | built-in idempotent tools. |
| `cmd/drover` | the CLI: `run` / `resume` / `version`. |

## What lives where

| Concern | Owner |
|---|---|
| Where a job is (its journal) | rerun `Store` (SQLite or Postgres) |
| Spend and budgets | the leash proxy |
| The conversation, the loop, tool dispatch | drover (`agent`) |
| Talking to a model | drover (`provider`), through leash |

drover's own store is separate from anything leash keeps: drover is just another
durable agent leash governs.

## See also

- [`docs/using-drover.md`](using-drover.md): the how-to guide.
- [`docs/adr/`](adr): the decisions behind the design.
- [`DESIGN.md`](../DESIGN.md): the one-page design contract.
- [`examples/e2e`](../examples/e2e): the whole stack running offline.
