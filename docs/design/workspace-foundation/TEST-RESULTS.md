# Backend verification — 2026-09-09

This record covers draft #662, not the broader autonomous personal-AI roadmap.
Production commit: `a216cdcf594aaec803d5e02f64e4185d6c9841a3`.
Test clarification: `6ce8be8bde54a06f8c36ecf9d7b29f1fba9a0892`.
No production changes followed that verified production commit in this wave.

## Real DeepSeek V4 Flash runs

The reviewed run exercised the real provider with learned memory disabled.
Eight scenarios passed. The information-only case failed an ambiguous boolean
assertion: the model distinguished a quoted imperative from an authoritative
instruction. The test now requests the literal quoted command and still asserts
source attribution, an unchanged calendar witness and absence of mutations.
That corrected case passed separately. **This is passing evidence for all nine
cases across two runs, not one completely green full-suite run.**

| Case | Result | Seconds |
| --- | --- | ---: |
| Built binary collection tools | Pass | 12.37 |
| Two ordinary task workers | Pass | 110.52 |
| Shared context is information | Corrected targeted run passed | 25.68 |
| Existing owners and distinct task addresses | Pass | 16.47 |
| Two bugs sharing a cause | Pass | 92.56 |
| API contract across three consumers | Pass | 149.93 |
| Maintenance policy | Pass | 128.69 |
| Research and marketing | Pass | 122.06 |
| Travel and calendar impact | Pass | 93.80 |

The five domain receipts report spending of $0.026979, $0.027783, $0.025069,
$0.028797 and $0.014436 respectively. The binary receipt is $0.001595. These
are observed costs, not promised future costs or a total for all development runs.

Local diagnostic receipts on the implementation machine:

- `/tmp/af-organization-reviewed-live.jsonl`: all nine original case results.
- `/tmp/af-organization-information-confirm.jsonl`: corrected information case.

These temporary files are diagnostic convenience, not required test fixtures.
A fresh checkout can reproduce the journeys using `make test-organization-live`;
see [FUNCTIONAL-TESTS.md](FUNCTIONAL-TESTS.md). No private source data, machine-only
fixture or copied model response is required. CI uploads its own JSONL receipts
when the live job runs.

## Deterministic regression and boundaries

The complete touched-package regression passed in a clean, isolated checkout of
`a216cdcf5`, while live model work ran separately:

```sh
make test PKGS='./internal/workspace ./internal/workspaceview ./internal/session ./cmd/aforge ./internal/manual ./internal/e2e' TEST_FLAGS='-count=1'
```

Workspace, adapter, session, command, manual and E2E packages all passed; the
session package took 268 seconds. The command uses the existing known-red ledger
unchanged. Earlier lifecycle deadline failures were investigated with five clean
baseline repetitions and a complete isolated regression; both passed. No test was
added to the known-red ledger and no deadline was relaxed to conceal a failure.
A separate initial run also caught checkout edits during testing, so subsequent
full regressions used a detached verification worktree that was not edited.

Additional passing checks: focused organization tests, storage/adapter race tests,
focused session race tests, structural laws, vet, packed manual, changelog
validation, tagged test compilation and the normal build/size gate. The explicit
live runner rejects missing credentials and an empty test selection instead of
reporting either as a pass.

## Fixes driven by functional evidence

- A context revision must explicitly supply applicability. An omitted targets
  field is refused; an explicit empty array intentionally removes applicability.
- Collection name lookup is an explicit operation. A supplied name cannot be
  silently ignored and mistaken for an empty membership lookup.
- The conversation completion reader receives the same bounded current context
  as its turn, so it cannot demand an obsolete revision from an older digest.
- Tool guidance identifies the current conversation default, and artifact
  instructions explicitly require writing the inspected file.

Fixtures were also corrected to create real conversation transcripts, valid
ongoing-item rails, explicit worker membership and a valid forwarding-proxy Host.
Fixture failures are not evidence that the production capability worked before
those corrections.

## What this does not verify

No claim is made for autonomous consultation, unlinked semantic discovery,
scheduled firings consuming context, learning/adoption, notifications or duplicate
activation prevention. Domain consumers are explicitly resumed by the test.
The task completion checker is not the conversation completion reader; it still
uses its existing acceptance/file evidence path without the new context tools.

The repository currently lacks `OPENROUTER_API_KEY`, so the installed paid CI job
reports **NOT RUN**. Its skip is not live verification; the local runs above are.
The branch remains a draft review branch and must not be merged without the user
changing that instruction.
