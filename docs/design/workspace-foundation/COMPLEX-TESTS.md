# Complex regular-use verification — 2026-09-09

This follow-up tests existing organization behavior; it adds no automatic
acceptance, discovery, coordination or activation semantics. Three real-model
cases are registered in the default organization suite. They use DeepSeek V4
Flash and the existing budgets. Commands and bounds are in FUNCTIONAL-TESTS.md.

## Cases

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
Live confirmation of the repair is recorded below when available.

Large retrieval passed in 38.96 seconds for $0.002800 after correcting the test's
inspection of a truncated display receipt. The model correctly began listing at
offset 6, skipping the snapshot's six records; that 25-item page reached the
reference at index 30. Its displayed receipt was clipped, while the model had
received the full result. The test now checks the requested range against actual
stored ordering, plus the secret value and exact provenance independently.

**Conflicting-source verification is not yet green.** Earlier runs exposed an
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
