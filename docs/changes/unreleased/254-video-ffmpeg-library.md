---
kind: added
title: an ffmpeg library and edit_video — a join that cannot come out silent
pr: 254
surface: [chat, engine, docs]
invalidates:
  - "Assembling a video was shell work, and the manual said so: 'a longer video is several `generate_video` calls stitched together with ffmpeg in the shell'. It is `edit_video` now — one belt verb with four actions, `measure`, `frame`, `join` and `score` — backed by the new `internal/video` package."
  - "A stitch had to remember to carry the audio: the corpus said 'the stitch has to map or crossfade the audio streams too, or concatenate both streams together', because an ffmpeg filter that only touches the video streams keeps the first input's audio and silently discards every other clip's. `internal/video`'s `Join` cannot do that — every clip contributes one video chain and one audio chain, and a silent clip is given generated silence of its own measured length, so there is no branch in which the audio is not mapped."
  - "Fitting a piece of `generate_music` to a video was arithmetic somebody did by hand — 'the file has to be measured and then looped or trimmed to fit, which is shell work with ffmpeg'. `edit_video`'s `score` does it in one invocation (`-stream_loop -1` in, the picture's own length out), so the length the music model chose stops mattering."
  - "Every conditional verb on the v3 belt was gated on the person's media settings resolving a model. `edit_video` is gated on a MACHINE FACT instead — ffmpeg and ffprobe on PATH — and on nothing else: it buys nothing, so it is present on a machine with no video model, no key and no credit, and absent (never present-and-refusing) on one without the binaries."
  - "`savingTools` in `internal/session/task_run.go` was six names, all of which always save. It is seven now and `edit_video` is the exception: three of its four actions write a file and `measure` writes nothing. It is there because that map is also the LANDING BELT, and a node ordered to land the film it spent its life cutting needs the verb that joins one."
  - "The harness belt with no media seams was exactly seven tools, and `media_everywhere_test.go` asserted that count. It is eight wherever ffmpeg is installed: `edit_video` needs no seam at all, so no media wiring does not take it away, and a saved harness may whitelist it."
  - "The fixed prefix was 758 bytes over budget on `dev` (issue #238, measured at c1776b76) — no longer true. It is 47,168 bytes now, 832 under the 48,000 the budget has held since the autonomy merge, and the budget itself did not move."
  - "`internal/video` is a new package and the only place in `internal/session`'s reach that shells out to ffmpeg. `internal/session/mp4.go` still exists and still does its own box-walking: it measures bytes already in memory when a render lands, with no process to start, and nothing about it moved."
---

Issue #250. The two halves of video work were being treated as one. A render is
minutes of somebody else's GPU for one short clip; a film is several of those
joined, with a frame carried out of each one to open the next and a score
underneath — and all four of those operations are local, free and deterministic.

WHY THE FIXES ARE STRUCTURAL RATHER THAN DOCUMENTED. Each of the three defects
above is silent: the cut plays, the file looks right, and the only witness is an
ear. `amix` normalizes unless told not to, so the obvious mix halves the dialogue
the moment a score is added, and one output is not enough to hear it happen. None
of that is knowledge worth writing down for a model to remember and apply
correctly under pressure, so it is compiled in instead: every operation is a pure
plan plus a run of it, the plans are asserted on as data by tests that need no
encoder, and the round-trip tests then prove ffmpeg accepts them.

WHAT IT COST THE FIXED PREFIX, because a lane adding a tool is the one person in
the loop who never sees that bill (`prefixbudget_test.go`). The verb's schema and
description are **1,885 bytes** — under `tasks` and over `bash`, for four actions
and nine arguments — and the first draft was 3,219 before the long-form teaching
moved to the manual, which is a tool the model can call. The routing line that
lane added to the system prompt was removed again too, because "never write
ffmpeg into bash" is stated in the tool's own description, which rides in front
of the same request.

AND THEN IT PAID THE WHOLE BILL, ITS OWN AND #238's. Adding the verb put the
prefix **2,632 over**, on top of the **758** `dev` was already carrying
(measured at c1776b76). Per that test's own rule the budget was not raised:
**3,464 bytes** came out of text that stated a law a SECOND time. The
media-making essay left `prompts/system.md` for one sentence a mechanism,
because the manual's `making-pictures-audio-and-video` page teaches all of it in
full; so did the paragraphs that read `bash`, `jobs`, `tasks`, `fork`, `manual`,
`build_harness` and `change_setting`'s own descriptions back at the model, and
the three-part `propose_task` contract its own schema fields spell out field by
field. Not one rule was dropped — each is still stated, once. The prompt went
23,954 → **20,589**, the tool block 26,678 → **26,579**, and the prefix is
**47,168**: 832 under budget, and green for the first time since #238 was
filed.
