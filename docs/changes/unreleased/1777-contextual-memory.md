---
kind: added
title: ordinary memory retains evidence and conditional context before actions
pr: 1777
surface: [chat, engine, docs]
invalidates:
  - "Project corrections previously searched every owner readable by a session. Settlement now stays in the candidate owner, and supersession refuses an owner change."
  - "Equal titles could discard distinct bodies and concurrent exact writes could duplicate a claim. Duplicate settlement now checks complete normalized bodies within one transaction."
  - "Router failure or late recall could remove binding context from the first action. Supporting explicit user rules and decisions now have bounded local context before the first request, with lexical fallback and optional semantic routing."
  - "Assistant assertions and model-reported memory use were treated as outcome evidence. Ordinary extraction now keeps source words or independent receipts, conditions, rationale and revision eligibility; model self-report does not train benefit ranking."
  - "Forgetting only hid a saved row. It now suppresses all historical recorded sources and derived use; searchable original conversation history remains."
  - "Cross-project names alone supplied no consequence evidence. Successful full reads can establish exact resolved dependencies, and bounded consumer rereads support actionable hypotheses with content-based dismissal. Dynamic dependencies remain unsupported."
  - "An observed attempt hashed the raw tool receipt while the evidence row for the same receipt hashed the redacted one, so a secret-bearing receipt broke the memory-forget provenance join. Attempts now hash the redacted bytes on every writer (session, delegated, settled job), and a settled line is bounded to the store's own title and text caps instead of being refused when the decider answered longer than the candidate it merged."
  - "A changed producer needed one of a fixed list of words in the turn before a consequence was considered. Impact checks are now driven by the observed state, so an ordinary request is not missed, and an unchanged file stays quiet."
  - "A delegated paired re-read of a changed producer beside an unchanged consumer reset the recorded baseline and hid the consequence. The baseline is now kept until the consumer's own assumption changes."
  - "A narrative of newer incidental observations could push a live rare approved rule out of the binding read. Approved rules and confirmed decisions now have their own bounded authority-window projection; latest correction, expiry, conditions and suppression still win."
  - "A cited history lookup, task summary or memory_evidence result could become an independent observation and re-learn suppressed history under a new receipt id. Derived receipts no longer corroborate; genuine raw tool evidence is unchanged. Original conversation history still exists on disk and is not erased."
  - "A standing run authorized to act unattended was built from a vision posture with no brain, so it could act before any of the project's approved binding rules were in front of it. The run is now lent the owner-scoped canonical brain READ-ONLY: it reads the rules before its first action and remembers, extracts, observes or dismisses nothing back."
  - "A home filing whose Save failed still answered with the moved origin as though the record were on disk. The item is now returned as it actually stands, with the write failure surfaced and a visible needs-person line."
  - "A true substring inside a question or a rejected third-party quotation could become an approved rule. Authority is now judged from the containing clause as well as the quote, so a question or a rejected quotation is a proposal while polite directives and ordinary literal constraints still bind."
  - "A standing run with memory on silently fell back to an unbound run when its brain could not be opened, so an action-capable task could edit without any approved binding rule in front of it. Memory-on binding now fails closed and visible before any provider or tool call; memory-off stays intentionally absent, and an already-supplied brain is bound rather than bypassed."
  - "Machine scope was kept for any supported quote and its evidence was pinned to the origin project, so an explicit machine-wide rule became silently project-local. Machine scope now needs its own supporting span, carries no project condition, and applies across projects on the same authorized machine; a project span beside a global sentence, 'everywhere in this project', and any tool observation all stay project-local."
  - "A credential spoken beside a genuine constraint was persisted and rendered verbatim in the claim body and evidence. Literal support is still checked against the person's original words, but every persisted or rendered human-readable field is now passed through the existing secret redactor first."
  - "Dismissal matched a substring over every held notice. It now drops exactly what the person named, an unmistakable singleton, or an explicitly plural batch; a bare ambiguous dismiss drops nothing and a negated one keeps the offer. The held set is bounded and evictable, so nine or more historical notices cannot permanently block new material."
  - "A firing that reached an open window wrote no inbox note. The note is now made durable before the live offer, so a crash or a window closing between the offer and the reading cannot lose the line."
  - "A file watch took its baseline at the first wake. Ratification now arms it at the yes, and a scan that could not read everything it matches sets no baseline and leaves a visible needs-person flag."
  - "An ambiguous, refused or timed-out judgment was folded into a decided no. The real tick now carries the three-way sentinel: an undecided look writes nothing and the item stays due."
  - "An unchanged positive probe reading was reported again whenever the sentinel said yes, so \"notify me once when ready becomes true\" could put a second notice in front of the person for one observed state. The identity of the last affirmative reading is now the machinery's own fact: the same evidence stays quiet however the sentinel words its answer, a decided no re-arms, and a document carrying the identity or a one-shot intent is fenced at a newer schema so the build that preceded it skips rather than re-reports."
  - "A condition watch could only speak the rising edge, so a literal \"notify me once\" re-notified on every false-then-true with no person action. The one-shot intent is now a compiled field the model sets once at proposal time — shown on the approval card and kept in the stored document — and a one-shot say delivers its one line and retires, including when a lost delivery is repaired under the same identity, and a retire write that is lost after the line reached the person is settled from the delivery’s own append-only ledger record, so a restart or a later false-then-true cannot put the one notice in front of them a second time. A recurring watch (the field unset) re-arms as before, and an ambiguous one-shot task still stops for the person rather than replaying."
  - "A completed task whose post-run item write was lost was read on the next pass as an ambiguous in-flight attempt and told the person a task was waiting for them, and a one-shot that had already spoken never retired. The firing's own append-only ledger line now carries what it came to, and a task attempt is settled from its run identity in that record before the marker is read as ambiguous: a completed one-shot retires once, a recurring task is not run again, and a settlement restores the firing's spend, outcome, history, run folder and any question it left for the person — so a needs-you firing keeps its question and puts its clean-run streak back to nothing, and a task with no durable outcome still stops for the person rather than replaying."
  - "A background pass reloaded the profile's tiers, role pins and fallback ladder, so an item ratified under `--one-model` could be answered by a different model. The item freezes the ratifying conversation's model policy on its origin and the pass honours it for the sentinel and every child seat."
  - "A run lent the canonical brain read-only still journaled project attempt rows: nested task workers and the run's own promoted jobs wrote through the delegated outcome collector, which checked no posture. The collector now refuses every observation when the root is binding-only, so the lent brain is read-only through every door while the binding read and the ordinary session's delegated continuity are unchanged. A session folder keeps exactly one persistent resident now that its inbox flock lives inside it, and the lock file's name is exported for callers that assert on a folder's contents."
  - "Only failures and blocks were ever written from the tool boundary. A success in the same turn and goal now attaches to the failure as its observed successful alternative, and no other success is retained."
  - "A fresh turn shown a prior failure had no note of a path already known to work, so it could repeat dead work to rediscover it. An applicable observed alternative is now rendered beside its failure with its own receipt and source circumstances, as observed history rather than a cause or a ban."
  - "A condition watch passed the proposer's model-authored hint to the sentinel as WHAT A YES LOOKS LIKE with no provenance, so a status-only probe against an application-readiness ask fired while the body said not ready. The sentinel now judges the person's own sentence as the criterion, the hint is labelled the proposer's own guess, and a weaker fact than they named — a host answering where they asked whether an application is ready — is unknown."
  - "The sentinel was handed the probe's output alone and inferred the contract from it, so a bare HTTP 200 read as the application being ready. The judgment now carries the look's own command, or belt tool and arguments, beside the evidence under WHAT THE CHECK RAN, read from the approved item and never re-derived; a status or reachability reading proves transport and not the application contract the person named, and a weaker witness is unknown unless the output itself carries what they named or their own words made that reading the criterion."
  - "The sentinel's closing question asked \"yes or no\" while the prompt promised three answers, so an unresolved look could be read as a decided no. The closing question now asks yes, no or unknown as the first word."
  - "The stand tool's probe guidance did not say the look must be read-only, and its hint field could read as the person's own condition. A probe must now take a read-only look at what the endpoint or command returns, and the hint field says reachability is not their condition."
  - "The sentinel-grounding matrix lived in an in-package double that re-encoded yes/no/unknown plumbing rather than showing what the model does with the composed prompt. It is now an opt-in, tag-gated real-model end-to-end regression at the production seam, pinned to the review's model, that SKIPS with no provider key and is never a pass; focused tests and a skip still do not establish real work."
  - "The whole-request outcome-composition fixture modelled the trailing failure without its own observed alternative, so it claimed the captured two-failure/two-alternative case fit after the shared-caveat dedup when it does not. The fixture now drives the real composition seam with each alternative indexing, and the renderer states a genuinely shared source snapshot, seen day or whole-command action launcher once above the rows instead of on every row: inside the one shared 4,800-rune ceiling, alongside a realistic mandatory approved-rule load, both complete pairs now fit, rows with different structured sources, days or launchers keep their own exact statement, and a pair that still does not fit is omitted whole with no orphan alternative. The boundary test is a bounded lexical subset, not a shell grammar: a span inside an unquoted substitution or grouping construct is refused rather than parsed, and unknown syntax keeps the full per-row actions. No production cap, preview window, slot count, priority or authority order changed."
