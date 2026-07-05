# Examples

The first three are runnable and offline: they use drover's `fake` model, so there
is no API key and no network. To govern a real model, point a provider at a leash
proxy (see the top-level [README](../README.md)).

| Example | Shows |
|---|---|
| [quickstart](quickstart) | the durable loop, goal to answer, in ~30 lines |
| [customtool](customtool) | defining a `Tool` and running an agent that calls it |
| [durable](durable) | the journal a run leaves behind, and a fresh runner reading it |

```sh
go run ./examples/quickstart
go run ./examples/customtool
go run ./examples/durable
```

## End to end: leash governs drover

[`e2e/demo.sh`](e2e) is the whole stack, offline: a drover agent runs through a
`leash serve` gateway in front of a fake OpenAI upstream. leash meters each call
against a price table and stops the agent when its dollar budget trips (a terminal
`cost_budget`); when a rate limit trips, leash returns `429` with `Retry-After`, so
drover Sleeps durably and resumes (transient). No API key and no real model; leash
is pulled with `go install ...@latest`, so no leash checkout is needed.

```sh
./examples/e2e/demo.sh
```
