# ADR-0004: A leash budget stop ends the run cleanly

Status: accepted

## Context

Every model call goes through a leash proxy, which answers off the wire. leash
refuses a call with `429` when a boundary trips, but a `429` means two different
things: with a `Retry-After` header it is transient rate-limit backpressure (the
run is still alive); without one it is a terminal budget stop (the run is done).

## Decision

Classify leash's response in the provider client:

- `429` **with** `Retry-After` → rate-limit → a durable `Sleep`, then retry.
- `429` **without** `Retry-After` → a terminal boundary → **stop the run cleanly**,
  finishing `Done` with the reason recorded, not `Failed`.
- `5xx` / network → transient → retry with durable backoff.
- `2xx` → decode the reply.

A budget stop is a *clean* termination because the governor doing its job is not a
failure of the agent.

## Consequences

- A governed run ends `Done`; its `Result` carries the stop reason (e.g.
  `cost_budget`), so callers can tell "finished the task" from "stopped on budget".
- drover never retries a terminal refusal, and never marks a correctly-governed run
  as failed.
- The rate-limit vs terminal distinction lives in one place (`base.send`), shared
  by every provider.

## Alternatives considered

- **Treat any `429` as an error.** Rejected: it conflates backpressure with a
  terminal stop, retries a run leash meant to end, and reports a correctly-governed
  run as a failure.
