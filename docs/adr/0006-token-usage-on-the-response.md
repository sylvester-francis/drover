# ADR-0006: Surface token usage on the response

Status: accepted

## Context

Every model provider reports the tokens a completion consumed: OpenAI as prompt,
completion, and total tokens; Anthropic as input and output tokens. drover's
provider clients decode the reply into `model.Response` but drop those counts
today, so a consumer that wants to display usage, price a run, or attach token
attributes to a trace has to re-read the raw provider wire format, which defeats
the point of the provider-agnostic seam.

drover already routes every call through the leash proxy, which governs spend. But
governing (refuse when over budget) and reporting (how many tokens did this call
use) are different concerns. leash decides whether a call proceeds; a consumer
holding the `Response` may still want the number for its own accounting, tracing,
or display, and should not have to ask a second service for a value the provider
already handed back. Reporting the count does not turn drover into a cost engine:
it carries a value it already received, and prices nothing.

## Decision

Add a provider-agnostic `model.Usage` (input, output, and total token counts) and
carry it as a value on `model.Response`, populated by each provider client from
the reply's usage block. Accumulate a run total on `agent.Result`, so a finished
run reports the tokens it consumed without the caller summing journal steps.

Usage is counts only. Pricing stays out of drover: turning tokens into money is
the caller's or leash's concern, against whatever rate table applies.

Like every other field on `Response`, `Usage` is a journaled value, so it
round-trips through rerun and replays deterministically (ADR-0003). A run recorded
before this change carries a zero usage, so the field is backward compatible.

## Consequences

- `model.Response` and `agent.Result` gain a `Usage` field, and `model.Usage` has
  an `Add` for accumulation. Consumers read tokens from one provider-agnostic
  value.
- Both shipped providers populate it: OpenAI maps prompt, completion, and total;
  Anthropic maps input and output and computes the total it does not send.
- The boundary holds (ADR-0001): drover reports counts, leash governs spend, rerun
  journals the value. No pricing and no storage are added here.
- A future provider fills `Usage` from its own reply; one that returns no usage
  leaves it zero, which is harmless. Usage is zero on a governor verdict, where no
  completion happened.

## Alternatives considered

- **Leave usage to leash.** leash sees the traffic and meters spend, but it is a
  separate proxy process. A consumer holding a `Response` should not have to call
  another service to learn the token count the provider already returned inline.
- **A separate accessor or callback.** More surface for no gain: the counts are
  part of a completion's result, so a value on the response is the natural home,
  and it journals and replays for free.
