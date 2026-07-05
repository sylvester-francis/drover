# Examples

Runnable and offline — each uses drover's `fake` model, so there's no API key and
no network. To govern a real model, point a provider at a leash proxy (see the
top-level [README](../README.md)).

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
