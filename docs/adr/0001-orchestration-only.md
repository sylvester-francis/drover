# ADR-0001: drover owns only orchestration

Status: accepted

## Context

An agent runner has to solve three problems: run a plan/act/observe loop, make
that loop survive a crash, and stop it from spending without bound. The middle
and last problems are already solved well by two systems drover can build on:
[rerun](https://github.com/sylvester-francis/rerun) (durable execution) and
[leash](https://github.com/sylvester-francis/leash) (spend governance). The
question is how much drover should own.

## Decision

drover owns **only orchestration**: turning an agent definition into a rerun
workflow, wiring the model client at a leash proxy, and surfacing progress. It
persists nothing itself (the workflow journal is the source of truth) and
governs nothing itself (every model call goes through the leash proxy).

## Consequences

- drover is **one durable agent among many that leash can govern**, not a special
  case. leash's "governs any agent" property stays intact.
- The surface stays small: no storage layer, no metering, no budget logic to test
  or secure.
- drover depends on rerun (engine) and speaks HTTP to a leash proxy; it does not
  depend on leash as a library.

## Alternatives considered

- **A monolith** that owns its own store and its own spend metering. Rejected: it
  duplicates two well-tested systems, couples governance to drover (throwing away
  leash's model-agnostic proxy story), and triples the surface a reviewer must
  trust.
