# ADR-0002: The agent loop is a rerun workflow

Status: accepted

## Context

An agent is a loop: plan, act, observe, repeat. Each trip is expensive (a model
call costs tokens; a tool call takes a real action) and a job can run for many
steps. A crash halfway through must not restart the whole job: that repeats work
and pays for it twice.

## Decision

Run the loop as a rerun workflow. Every nondeterministic input (the model's
reply, a tool's result) is captured inside a `Do`, so each model call and tool
call becomes a journaled step. drover holds **no state of its own**: on recovery
the conversation history is refolded from the journal, which is the single source
of truth for where a job is.

## Consequences

- A crash resumes at the step that was in flight; steps that completed replay from
  the journal without re-running.
- Because rerun is **at-least-once** for side effects (a step re-runs if the
  process dies after its effect but before its journal entry commits), **every
  tool must be idempotent**. This is stated in the `Tool` contract.
- drover carries no checkpointing code and no second store to keep in sync.

## Alternatives considered

- **drover-side checkpointing** into its own state store. Rejected: it reinvents
  rerun, and two sources of truth (drover's state and rerun's journal) inevitably
  drift.
