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
  - "A condition watch could only speak the rising edge, so a literal \"notify me once\" re-notified on every false-then-true with no person action. The one-shot intent is now a compiled field the model sets once at proposal time — shown on the approval card and kept in the stored document — and a one-shot say delivers its one line and retires, including when a lost delivery is repaired under the same identity. A recurring watch (the field unset) re-arms as before, and an ambiguous one-shot task still stops for the person rather than replaying."
  - "A background pass reloaded the profile's tiers, role pins and fallback ladder, so an item ratified under `--one-model` could be answered by a different model. The item freezes the ratifying conversation's model policy on its origin and the pass honours it for the sentinel and every child seat."
  - "A run lent the canonical brain read-only still journaled project attempt rows: nested task workers and the run's own promoted jobs wrote through the delegated outcome collector, which checked no posture. The collector now refuses every observation when the root is binding-only, so the lent brain is read-only through every door while the binding read and the ordinary session's delegated continuity are unchanged. A session folder keeps exactly one persistent resident now that its inbox flock lives inside it, and the lock file's name is exported for callers that assert on a folder's contents."
  - "Only failures and blocks were ever written from the tool boundary. A success in the same turn and goal now attaches to the failure as its observed successful alternative, and no other success is retained."
  - "A fresh turn shown a prior failure had no note of a path already known to work, so it could repeat dead work to rediscover it. An applicable observed alternative is now rendered beside its failure with its own receipt and source circumstances, as observed history rather than a cause or a ban."
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
live machine's own state.

Final-head terminal acceptance is required; focused tests alone do not establish
real work improved. See docs/contextual-memory.md for limitations and evidence.
