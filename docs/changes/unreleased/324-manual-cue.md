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
