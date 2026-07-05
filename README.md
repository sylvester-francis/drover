# drover

Runs durable, multi-step agent jobs on [rerun](https://github.com/sylvester-francis/rerun),
with every model call governed by the [leash](https://github.com/sylvester-francis/leash)
proxy.

> Status: early scaffold. The durable runner is being built on rerun's execution
> engine. This repository is the home for that work; the interfaces below are the
> intended shape, not yet the implementation.

## The idea

An agent is a loop: plan, act, observe, repeat. Two things go wrong with that loop
in production, and each has an answer that already exists:

- **It is not durable.** A crash halfway through a twelve-step job restarts the
  whole job, repeating work and paying for it twice. rerun makes the loop a
  durable workflow, so a restart resumes at the step that was in flight.
- **It is not bounded.** Nothing stops a stuck loop from spending forever. leash
  is a proxy that meters token cost off the wire and refuses the next call the
  moment a budget trips.

drover is the consumer that composes both. It runs the agent loop as a rerun
workflow, `Do(call-model) -> Do(use-tool) -> Do(call-model)...`, and points every
model call at a leash proxy. rerun makes the run survive a crash; leash makes it
survive the invoice.

```
      drover  (orchestrates the durable agent loop)
      /     \
  rerun      leash
 (durable    (spend
 execution)   governance)
```

## What drover is, and is not

- drover **orchestrates**. It ships no persistence of its own (that is rerun's
  engine) and no governance of its own (that is the leash proxy). It composes the
  two into a durable, budgeted agent runner.
- Because governance is a proxy leash already provides, drover does not couple you
  to itself for spend control. leash governs any agent; drover is simply one that
  is durable by construction.

## Family

- [leash](https://github.com/sylvester-francis/leash) - the spend governor (a
  reverse proxy that stops a run when it trips a budget).
- [rerun](https://github.com/sylvester-francis/rerun) - the durable-execution
  engine (crash-safe, replay-on-recovery workflows).
- **drover** - the durable agent runner that composes both.

## License

Apache 2.0. See [LICENSE](LICENSE).
