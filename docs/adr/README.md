# Architecture decision records

These record the decisions that shaped drover, and the alternatives rejected, so a
future reader (including a future maintainer) can see not just what drover does but
why it does it that way. Each is short. A decision that is later reversed is
superseded, not deleted.

Format: context, the decision, its consequences, and the alternatives weighed.

| # | Decision |
|---|---|
| [0001](0001-orchestration-only.md) | drover owns only orchestration; rerun persists, leash governs |
| [0002](0002-loop-is-a-rerun-workflow.md) | The agent loop is a rerun workflow; the journal is the only state |
| [0003](0003-branch-on-values-not-error-types.md) | Branch on journaled values, never on error types; they do not survive replay |
| [0004](0004-budget-stop-is-a-clean-completion.md) | A leash budget stop ends the run cleanly (Done), not failed |
| [0005](0005-framework-in-go-no-config-dsl.md) | The framework is public Go packages; agents are defined in code, not a DSL |
