# Complex regular-use verification — 2026-09-09

This follow-up tests existing organization behavior; it adds no automatic
acceptance, discovery, coordination or activation semantics. Four real-model
cases are registered in the default organization suite. They use DeepSeek V4
Flash and the existing budgets. Commands and bounds are in FUNCTIONAL-TESTS.md.

## Cases

- **Removed-scope identity read:** explicitly read a previously used record after
  removing membership, then assert that its text stays readable while the final
  report leaves current applicability false and does not restore membership.
- **Working day:** twelve consumer chats and one author, batches of three
  simultaneous initial turns, eight further real review turns in one chat,
  source revision, observed idle interval, reopen, overlapping membership
  removal/rejoin, and model-tool withdrawal. A complete run has 33 submitted
  turns. Review documents are synthetic fixtures, read by the actual model.
- **Conflicting sources:** two independent records disagree; a person chooses a
  date for one conversation's draft. That local choice should survive reopening
  without rewriting either shared record or becoming another chat's choice.
  Source disagreement and draft choice are separate output fields.
- **Large retrieval:** one reference among 31 applicable records, outside the
  initial snapshot and first metadata page, contains a nonced fact beyond 8,000
  characters. The real model must page, read with pinned revision and write the
  correct source identity and value.

## Findings before the scope repair

The full working-day run completed in 577.06 seconds, spending $0.110078.
It produced a 332,411-byte journal after eight follow-up reviews, used current
revision 2 after the revision and reopen, and observed an idle consumer for
50.636 seconds without output changes, new turns or wakes. Removing one of two
memberships kept the context available, and rejoining restored it.

**The working-day run failed after removing both memberships.** The chat first
wrote unknown/unavailable, then its completion continuation read the remembered
record ID. Because the record was globally present and not withdrawn, it rewrote
the artifact with the old applicable value. The record was readable but no longer
applicable to this consumer. The strict assertion remained in place and caught
the final artifact, not merely the first correct write.

The repair adds `applicable_here` and an explanatory `scope_note` to identity
reads. One indexed query reuses the same scope predicate as selection and checks
the exact current revision plus withdrawal. Global reads remain available.
The retired-context note explicitly distinguishes existence from applicability.
Deterministic tests cover both membership paths, removal, rejoin, historical
reads, withdrawal, direct task identity and the absence of ancestor inheritance.
Live confirmation of the repair is recorded below.

Large retrieval passed in 38.96 seconds for $0.002800 after correcting the test's
inspection of a truncated display receipt. The model correctly began listing at
offset 6, skipping the snapshot's six records; that 25-item page reached the
reference at index 30. Its displayed receipt was clipped, while the model had
received the full result. The test now checks the requested range against actual
stored ordering, plus the secret value and exact provenance independently.

**Earlier conflicting-source verification was not green.** Earlier runs exposed an
ambiguous combined status field (sources can still conflict after a local choice)
and noncanonical source-ID formatting. The test now separates source status from
local choice, requests exact raw identifiers, and restricts writes to the named
report. A later run still discussed the correct conflict without creating the
requested report. This is a real model/runtime completion failure, not proof of
reliable regular use. It is not skipped or added to the known-red ledger.

An early working-day fixture also required an unused note field to be empty;
that irrelevant requirement was removed. Real document review codes remain
strictly checked. Another run omitted a requested record ID; explicit provenance
instructions were added without relaxing the identity assertion. These failures
are retained rather than summarized as a completely green first attempt.

## Receipts and remaining limits

Local diagnostic directory:
`/Users/santoshkumar/af-personal-ai-complex-evidence-20260909/`.
The original failure is `working-day-confirm.jsonl`; retrieval confirmation is
`large-retrieval-confirm.jsonl`; the failed clarified conflict case is
`conflicting-sources-final.jsonl`. These are disposable-machine receipts, not
required fixtures: the committed cases construct their own homes and data.

This is 12 conversations with up to three submitted turns at once, not 12
simultaneous running task workers. It does not establish context-window saturation,
automatic compaction, weeks of persistent background execution, concurrent
mid-read revisions, cancellation during context refresh, or crash recovery.
Existing deterministic tests cover parts of those storage guarantees separately.
It does not prove autonomous semantic discovery or cross-chat consultation.

The paid CI workflow remains enabled for mechanism changes but currently lacks
the repository credential and reports NOT RUN. Local real-model failures must
not be hidden behind successful ordinary PR gates. Keep #662 draft and unmerged.

## Final observed results for this wave

The three-case rerun used production/test commit `c43119be5` and retained
`scope-repair.jsonl`. It completed all cases, but the combined run was **red**.

