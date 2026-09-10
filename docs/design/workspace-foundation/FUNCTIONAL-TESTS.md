# Real-model organization journeys

Run `make test-organization` for deterministic storage/runtime checks and
`make test-organization-live` for real DeepSeek V4 Flash journeys. The latter
builds `bin/aforge` using the normal build before executing tagged tests.

## Running and evidence

Requirements: Go, Git and `OPENROUTER_API_KEY`. No tmux or Docker is required.
The runner creates a disposable profile and each case creates its own temporary
home and work folders. It never uses the person's conversations or installs an
OS schedule. Optional learned memory is disabled. All text role/model rows are
pinned to `deepseek/deepseek-v4-flash`; ledger assertions check those pins.

```sh
make test-organization
make test-organization-live
ORGANIZATION_TEST_RUN='^TestOrganizationE2E$/api_contract$' make test-organization-live
```

`ORGANIZATION_TEST_LOG` selects the JSONL receipt path; otherwise the runner
prints a fresh temporary path. Missing credentials fail the explicit runner.
A skipped test or empty selection is not live verification. Ordinary tagged
package runs retain the existing harness's missing-credential skip convention.
Organization cases run sequentially because profile selection is process-wide.
The separate session-package completion probes may run concurrently; each package
has its own process and disposable home.

Receipts contain generated fixture IDs, tool arguments/results, structured
artifacts, model IDs, durations and spending. Assertions inspect stored revisions,
actual tool calls and files, not exact assistant prose or a second LLM's score.
`TestRealCompletionEvidence` separately exercises the actual completion reader on
controlled transcripts: a correct report, a wrong revision type, a wrong source,
and a failed write. These are live reader probes, not full user journeys. The
normal live runner always includes them, even with a narrowed organization case.
Each probe keeps the production reader deadline and checks a $0.05 spend ceiling.
For diagnosis alone: `go test -tags e2e -count=1 -run '^TestRealCompletionEvidence$' ./internal/session`.
The out-of-scope case observes the real outgoing provider request through a local
forwarding proxy; it does not replace the provider with a model stub. It also
reads and parses the unrelated chat's `scope.json`, requiring status
`unavailable`. Prompt isolation alone cannot establish a valid delivered file.

## Cases and limits of their claims

| Case | Functional evidence |
| --- | --- |
| `removed_scope_read` | A forced read by remembered ID stays globally readable but reports `applicable_here: false`; the completed turn does not restore current applicability or membership. |
| `working_day` | Twelve consumer chats, three simultaneous initial turns, eight additional file-review turns in one chat, revision while another stays observed idle, reopen, overlapping membership removal/rejoin, and withdrawal. |
| `conflicting_sources` | Two independently sourced dates remain contradictory while one conversation follows its person's local draft choice, including after reopen; another conversation still needs a choice. |
| `large_retrieval` | One requested reference among 31 records is outside the snapshot and first metadata page; its nonced answer requires continued text reads past 8,000 characters. |
| `binary_door` | The built binary creates a collection and files the current conversation through the production tool assembly. |
| `existing_owner_state` | A named collection resolves a real conversation, two task #1s in different sessions, and an existing ongoing item. A stale running index with no live owner is incomplete. |
| `task_workers` | Two actual `Agent.StartTask` workers on separate Git repositories read the same nonced source record and write separate artifacts with its identity, revision and source. The shared record stays unchanged. |
| `context_is_information` | A record containing an imperative is inspected and attributed; it does not authorize changing the calendar witness. |
| `two_bugs` | Two fix consumers use a shared cause; both refresh when its isolation boundary changes. |
| `api_contract` | Backend, frontend and documentation consumers use one response-contract record and refresh to its new revision. |
| `maintenance` | Planner and report consumers retain and refresh the allowed dependency-update level. |
| `research_marketing` | Brief and campaign consumers refresh an offline-support finding and stop reporting withdrawn context as current. |
| `travel_calendar` | Itinerary and calendar-impact reports detect a changed schedule conflict without changing the calendar witness. |

Each of the five domain cases creates shared context through the real model tool,
reads it in separate chats, revises the same identity, closes/reopens consumers,
and withdraws it before another reopen. An unlinked conversation's provider
request must omit the source nonce. These are parameterized exercises of one
mechanism, not five new domain frameworks.

The worker case uses the ordinary worker path. The five domain cases explicitly
resume consumers: they do not establish autonomous detection, collaboration,
scheduled-fire context, notification delivery or action deduplication. The
maintenance policy and resolved standing item do not prove a schedule executes
with that policy. Those remain later acceptance slices.

## Budget and automation

Conversation turns have a two-minute outer deadline and a $0.20 session rail;
the worker recording/read helper retains the existing six-minute turn bound.
Each task has a five-minute deadline; the two-worker case checks a $0.50 ledger
ceiling. Each domain checks a $0.75 ledger ceiling. The binary has a two-minute
deadline and $0.15 rail. Ledger ceilings detect excessive spending after the run;
they are not substitutes for the runtime rails. The suite has a 40-minute cap.

`.github/workflows/organization-live.yml` triggers for changes to the storage,
adapter, session engine, standing package, binary assembly or these tests and
also supports manual dispatch. It requires the repository's OpenRouter secret.
When unavailable, CI says **NOT RUN** and skips the paid job; that is not evidence
of a live pass. Credentials are never installed into GitHub by this change.
Current verified results belong in [HANDOFF.md](HANDOFF.md).

## Deeper regular-use checks

```sh
ORGANIZATION_TEST_RUN='^TestOrganizationE2E$/(working_day|conflicting_sources|large_retrieval|removed_scope_read)$' make test-organization-live
```

These are registered in the same default suite and existing path-filtered CI
workflow. Each retains the existing $0.75 case ceiling and normal turn/session
rails. The working-day case has 12 consumers plus a source chat, 33 real turns
when complete, and at most three simultaneous submitted turns. It uses synthetic
review documents read by the real model, not seeded assistant transcripts. It
asserts a journal exceeding 100 KB; it does not force context-window saturation
or claim that automatic compaction ran. The idle assertion covers the observed
revision-and-two-consumer interval only, using output bytes, turn count and a
wake subscription. It is not a perpetual no-wake proof.

Membership changes and the 31-record library are storage fixtures. The working-day
author creates, revises and withdraws through real model tools. The conflict case
separates source disagreement from a person's conversation-local draft choice; it
does not introduce a structured accepted-decision record. The retrieval case
requires real list/text pagination, correct provenance and a pinned read revision.
Display receipts can be truncated, so pagination checks use requested ranges and
the actual stored ordering, with the secret tail value checked independently.

Results and development failures are recorded in [COMPLEX-TESTS.md](COMPLEX-TESTS.md).