---

This draft starts from #1751 and keeps one canonical event journal. Source,
applicability, authority and validity remain separate. Ordinary automatic scope
is project-local unless the person's words explicitly grant wider applicability.
Approved rules retain their source conditions before actions. Existing standing
orders remain the only ratified scheduling mechanism; memory does not authorize
unrelated edits. Sync records replay identity and owner-proven pending tombstones,
but opposite-order conflicts remain arrival-ordered rather than convergent.

Also in this head: exact authority is judged against a claim's own supporting span,
not the whole utterance; the harness gained optional `--model`/`--one-model` and an
explicit `CODEAF_CALL_LOG_BODIES=1` opt-in, and a dedicated profile that pins every
text seat; and the immutability law on the canonical journal is unchanged, so raw
audit growth stays an explicit limit rather than something pruned.

A probe watch is bounded by the identity of the evidence it was shown, not by
the sentinel's wording: an unchanged positive reading is told once and stays
quiet, and only a reading that moved is judged afresh. A watch that asks to be
told once carries that as a compiled field — shown on the card, kept in the
document — and retires after its one durable, delivered line; a watch without
it speaks each change, and a rhythm keeps speaking on its schedule.

Standing deliveries are durable-first with this head. A delivery is appended to
its inbox under the pending identity and Sync'd before anything is drawn or
queued, then offered to an open window on top of the note. The inbox dedups on
the identity across retries and restarts. The live handoff is at least once: a
line offered live and then folded when the window is reopened can be read twice,
because whether a screen drew the row is not observable here, and loss is the
direction this refuses to fail toward. A write that could not be made durable,
or an item with no address at all, is reported so the caller keeps the intent
rather than reading success. A ratified FILE watch is armed at the yes, so a
change between the card and the first wake is a change; a bounded scan that could
not read everything it matches sets no baseline, leaves a visible flag, and the
first complete look clears it. A ratified item also keeps the model policy it
was given on: when the session ran under `--one-model`, the origin records the
promise and the model, the origin is written at the schema barrier so a build
without those fields skips it, and the reloaded background pass resolves the
sentinel and every child text seat from the item's pin with the role pins,
tiers, fallback chain and nearest-model guess withheld. One behaviour changed
for the keyless walk: an undecided look (for example, a machine with no API key)
is counted as checked and writes nothing, rather than being published as a
failed check; the item stays due and is retried next pass.

