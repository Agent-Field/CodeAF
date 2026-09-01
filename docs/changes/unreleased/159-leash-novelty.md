---
kind: changed
title: the leash judges what a task produced, not what it was called, and it has to open the tree
pr: 159
surface: [engine]
invalidates:
  - "A task's checkpoint was handed the hand and its arguments — `edit {\"path\":…}` — and nothing about what the call left behind. Every evidence line now carries whether the call produced anything that was not already there, and a run of them is stated in front of the list as `the last 6 write calls produced byte-identical content`."
  - "Three named thresholds ended a task's work: the deadline, `max_steps` and `no_progress`. There is a fourth — a run of steps that counted as progress and produced byte-identical content — and it is the only one that ends nothing on its own: reaching it makes the harness ASK, and the report of a task landed that way is `stopped at repeat checkpoint: <what the second look said>`."
  - "`no_progress` was only the counter's limit. It is also how many calls in a row may produce byte-identical content before the second look is asked about it — one number, on the wire, moving both. There is no constant anywhere saying how many identical saves are too many."
  - "A checkpoint answering WORKING always bought the task another equal slice of its deadline and its step budget, up to four. The repeat checkpoint buys it nothing: WORKING starts the run again and the task carries on inside the allowance it already had, because a spin granted another two hundred steps is a spin buying itself room."
  - "The second look was told `Read the working copy if useful` and could answer without opening anything. Reading it is required now — the charter says so, the question names the ground the work is running in, and an answer given without a single look is sent back once with `You answered without reading the working copy`; the second answer stands."
---

Measured 2026-08-31: a cheap worker rewrote one file in near-identical increments
twenty-odd times over twenty-two minutes and $7.99. Every call exited cleanly, so
the no-progress counter — which a successful save resets — sat at zero the whole
time, and the 200-step cap was the only thing that ended it. The checkpoint that
could have stopped it was asked, was handed a list of successful `edit` calls,
and renewed the leash.

Nothing about the thresholds was wrong. What was wrong was the inputs: the reader
was judging a story about intentions instead of a fact about the world, and it was
never made to look at the tree it was standing in. So the harness now fingerprints
what each action PRODUCED — the bytes of the file a saving call wrote, the state
of the tree a command moved, or the answer any other call brought back — which is
the same finding for a search run twice, an API called again with the same body,
a page downloaded twice and a file rewritten with the same bytes. The facts are
supplied; the ruling stays with whoever is asked.
