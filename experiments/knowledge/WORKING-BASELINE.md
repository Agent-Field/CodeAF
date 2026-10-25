# Known working baseline

Santosh approved commit **a7f167c** on 2026-10-25 as the working knowledge-base
prototype. Permanent Git tag: **knowledge-base-working-2026-10-25**. This tag points
to the existing working version and is not moved by later experiments.

The baseline includes sourced guides, precise paragraph claim dependencies,
source-basis/conflict cues, compact review and separate support/usefulness gates.
Its documented limitations remain part of the baseline; approval is not a claim
of complete capture, primary-source accuracy or production readiness.

Subsequent work stays on `knowledge-base`. Compare changes against this tag;
create a separate checkout at the tag when reproducing the approved version.
Do not reset the active checkout or overwrite private experiment artifacts.

# Binary integration checkpoint

The tag `knowledge-base-tui-working-2026-10-25` identifies the next working
checkpoint: opt-in project knowledge in Memory, a linked terminal reader, durable
future-chat capture and cited chat/child-worker retrieval. The earlier checkpoints
above remain intact. Build this binary with `make build` so it includes the pinned
execution engine.

Live compilation and chat verification used only
`deepseek/deepseek-v4.1-flash`; recorded fixture cost was $0.016322532, including
the repeated chat check. Unchanged compilation added zero requests. This is a
small development fixture, not a production per-turn cost estimate.

Repository quick checks, focused integration checks and race checks passed.
The full session package also exposes a shared-ledger budget test failure
reproduced unchanged at the pre-integration checkpoint; it is not reported as
an integration pass. Older installed-engine failures were resolved for the
experiment by building with the pinned engine, without replacing the installed
default. See `BINARY-INTEGRATION.md` for limits.
