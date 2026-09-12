---
kind: internal
title: every request says what it is for, a dead role goes, and the prefix gate weighs the real belt
pr: 996
surface: [chat, engine]
invalidates:
  - "`auxiliary.go` said `callRole` was \"the one door all auxiliary provider calls pass through\". It never was — ten calls went round it. It is the one door every ERRAND passes through; the one door every REQUEST passes through is `clientdoor.go`'s `completeWithModel`."
  - "The call tag was a context value set with `provider.WithCallTag` wherever a caller remembered to. It is an argument now — `callPurpose`, the second parameter of `completeWithModel` — and a call with no purpose does not compile. Nothing in `internal/session` but the door may call `WithCallTag`."
  - "Nine auxiliary calls reached the wire with no tag: the guardian, vision, the shaper, the spell-out, the subharness intake, the run planner, the harness designer, the ceiling's handoff draft and a saved program's AI step. Every one of them now names itself in the model-call log."
  - "`roles.RoleCompaction` existed, sat on `TierHigh`, had a description and a settings row. It is deleted: no compaction has made a model call since the summariser went, and `Agent.compact` takes `_ context.Context`."
  - "`TierHigh` had a built-in tenant in `DefaultAssignment`. It has none — every careful call (`auditor`, `shaper`, `vision`, `careful`, the repair) registers itself from the package that makes it."
  - "`TierHigh`'s five-minute patience was justified by \"a compaction summary over a full window\". That call does not exist; the figure is derived from the auditor now."
  - "The settings roles list offered a `compaction` row and the crew's careful line read \"audits, compaction, vision\". Neither does; the line reads \"audits, briefs, vision\"."
  - "`fixedPrefixBudget` was 48,000 and weighed `v3ShapedAgent` — 18 tools, no memory store, no accounts hub, no standing items, no saved programs. It weighs the fully-wired conversation (`shippedShapeAgent`, 23 tools) and is 53,141, which is that shape's measurement on the widest machine and no headroom."
  - "The shipped prefix was believed to be inside 48,000. It is 53,141 — 5,141 over — and has been for as long as the shape existed. 48,000 is a target the test prints the shortfall against, not a cap anything passes today."
  - "`v3ShapedAgent` claimed to be the conversation the interactive door configures and built its own thin config — 18 tools, no memory store, no accounts hub, no standing items, no saved programs. Four gates weighed the shipping door through it, including both prefix budgets. It is `shippedShapeAgent` now: there is ONE shipped shape."
  - "`leanPrefixBudget` and `fixedPrefixBudget` printed their overage with `t.Logf`, which nothing in the Makefile passes `-v` to — the line saying the prefix was thousands of bytes over its target was printed where nobody would see it. The cap is the target plus a dated waiver in one place (`prefixWaivers`), and a waiver only shrinks."
  - "A role's word could be declared at its call site, which internal/roles' own header invited. It cannot: everything that reads the vocabulary reads one file, so `spellout`, `sentinel` and `repair` were each priced as errands nobody waits on. All three are declared in `internal/roles` and a law catches the next one in either spelling, including `roles.Role(\"x\")`."
  - "The replay table priced `vision` as an auxiliary; `image.go`, its only writer, sets `lane.RoleTalk` — a look at an image streams into the room during the person's own turn."
  - "The manual taught a summariser rung for `/compact`, its `session: summarizer returned nothing` refusal, and a focus appended to the summariser's instructions. There is no summariser: a pass stubs tool results and folds the oldest assistant work to one marker line, asks no model, and never folds your own words."
  - "A profile whose `models.roles` row pinned `compaction:` was left unreadable by the role deletion: the panel drew no row for it and any other pin re-serialised the whole string through a writer that refused `\"compaction\" is not a role`, so every pin on that machine was unchangeable until config.json was hand-edited. The reading is symmetric now: a pin for any word that is not a role is dropped on read, never written back, and refused only when it is the pin being ADDED. The row draws what is acted on rather than the raw stored text, and the panel says `compaction is no longer a role — that pin is ignored`."
  - "`leanPrefixBudget` was 31,500 and weighed a lean conversation with no memory store, no accounts hub, no standing items and no saved programs — 16 tools. It weighs the shipped shape on a small window and is 44,920. The arm that protects the person with the LEAST room was out by 13,420 bytes, which is more than the budget it was enforcing."
  - "`spellout` priced as an errand nobody was waiting on in every cost report, because its Role constant was declared in internal/session and `roleWords` parses internal/roles. The word is `roles.RoleSpellOut` now, and a law fails the next role declared anywhere else."
  - "The memory tidy-up, the standing sentinel and the document reader reached `calls.jsonl` with no tag at all. They build their own provider client and never pass the door, so they now say the door's own word through `withPurpose` — `consolidate`, `standing-check`, `document` — and the law fails a file that builds a client and names nothing."
  - "The fixed prefix was believed to be the same size everywhere. It is not: `grep` says a longer sentence about itself where ripgrep is absent (134 bytes) and `load_capability` lists the tool groups this build has (27). The gate weighs the widest machine — `bare.WidestGrepDescription` is the one place that knows which spelling that is — so the number does not depend on who runs it."
---

Three things this build reported about itself incorrectly. None of them changes
what any model is asked or what it answers.

The tag is the load-bearing one. A tag that can be forgotten is a tag that will
be, and the log that was supposed to answer "what was this build spending that
model on all night" could not: 2,309 of the 2,839 untagged finishes in the ten
days to 2026-09-10 were this package's. Making the purpose an argument is the
whole fix — the type refuses an anonymous call, and `nohiddenwork_test.go`
refuses one that satisfies the type with a blank.

The prefix gate is the same disease in a test: a gate pointed at something
nobody runs passes without having tested anything, which is #576's shape. The
5 KB it was hiding is filed rather than paid — `stand`'s schema is 9,607 bytes,
more than a quarter of the tool block, and trimming it is a change to a tool's
contract with the model rather than to a byte count.

Two seams are reported rather than worked around: the standing sentinel builds
its own `provider.NewClient` and has no `*Agent` to reach either door through,
and four of the nine anonymous calls (guardian, shaper, spell-out, intake) are
genuine errands that could go through `callRole` — which would ask a second
model on failure, and so is a behaviour change rather than a fix.