| Case | Result | Duration | Model spend |
| --- | --- | ---: | ---: |
| Large retrieval | Pass | 46.83 s | $0.006959 |
| Working day | Fail: numeric revision corrupted in one initial report | 555.98 s | $0.127475 |
| Conflicting sources | Pass, including local choice after reopen and separate reader still needing a choice | 39.49 s | $0.023976 |
| Forced removed-scope identity read | Pass in separate run using production `3a6140a72` | 22.67 s | $0.004045 |

The working-day removal failure is repaired: after both memberships were removed,
the completed turn left `value: unknown`, `status: unavailable` and revision 0.
Rejoining restored revision 2. The idle interval was 50.352 seconds; the long
review journal was 317,140 bytes. Withdrawal completed across three consumers.
The separate forced-read case proves the old record is still readable while
`applicable_here` is false and the final report's `current_value` is empty.
That receipt is `removed-scope-read.jsonl`. Commit `3a6140a72` only moves scope
metadata ahead of long text; its dedicated clipped-receipt and race checks pass.

The remaining working-day failure is tracked as
[#672](https://github.com/Agent-Field/aforge-v2/issues/672). A support-guide report
initially contained the correct IDs and numeric revision 1. After reporting
contradictory completion feedback, the agent changed that field to string `"1"`;
the strict JSON assertion failed. The observable corruption is certain; the
actual completion page/judgment still needs direct capture to isolate the cause.
The completion owner checked newer upstream changes and found no targeted fix.
Do not loosen the numeric/provenance assertions or skip this case to get green.

The complete touched-package regression passed on `c43119be5`: workspace, adapter,
session, command, manual and untagged E2E packages. Session took 269.299 seconds
in an isolated checkout. Focused race checks, vet, packed manual and the normal
build/size check pass. PR gate run `34380523465` passed on `3a6140a72`. The paid
CI job skipped because its credential remains unavailable; that is not evidence
of a green full live suite.

Earlier failed runs remain in the diagnostic directory. This wave does not
establish model reliability across repeated days or all supported providers.
The shared-context mechanism is more accurately reported now; the full
regular-use scenario still has an open completion failure.

## Completion repair investigation — follow-up on #672

A fresh baseline at `ca2a59d5a` captured actual model requests and carry-on
messages with the built-in call log. The reader repeatedly claimed fields were
missing from valid reports. It saw a write label clipped inside the JSON and a
byte-count receipt, but not the complete submitted object. Its objection then
rode as an asserted instruction. The baseline ended red after repeated extra
work led to a task owning the directory and the final requested review report
being absent; it is not a green baseline.

The repair pairs bounded, whole write/edit arguments with their matching tool
results. The ledger names attempts rather than claiming success. Larger inputs
are explicitly omitted, never presented as complete partial JSON. Reader and
carry-on instructions distinguish observations from established facts: inspect
missing evidence and preserve correct work when an objection is mistaken.
No domain-specific provenance comparator or skipped completion check is added.

The first live negative probes still accepted a wrong source and a failed write.
An actual comparison also exhausted the inherited 300-token sketch limit. The
completion call now uses provider/operator generation defaults, consistent with
upstream #665, and the prompt explicitly prioritizes results over claims. All
four probes then passed in 12.76 seconds: correct report accepted, wrong numeric
type, wrong source and failed write rejected. This is controlled reader evidence,
not a substitute for the complete working-day rerun. Deterministic tests failed
before the repair and pass after, including input/result pairing and size bounds.

Evidence is retained under `/Users/santoshkumar/af-completion-evidence-20260909/`:
`before-workday.jsonl`, `before-calls.jsonl`, `before-completion-pages.json`,
`reader-check.jsonl` (first negative probe failures), and `reader-v2.jsonl`.
The full repaired journeys and final regression checks are pending in this
checkpoint; keep #662 draft and unmerged until their receipts are recorded.

The repair's follow-up review found that retaining every small write input could
crowd earlier test failures out of the digest. A deterministic 20-write fixture
reproduced that risk. Only the newest write/edit with a matching result now
carries its bounded exact input. Existing result tails remain; the fixture keeps
both the earlier failure and the newest input. Shared mark and handoff readers
receive the same improved digest. Older inputs remain unavailable rather than
being represented as fully inspected.

`dev` moved to `4737e7f1` while this was tested. Merge `3526f2b8c` brought those
landed changes into the review branch, resolving one completion-call conflict
and retaining both our evidence repair and upstream #665 generation defaults.
This is not a merge of #662 into `dev`. Final acceptance must be taken on the
combined branch; the separate repair-only receipts remain historical evidence.

## Completion follow-up receipts and limits

The following supersedes the pending-rerun status above. Receipts remain under
`/Users/santoshkumar/af-completion-evidence-20260909/`; portable commands use the
committed fixtures and never require those private paths.

| Revision / receipt | Result and meaning |
| --- | --- |
| `fa9f1fe64`, `after-complex.jsonl` | The 12-chat working day passed in 665.99s. The combined selection was red: conflict checks rejected both a genuine mistyped path attempt and macOS symlink aliases. |
| `53dd6443f`, `integrated-all.jsonl` | Removed-scope read and large retrieval passed. Working day missed reports after the model dropped `_day` from two absolute destinations. Completion probes falsely accepted wrong type/source. A separate cleanup deleted the active integration checkout mid-run; binary failure/interruption is infrastructure damage, not full acceptance. |
| `743f70866`, `presentation-v2-reader.jsonl` | Clearer ledger labels did not establish reliable judgment: a correct report was rejected and a failed write accepted. Keep these negative results. |
| `743f70866`, `final-remaining.jsonl` | The rebuilt binary's real-model test passed. This run was interrupted later; it is not a complete suite. |
| `8a104b169`, `resumed-remaining.jsonl` | The conflict fixture rejected read-only `stand/list`; task workers created shell output without carrying it home. Later reruns below address these cases. Historical domain passes here predate the stricter scope-file assertion. |
| `3f678b893`, `path-workday.jsonl` | All selected cases passed: removed-scope read 33.10s, large retrieval 28.51s, working day 561.86s. Complete selection 623.47s, plus all four real reader probes. |
| `fb95cd197`, `path-worker-conflict.jsonl` | Conflict/local-choice 91.96s and actual task workers 106.42s passed, including returned artifacts and independent task addresses. All four real reader probes also passed. |
| `c9b9ff388`, `strict-scope.jsonl` | The two-bug journey passed with the newly required valid unrelated-chat JSON report. The remaining domain reruns use the same stronger assertion. |

Two test corrections preserve the requested boundaries: compare actual file
identity so `/var` and `/private/var` can name the same artifact, while rejecting
missing/different files; allow exactly `stand` with `op:"list"`, while rejecting
all its mutations and malformed operations. A new assertion now reads and parses
`scope.json`, separately from checking that no unrelated context entered the
model request. Earlier domain receipts did not establish that output guarantee.

The worker failure's actual trace showed a shell redirect creating the file in
the task copy, then an empty delivered-file list. The full-path instruction was
broader than intended: file tools already resolve relative paths. The narrowed
instruction preserves final absolute references, supplied paths and approval
rules. The worker rerun returned both files without changing the fixture brief
or allowing a missing artifact.

Local full `internal/session`, `internal/manual` and untagged `internal/e2e`
suites passed on `3f678b893` (265.285s, 2.059s, 0.413s). The new assertion helpers
passed their untagged suite. Build and size passed: 52,033,090 bytes against the
54,600,000-byte budget on `fb95cd197`. PR gate 34387315191 passed on `743f70866`;
later head checks must be read independently. Earlier local regressions suffered
full disk, removed cache objects, deleted logs and interruption; they are not
claimed as green. Final evidence is kept outside `/tmp` to preserve receipts.

Alternative reader presentations were investigated in standalone diagnostics.
Unescaping the displayed content still missed a wrong source; asking for a short
evidence statement before a structured result falsely rejected correct content.
Neither experimental presentation was installed. Those experiments did not
justify another parser or a second model call. #672 remains open for the observed
reliability gap; no claim of universal model reliability follows from the latest
passing cases. The branch remains draft and unmerged.

### Final strict domain reruns

All five domain journeys passed the strengthened `scope.json` assertion on
`c9b9ff388`: `two_bugs` 139.56s (`strict-scope.jsonl`), `api_contract` 102.74s,
`maintenance` 83.71s, `research_marketing` 97.49s and `travel_calendar` 93.86s
(`strict-<case>.jsonl`). The last four ran in separate processes with independent
profiles and disposable homes, so process-global test environment changes could
not cross cases. All four process exits were zero; `strict-domains-results.json`
records the revision and exits. No model assertions were skipped or weakened.

The combined historical `resumed-remaining.jsonl` run is still red for its
pre-fix worker and standing-list failures. Its other cases passed. The targeted
reruns above are the repair evidence, not a rewrite of that full-run result.
The latest production code has passing working-day, worker, conflict and domain
evidence across the recorded runs; there has not been one final all-thirteen
invocation after every test refinement. #672 still records earlier inconsistent
reader judgments, and paid CI still lacks credentials. Keep the draft open.
