# ADR-0005: A Go framework, not a config DSL

Status: accepted

## Context

drover is both a CLI (`drover run …`) and a library other code extends with custom
tools and agents. "Extensible" could mean a configuration language (define agents
in YAML/JSON) or a code-level plugin surface (define them in Go).

## Decision

The framework packages (`agent`, `model`, `provider`, `runner`, `tools`) are
**public and importable**, and agents and tools are defined in **Go**. There is no
config DSL. The CLI ships a default agent; power users compose their own in a
little Go against the same interfaces.

## Consequences

- A real plugin surface: a `Tool` is one small interface, a `model.Client` another,
  and `agent.Loop` composes them.
- Nothing to design, document, and secure as a separate language; the type system
  does the validation a DSL would reinvent.
- The packages are public (not `internal/`), so the API is a committed surface,
  appropriate at `v0.x`, where it may still change.

## Alternatives considered

- **A YAML/JSON agent-definition format.** Rejected: a config DSL is a language you
  must design, version, and secure, and it always grows toward a worse programming
  language. Go is already expressive, typed, and testable. (This is the "no config
  framework beyond what running a job needs" non-goal in `DESIGN.md`.)
