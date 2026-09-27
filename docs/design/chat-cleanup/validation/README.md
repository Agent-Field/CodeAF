# Validation provenance

- Application source `18a6ec5f21386c1a2e1d5c6165b55a4804086c2d`: sustained live workflow and four live TUI scenarios passed in Fleet job001252. Its initial repository gate stopped on a help-search heading regression; that failure is preserved here.
- Follow-ups `d606fa413` and `ed8fed2bc`: restored the searchable Escape heading and corrected a stopped-versus-completed test fixture without changing application code. Existing reviewed #1619/#1622 test teardown fixes were reused in `561e1c5f1` after the first full run reproduced that known cleanup race. Full `make pr-ready` passes in Fleet job001257; see `pr-ready.log`.
- Independent generated-program checks: Fleet job001254, with exact outcome assertions in `../live-workflow/independent-verification.json`.
- The later artifact-only commit packages these results; its GitHub CI is linked in PR #1607.

Live model calls use DeepSeek v4.1 Flash through OpenRouter on Spark. Offline fake-provider tests remove inherited provider overrides and pin the Go build cache before temporary-home tests.

Original logs and terminal captures retain their whitespace, including blank terminal rows. They are preserved as evidence rather than reformatted.
