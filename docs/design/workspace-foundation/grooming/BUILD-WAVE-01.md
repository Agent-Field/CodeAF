# First build: integrated foundation and safe control persistence

2026-09-10. Authorized by C19–C21 in [DECISIONS.md](DECISIONS.md).
This is an implementation contract for one functional slice, not the whole target.

## Outcome

Retain the useful organization/context foundation on a current-dev integration
candidate. A scheduled check or action finishing must not overwrite a successfully
saved pause, stop or configuration change made while it was running. Subsequent
ticks respect the current owner state. Existing records, history and grants remain
readable; no universal property store, new scheduler or UI redesign is introduced.

## Parallel ownership

| Lane | Isolated worktree / branch | Owns |
| --- | --- | --- |
| Integration | `af-personal-ai-integration` / `codex/personal-ai-integration` | Merge reviewed dev into retained #662 ancestry, resolve overlapping runtime seams, inspect automatic merges. |
| Lifecycle implementation | `af-standing-lifecycle` / `codex/standing-lifecycle` | Typed owner operations and safe runtime writeback in standing/session control paths; relevant manual changes. |
| Independent validation | `af-standing-validation` / `codex/standing-validation` | New deterministic functional tests reaching owner/control transitions and subsequent tick behavior. |
| Root | Grooming record and combined candidate | Confirm scope, review integration, apply lane commits, run combined focused Spark checks, publish draft and update checklist. |

Initial source is backend `c63e03b7fe8ee84b6b94befa5403002593af0ebe`.
Integration lane pins dev before editing; reviewed dev was `9961173140ba24ff99ef91c8e93fb79e326d854b`.
Standing owner source was identical at those revisions. Each lane reports its
exact input and result commits; root records the final combined revision.

## Contract

- File replacement remains atomic. Protect read/modify/write ownership, not just
  individual writes. Never hold the item's lock across a model/network action.
- Separate owner-controlled specification/control fields from runtime observation,
  count, spend and outcome updates. An old runtime snapshot cannot restore old
  control/configuration fields.
- Successful pause/stop remains effective after an already-started action returns.
  Stop prevents later admission. This slice does not promise rollback or immediate
  interruption of an irreversible action already underway.
- Quiet checks and failure handling obey the same preservation rule as successful
  firings. Runtime receipts retain what actually happened without fabricating
  successful delivery or resetting newer user intent.
- Preserve relevant concurrent changes such as grant narrowing, exceptions and
  execution configuration; stale whole-object saves must not undo them.
- Reuse existing schema/owners where possible; if revision fields are needed,
  make legacy behavior explicit and test old records. Do not manufacture an
  independent Work store solely to demonstrate composition.

## Functional acceptance for this iteration

Deterministic fixture runner callbacks impose the order without timing-sensitive
sleeps: ticker reads active state, callback saves a control/configuration change,
runner finishes, then another tick runs. Assert persisted current state and
actual action count. Exercise pause, stop, quiet and failure paths, configuration
preservation and normal unchanged recurrence. Independent tests must fail on the
old behavior and pass on the combined candidate where applicable.

Focused organization/context and completion/history inheritance checks protect
integration seams. Build through `make build`. Run selected backend checks on
Spark; do not run tui3 or broad expensive UI/E2E/full acceptance (C21). Report
skips and deferred checks honestly. No paid model is required to prove the
owner-state race; later conversational/connector journeys need their own evidence.

## Delivery boundary

Push a reviewable draft candidate; keep #662 and #663 intact and unmerged into dev.
This iteration does not authorize release or claim the full architecture works.
Record the resulting commit, test job, outcomes and remaining work in NEXT-STEPS.
The next contract covers current governing context only after its remaining
scope/authority choices are made explicit.
