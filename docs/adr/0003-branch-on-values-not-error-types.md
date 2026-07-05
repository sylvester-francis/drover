# ADR-0003: Branch on journaled values, never on error types

Status: accepted

## Context

drover's loop must make decisions on how a model call turned out: did leash refuse
it for budget (stop), refuse it for rate (wait and retry), or fail transiently
(retry)? The obvious encoding is typed errors plus `errors.As`.

But rerun preserves a step's return **value** across replay and *not* its error's
concrete **type**: a replayed step failure comes back as a generic `*StepError`
carrying the message only. So `errors.As(err, &SomeType)` inside a workflow passes
on the live run and fails on replay — a determinism bug as real as a diverging
tag. (This surfaced building drover; it is now documented in rerun's `StepError`.)

## Decision

Encode every control-flow decision as a **value on the response**, not an error
type. `model.Response` carries `Stopped` (a terminal governor boundary) and
`RetryAfter` (rate-limit backpressure). The loop branches on those values and on
the mere *presence* of an error (a transient failure), never on an error's type or
sentinel.

## Consequences

- Replay is deterministic: the governor decision is a journaled value, reproduced
  exactly.
- Provider clients convert leash's wire responses into these values (see
  [ADR-0004](0004-budget-stop-is-a-clean-completion.md)); a transient failure is
  the only thing returned as an error.

## Alternatives considered

- **Typed errors + `errors.As`.** Rejected: it reads naturally but diverges on
  replay, exactly the silent corruption durable execution exists to prevent.
