# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). While the
version is `0.x` the public API may change between minor releases.

## [0.1.0] - 2026-07-04

The first release: a durable, budgeted agent runner. An agent's plan/act/observe
loop runs as a [rerun](https://github.com/sylvester-francis/rerun) workflow — every
model and tool call is journaled, so a crash resumes at the step that was in flight
instead of restarting the job — with every model call governed by the
[leash](https://github.com/sylvester-francis/leash) proxy. drover owns only
orchestration: no persistence of its own, no governance of its own.

### Added

- **`agent`** — the plan/act/observe loop as a rerun workflow, plus `Agent`,
  `Tool`, `FuncTool`, and `Toolset`. drover holds no state of its own; on recovery
  the conversation is refolded from the journal.
- **`model`** — provider-agnostic chat and tool types and the `Client` interface.
  Governance decisions ride as values (`Response.Stopped`, `Response.RetryAfter`),
  never error types, because rerun does not preserve an error's concrete type
  across replay.
- **`provider`** — OpenAI- and Anthropic-compatible clients plus an offline `Fake`,
  each pointed at a leash proxy. The governor seam classifies leash's reply: `429`
  with `Retry-After` → durable retry; `429` without → a clean budget stop; `5xx` →
  transient retry.
- **`runner`** — engine wiring over a rerun `Store`: `Start`, `Recover`, `Wait`,
  `Result`, and a step-progress hook.
- **`tools`** — the built-in idempotent `http_get`.
- **`cmd/drover`** — the `run` / `resume` / `version` CLI.
- **`examples/`** — `quickstart`, `customtool`, and `durable`, runnable offline
  with the fake model.
- **`docs/adr/`** — the five decisions behind drover.
- CI (a build/vet/test/`-race`/gofmt matrix), `govulncheck`, and a Pages landing
  site.

### Guarantees

drover inherits rerun's guarantees: runs are durable and resumable, and tool side
effects are at-least-once — so **tools must be idempotent**. A crash-injection test
proves it: a fresh runner recovers over the same store, replaying the completed
tool step without re-running it while the un-journaled model step re-executes.

[0.1.0]: https://github.com/sylvester-francis/drover/releases/tag/v0.1.0
