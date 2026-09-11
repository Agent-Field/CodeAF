---
kind: fixed
title: a failed request leaves a row where you are reading, and a finished step stops saying it runs
pr: 795
surface: [chat]
invalidates:
  - "A retry showed only on the status line, as `trying again`, and that was taken to be enough. It is not: the status line is gone by the next redraw, so a turn that failed four requests over ninety seconds and gave up left NOTHING in the conversation. Every retry, hop and give-up now draws one dim row in the feed, and the end of the ladder reads `gave up after 4 tries · <what the provider said>` where it used to read `error: after 3 retries: <the same thing>`."
  - "The retry rows drew the engine's whole sentence (`the model went quiet mid-reply — asking again`, `… — finishing this one on openai/gpt-5-mini`). They are built from `session.RetryNews` (#794) now: `the model went quiet · asking again · 2 of 4`, and `the reply lost its thread · moving to gpt-5-mini` for a hop, with the model spelled by its basename. A row that is MOVING carries no count — the count was the patience of the model being left. `Event.Text` is still sent and is still what an older engine across a `--host` link is drawn from. Every page that quoted the old spelling was corrected in the same change; `session.ResumedWord` is a note from takeover.go and is unaffected."
  - "A step's title used to be spelled in the present whether or not its calls had finished, so a batch that had closed went on reading `running 2 commands` for as long as the turn waited. The floor caption a step composes from its own tool calls now takes the past once every row in it has closed — `ran 2 commands`, `read 3 files in internal/tui3`, `edited 2 files and ran the suite`, `built`. A title the MODEL narrated is still drawn exactly as the model wrote it."
  - "There were three spellings of a failed request — the chat's, a node's page's, and the status line's. There is one: internal/tui3/failurerow.go's `failure` struct and `failureRow`, which all three compose from. A wave adding a fourth surface writes no new sentence."
  - "`internal/tui3` was believed to lose the EventError note (no row on the owner's screen). It does not: the note is drawn, and failurerow_test.go pins the whole sequence through `app.event`. What was missing was a give-up that read as an ending, and three identical retries collapsing to one row by the note-repeat rule. A blank feed after a page switch is a different fact — surface notes are never journaled, so a conversation re-attached from the engine's transcript rebuilds without them."
  - "A node's page draws these rows from its LIVE lane only. It still cannot draw them from a replayed journal: internal/session's reader drops `error` lines on purpose and `session.Record` has no field for them, so a room opened AFTER the failure shows nothing about it. That needs an engine change."
---

The measured screen (2026-09-10, conversation `57d51779f63ac603`): two bash calls
came back within the second, the follow-up request failed four times over ninety
seconds, the turn gave up — and two minutes later the conversation showed a step
captioned `running 2 commands` with nothing under it. Three defects that read as
one thing: work that looked live, a ladder nobody could see, and an ending
nobody was told about.