A recorded failure can also carry the later observed success that answered it.
The alternative is an `AttemptSucceeded` row whose `AlternativeOf` names the
failed attempt's source key, so an explicit forget of either source retires the
pair through the existing suppression join and the read side never shows a
success on its own. A source snapshot is labelled as a snapshot, never as the
live machine's own state. The rendered prior-outcome block is bounded by whole
records inside the one shared memory ceiling, and it states a fact once where it is actually
shared: when every row carries the same raw source snapshot and the same seen day, or the
same whole-command action launcher, that circumstance is stated once above the rows and the
per-row repetition is dropped, so two complete failure/alternative pairs fit where
repetition previously pushed the second one out. Rows whose structured source, day or
launcher genuinely differ keep their own exact statement, a span that stops inside a word,
quoted argument or heredoc is never factored, an unquoted substitution or grouping construct
is refused rather than parsed, and a pair that still does not fit is omitted whole with no
orphan alternative. A larger mandatory approved-rule load trims by whole
record exactly as before; no cap, preview window, slot count or authority priority changed.

The condition sentinel is grounded in the person's own sentence. A watch's judgment now
carries the look's own command, or belt tool and arguments, beside the evidence — read
from the approved item and never re-derived — so a status-only probe against a readiness
ask is unknown rather than a false yes; the model-authored hint is the proposer's guess,
not the person's criterion. The stand tool asks for a read-only look at what the endpoint
or command actually returns. The recorded failing shape was measured at the production
seam on the review's pinned model, and that live matrix is an opt-in e2e regression that
skips with no key and is never acceptance.

