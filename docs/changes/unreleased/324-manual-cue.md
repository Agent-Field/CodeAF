---
kind: added
title: a question about aforge now arrives with the manual's own matching titles beside it
pr: 324
surface: [chat, engine]
invalidates:
  - "\"the manual is only ever read when the model decides to call the `manual` tool\" — on a turn whose message reaches for the manual's own vocabulary, the top four matching section TITLES (heading plus an opening clause each) are now attached to that turn's requests before the model decides anything. The bodies are not: the sections themselves still have to be looked up."
  - "\"what the model is sent for a turn is exactly the transcript\" — it is the transcript plus this one block, on cued turns only. The block is attached to the COPY the requests are made from, so the transcript, the journal, a resume, an export and [Agent.taskRequest] are all unchanged, and the block dies with the turn."
---

#307 made the lookup land on the right page. #321 measured what was left and it
was bigger than everything #293 and #307 fixed put together: **whether the model
opens the manual at all swings by up to nine questions of twenty-five between
runs**, and a turn that never opened it cannot reach a page. That one bit sits
in front of every retrieval number this repository keeps.

The tool description already says what the manual is for and `prompts/system.md`
already names it, so asking harder is asking the same model the same way. What
the model cannot know is that **an answer exists** — this corpus is a few dozen
sections nothing in its training data has ever seen. So the harness shows it:
`internal/session/manual_cue.go` searches the corpus with the person's own words
and attaches the top titles beside their message. Evidence, not an instruction.

The gate is `manual.Chat().Cued`, the corpus's own derived vocabulary and
already the resident's self-question trigger, so an uncued turn is byte-identical
to what it was. `TestTheFixedPrefixStaysUnderItsBudget` reads **47,843 bytes
before and after**. `Config.ManualCueOff` is the one switch and exists for the
wire lane, which cannot report what a mechanism moved without a baseline
measured the same night.

## REPORT — what it did on the wire, and it is a null

`TestManualOnTheWire`, `deepseek/deepseek-v4-flash` pinned on every role and
verified off the usage ledger, the 25-set and the held-out 22, one fresh
conversation each, 2026-09-02. Three complete passes with the cue and one
complete pass without it on the same night, the same code path and the same
build. **The acceptance in #321 is not met and the cue did not move the open
rate.**

| pass | 25-set opened | first | within four | held-out opened | first | within four | wall |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **with** the cue, 1 | **16/25** | 14 | 16 | 12/22 | 7 | 9 | 9m58s |
| **with** the cue, 2 | **25/25** | 23 | 25 | 18/22 | 8 | 16 | 18m54s |
| **with** the cue, 3 | **17/25** | 16 | 17 | 19/22 | 8 | 16 | 15m0s |
| **without**, same night | **21/25** | 14 | 21 | 14/22 | 6 | 13 | 11m5s |
| (#313's without-cue baseline) | 19/25 · 17/25 | | | 18/22 · 17/22 | | | |

The acceptance wanted 23/25 or better on three consecutive passes. It got 16,
25, 17 — and the one pass measured without the cue on the same night, 21/25,
sits above two of the three. **Sixteen and twenty-five on identical code in
consecutive passes** is the variance #321 filed, unchanged.

Retrieval, normalised to the turns that did look something up, is the one
number that holds: **58 of 58 on the 25-set with the cue** (16/16, 25/25, 17/17)
against 21 of 21 without — both at 100%, so the ≥95% half of the acceptance is
met on a measurement that no longer discriminates. Held out: 41 of 49 with, 13
of 14 without.

**And there is a named mechanism for why it can hurt.** On the turns that never
opened the manual, the model treats the block as a lookup that already
happened — measured, in its own words: `what is aforge` was answered with
"That's the manual you found", and `what does it remember` with "Yes. The
manual covers memory in three sections:", neither turn having called anything.
Four such replies in pass 1, two in each of passes 2 and 3, none in the pass
without the cue. **Titles and an opening clause are enough to answer from and
not enough to be right from.** The obvious next experiment is titles with no
clause at all, and it is not run here.

Spend was not recoverable exactly: no question breached the lane's $0.05 cap in
any pass, and the ledger the lane reads lives under the throwaway home the test
deletes. The passes were 10–19 minutes each against #313's $0.15–0.20 a pass,
which puts each of these in the low tens of cents and the five passes (the four
above plus one casualty) at a few dollars. **The lane now sums it and prints
`THIS PASS SPENT $…`**, so the next pass reports it rather than estimating.

One earlier pass is a **casualty and is pooled with nothing**: it went silent at
question 22 of 25 and stayed silent for 26 minutes, and `SIGQUIT` named a
deadlock between `hedgeRace.mu` and `streamWatch.mu` in `internal/provider`
that has nothing to do with this change. Filed as #330 with the stacks and a
deterministic replication.
