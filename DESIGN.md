# Design

drover composes two existing systems into a durable, budgeted agent runner. It
adds orchestration and nothing else: rerun owns durability, leash owns
governance.

## The shape

The agent loop runs as a rerun workflow. Each step is a `Do`, so a crash resumes
at the step that was in flight rather than restarting the job:

```
Do(plan)      -> a model call, routed through the leash proxy
Do(act)       -> a tool call
Do(observe)   -> fold the result back into the agent's state
... repeat until the agent is done or leash refuses a call
```

Every model call is made against a leash proxy URL, so the whole job runs under a
hard spend budget. When leash trips a boundary, the model call returns a refusal;
the agent loop ends the way any loop ends when its next call fails. drover does
not re-implement any of that: it points the calls at leash and lets the boundary
do the work.

## Boundaries (what drover does not own)

- **Durability** is rerun's. drover does not persist state itself; the workflow
  journal is the source of truth for where a job is.
- **Governance** is leash's. drover does not meter or cap spend; it runs its model
  calls through the proxy. This keeps leash's "governs any agent" property intact:
  drover is one durable agent among many that leash can govern, not a special
  case.
- drover owns only the orchestration: turning an agent definition into a rerun
  workflow, wiring the model client at the leash proxy, and surfacing progress.

## Work split (two-agent build)

- **rerun side** owns the rerun-native runner: the workflow encoding of the agent
  loop, the `Do`/`Retry`/`Recover` usage, and the engine wiring. This is where the
  real durability lives, and it is being built here as the flagship consumer that
  will surface anything the engine still needs for real agent loops.
- **leash side** is already done for drover's purposes: drover uses the leash
  proxy as-is, with no changes required. drover routes calls at it.

## Non-goals

- No provider SDK lock-in; drover speaks to whatever model endpoint the leash
  proxy fronts.
- No governance logic. No persistence layer of its own. No config framework beyond
  what running a job needs.