The built-in manual now names these limits in the words people use: an
app-closed watch has no separate helper to install; a `once` watch waits as an
inbox note rather than a notification or toast and is delivered only once, so
reopening does not repeat it; a judged look runs on a model, so the provider
serving THAT judge model must hold the credential — reconnecting an unrelated
provider leaves the watch where it was — and a look the judge model's provider
cannot pay for queues no note, leaves the item active and due, and is retried
once that provider is connected; and a check missed while the machine was off or
asleep is a best-effort request rather than a guarantee, and only while
background checks are on. It also draws the distinctions the old
pages blurred: an observed prior attempt is an advisory read before a relevant
turn, not the error-fix suggestion looked up only after a command fails; the one
shared store is not one shared scope, so "a memory kept in one project is there
in the next" is gone in favour of the closed shelves — you, this workspace,
this machine — with no memory owned by a whole team; and while a manager, a new
member and a task in that workspace read the workspace's approved rules and
confirmed decisions locally and deterministically before their first request (a
task read-only and bounded, with no provider asked), that binding read is a
separate path from the router's advisory relevant-memory shortlist, and a search
of an old conversation is reading only — it does not reactivate a forgotten
decision. The failed-method page now answers whether remembering a failed method or
tool stops codeaf trying it again: that observed-outcome advisory is read locally
from the saved memories before the first request and is not gated by the router's
ranking or small relevance model, it bans no tool, and with changed inputs or
circumstances the agent may retry or adapt rather than being forbidden — it never
promises a retry either. The scope list now names which mouth is which: an
omitted scope on the `remember` tool means this project, and only `/remember`
is the personal save kept as you — the engine's default, not a claim that
the personal save is the default. Retrieval probes for these questions are in
`internal/manual/chat_test.go`.

Final-head terminal acceptance is required before this entry is treated as
landed, and focused tests alone do not establish real work improved: the acceptance runs
against the frozen final head on the pinned model, and any key-skipped e2e run is a skip
rather than a pass. This entry makes no native-pass claim; see docs/contextual-memory.md
for limitations and evidence.
