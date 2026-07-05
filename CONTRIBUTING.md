# Contributing to drover

drover composes [rerun](https://github.com/sylvester-francis/rerun) (durability) and
[leash](https://github.com/sylvester-francis/leash) (governance) into a durable,
budgeted agent runner. It owns only orchestration, so the bar is: a change keeps
that boundary clean, and the tests prove the behavior.

## Prerequisites

- Go 1.25 or newer.

## The local bar

```sh
go build ./...
go vet ./...
go test -race ./...   # the whole suite, including the crash-injection resume test
gofmt -l .            # must print nothing
```

CI runs the same across a ubuntu and macOS matrix, plus `govulncheck`.

## Design rules

- **Orchestration only.** drover persists no state (that is rerun's journal) and
  governs no spend (that is the leash proxy). A change that adds a storage layer or
  budget logic belongs in rerun or leash, not here.
- **Branch on values, not error types.** rerun does not preserve an error's concrete
  type across replay, so control-flow decisions ride as values on `model.Response`,
  never as typed errors inspected with `errors.As` / `errors.Is`. See
  [ADR-0003](docs/adr/0003-branch-on-values-not-error-types.md).
- **Tools are idempotent.** rerun is at-least-once for side effects, so a tool may
  run again on recovery. A new tool must tolerate a second identical call.
- **Style.** No em dashes; use `:`, `;`, `,`, or restructure. Short names in tight
  scopes; comments explain *why*, not *what*; `context.Context` first on anything
  that blocks or does I/O.

## Adding a tool or a provider

- A tool implements `agent.Tool` (or wraps a function with `agent.FuncTool`) and must
  be idempotent.
- A model provider implements `model.Client` and points at a leash proxy; reuse the
  governor seam in `provider` (`base.send`) so the 429 handling stays in one place.

## Pull requests

Keep them focused. Explain what changed and why, and confirm the local bar is green.
By contributing you agree your work is licensed under Apache-2.0.
