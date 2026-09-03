# The chat surface — polish audit

Captured from `bin/aforge` in a real terminal on socket `polish-chat`, against the
demo home, at 160x50 / 120x40 / 80x24 / 60x30. Frames are in
`docs/design/polish/frames/chat-*`. Three fixtures were seeded into the demo home to
reach shapes the stock seed never draws: `Wrapping Torture` (markdown, URLs, a fence,
CJK, emoji), `Long Question` (a six-line question), `Emoji Widths` (one hard grapheme
per line). Two live turns were run — one on the demo's fictional model (which never
answers, and so exercises the whole waiting ladder) and one on `anthropic/claude-3-haiku`
(which answers, streams, and calls a tool).

Much of this surface is right and is deliberately not listed: first paint is under
100ms with the whole transcript already drawn; resize mid-stream redraws clean inside
250ms with no garbage and no dead frame; a multi-line draft survives a 120→60 resize
and re-wraps with its hanging indent intact; the key hints on the legend change
correctly when a draft is in the box; there is not one `lipgloss.Color(…)` literal in
the package — every hue comes through the token ramp, and the light ramp does swap when
the terminal says so; the scrolled state grows a real `↓ latest · ctrl+l` door; the
thinking pulse never changes row height while it animates; CJK, flags and combining
marks all measure correctly.

---

1. A person's own message longer than three rendered rows is cut off mid-sentence, with no ellipsis, no door and no key that opens it — `internal/tui3/render.go:876` (`briefFoldCut` is applied to every `entryUser` block, but `brieffold.go:90`'s `briefFoldHidden` returns 0 unless `e.brief`, so `render.go:522` never draws the door and `brieffold.go:131` makes `ctrl+o` a no-op) — the transcript is the only record of what was asked for, and here it silently loses most of it: a six-line question shows three lines at 120 cols and stops at "the cheapest single", and at 60 cols it keeps a quarter of what was typed; brieffold.go's own header states the opposite law ("the one thing on this surface a fold may never hide is the person's own words") — gate `briefFoldCut` on `e.brief` exactly as `briefFoldHidden` already is, so an ordinary message is drawn whole and only a node's instruction folds — sev: high — frames: docs/design/polish/frames/chat-longmsg.120x40.txt, chat-longmsg.60x30.txt, chat-longmsg-ctrlo.120x40.txt, chat-resize-mid.80x24.txt

2. The live status line prints `0 tok/s` — `internal/tui3/render.go:2432` guards `written <= 0` but not a *rate* that rounds to zero, so one output token over a sixty-second turn reaches `burnStep(0)` and renders the zero — the emptiness law's one exception is `$0.00` on this line and nothing else, and a zero rate is the least informative cell on the frame at the exact moment a person is deciding whether to interrupt — drop the segment when `burnStep(...) <= 0`, the same way the other three guards above it already return `a.holdBurn("")` — sev: high — frames: docs/design/polish/frames/chat-late-t4.120x40.txt, chat-late-t5.120x40.txt, chat-resize-final.160x50.txt

3. The rate on the status line contradicts the pulse two rows above it: `5 tok/s` and `30 tok/s` stand beside "nothing has come back yet" — `internal/tui3/render.go:2435` (and the sighting rate at `render.go:1991`) count `a.outputTokens` deltas the pulse at `render.go:1349` does not consider to be the stream speaking — a person watching a stalled turn reads two of this surface's own sentences saying opposite things and cannot tell which to believe; it is the single worst moment to look unreliable — measure the burn from the same signal `waitingWords` reads (bytes of visible answer since `a.lastDelta`), so silence is silence on both rows — sev: high — frames: docs/design/polish/frames/chat-flow-t11.120x40.txt, chat-resize-done.160x50.txt, chat-resize-mid.80x24.txt

4. Every resumed conversation opens by printing its absolute transcript path across four to six rows of the transcript — `internal/tui3/app.go:2327` — it is the first thing on the page, it is longer than most answers, at 60 cols it costs a fifth of the screen and breaks into ragged 17-character stubs, and the fact a person wants there is which conversation this is, not where the journal file lives — say the conversation's name and leave the path to `/status`, or abbreviate through `~` and cut to one row — sev: high — frames: docs/design/polish/frames/chat-md.60x30.txt, chat-md.80x24.txt, chat-longmsg.120x40.txt, chat-home.120x40.txt

5. A wrapped notice loses characters where its rows join — `internal/tui3/render.go:1003` wraps at `width-2` while the rendered row carries a four-cell lead, so the first row overshoots the frame by two cells and is ellipsised, and the three characters it was holding never appear on the next row — at 160x50 the resumed path reads `…/projects/-t…` then `claude-1001--…`, and the `mp-` between them is gone; a path or a command a person was about to copy comes out wrong with nothing saying so — wrap at `width - noteLead - conversation lead` so no row ever needs clipping — sev: high — frames: docs/design/polish/frames/chat-md.160x50.txt, chat-resize-wide.160x50.txt

6. The status line's right-hand segments are pushed off by the phase narration on its left, so cost, context and the watch count disappear and come back mid-turn — `internal/tui3/render.go:1959` builds the rider onto the identity cluster and it is not in `dropOrder` (`render.go:2168`), which the code at `render.go:1962` already admits ("THE LANE LAYER'S OWN RIDER IS NOT ON THE LADDER YET") — across one live turn `2 open`, `◦ keeping an eye on 3` and `$1.12` each vanished as the phase words grew and returned as they shrank; the whole reason this line keeps `$0.00` is so its segments do not jump sideways, and they jump anyway — put the rider on the drop ladder above `segCost` and `segCtx` — sev: med — frames: docs/design/polish/frames/chat-stream-t6.120x40.txt, chat-stream-t7.120x40.txt, chat-stream-t9.120x40.txt, chat-live-t8.120x40.txt, chat-live-t9.120x40.txt

7. The phase words are drawn twice on the same frame, verbatim, two rows apart — `internal/tui3/render.go:1349` (the pulse) and `internal/tui3/render.go:1959` (the status rider) both render `phaseFields` from the same `livePhase()` — the screen reads `·· paced · retry in 2s` and then `… · paced · retry in 2s`, two live things moving in lockstep saying one fact, and it is the duplication that costs row 6 its telemetry — pick one home for the phase: the pulse when the turn is forming, the rider otherwise — sev: med — frames: docs/design/polish/frames/chat-stream-t7.120x40.txt, chat-flow-t7.120x40.txt, chat-resize-post.80x24.txt

8. The clock in the phase line runs backwards — `internal/tui3/phase.go:309` and `phase.go:357` each start their own count-up, so a turn that has been running 19s shows `all lanes slow · still waiting · 10s` and ten seconds later shows `via openinference · first word 4.5s` — a number that goes down while you watch it is read as the program having lost track of itself, and it is on screen next to the elapsed clock that is still climbing — carry one instant for the whole wait and label the phases against it — sev: med — frames: docs/design/polish/frames/chat-late-t1.120x40.txt, chat-late-t2.120x40.txt, chat-flow-t7.120x40.txt, chat-flow-t9.120x40.txt

9. This surface's own notices are indistinguishable from a bullet list the model wrote — `internal/tui3/render.go:1006` leads every note row with `"· "` at the conversation indent, which is exactly the glyph and column markdown bullets land on, and the note block is glued to the block above it with no blank row — under an answer that ends in a list, `· 3 standing orders here — /standing` reads as the model's fourth bullet; the one boundary a transcript must draw is who is talking — give notes their own lead (or at least a blank row above the block) so the surface's voice is not the model's — sev: med — frames: docs/design/polish/frames/chat-md.160x50.txt, chat-md.120x40.txt, chat-md.80x24.txt

10. Below seventy columns the legend is a bare rule with nothing written on it — `internal/tui3/render.go:3056` returns `""` from `legendRight` under `hudTight`, and `render.go:3010` blanks the branch at the same width, so `legendLine` is refused at both ends and `render.go:2873` falls back to `a.rule(width)` — the phone tier is the size where a newcomer most needs `/ commands`, and it is the only size where they are never told it exists; meanwhile the status line one row down still spends sixty cells on `◦ keeping an eye on 3 · $0.12 · 5.8k/1.3M · idle` — keep `microcopy` at every width and drop the branch instead — sev: med — frames: docs/design/polish/frames/chat-md.60x30.txt, chat-longmsg.60x30.txt, chat-draft-resized.60x30.txt, chat-transcript.60x30.txt

11. Lines inside a code fence are truncated with `…` and there is no way to see the rest — `internal/tui3/codeview.go:60` (`codeRows`: "truncated to width and never wrapped") with no expand door and no key named on the block — at 120 cols a 96-column Go line loses its tail and the reader has no signal that the truncation was the renderer's rather than the model's; the same content is whole at 160 and cut at 120, which is the kind of difference that sends someone hunting a bug that is not there — either wrap continuation rows at the fence's own indent, or name the key that opens the block the way `▸ worked … · ctrl+e` does — sev: med — frames: docs/design/polish/frames/chat-md.120x40.txt, chat-md.160x50.txt, chat-scrolled.120x40.txt

12. The wrapped rows of a person's message run flush into the sidebar rail with no gutter — `internal/tui3/render.go:866` wraps the user body at `width-userLeadCols` where the model's prose is measure-capped several cells short of the same rail — one row reads `…and tell me│` with the words touching the divider while every neighbouring row keeps two cells; it is small and it is the first thing the eye catches on the block — take the rail's own cell out of the user body's measure — sev: low — frames: docs/design/polish/frames/chat-longmsg.120x40.txt

13. A ZWJ emoji sequence pushes the sidebar rail four cells right and a VS16 emoji pulls it two cells left — the wrap's width accounting disagrees with the terminal on `👩‍👩‍👧‍👦` and `❤️` only; plain emoji, regional-indicator flags, CJK and combining marks all measure exactly — the divider visibly bends on those rows, and the same miscount is what decides where the line breaks — measure graphemes rather than runes for the ZWJ and VS16 cases in `wrap` — sev: low — frames: docs/design/polish/frames/chat-emoji.120x40.txt, chat-md.120x40.txt

14. One turn counts its tool calls in two words six rows apart — `internal/tui3/workfold.go:446` says `1 tool call` on the fold chip and `internal/tui3/timestamps.go:169` says `1 tool` on the receipt under the same turn — two spellings of one number invite the reader to check whether they are two numbers — pick one noun and have the other call it — sev: low — frames: docs/design/polish/frames/chat-live-t8.120x40.txt

15. A blockquote's gutter bar is the same glyph as the conversation's own right-hand divider — `internal/tui2/tokens/code.go:88` (`GlyphProseQuote = "│"`), drawn at the left margin of the feed while the margin column's divider runs down the right of the same rows — on a screen with no other vertical rules, two `│` columns that mean unrelated things read as one broken frame; the fence beside it already uses `▏` for its own gutter, so the vocabulary exists — give the quote `▏` too, or the dock something else — sev: low — frames: docs/design/polish/frames/chat-md.160x50.txt, chat-md.120x40.txt

16. The money segment changes width mid-turn and shoves the whole right cluster sideways — `internal/tui3/app.go:7617` renders four decimal places under a cent, so a turn passes through `$0.0052` (seven cells) and lands on `$0.01` (five), moving every segment right of it two columns while the person is reading them — the four-place form is right on a receipt and wrong on a line whose stillness is the point — hold the live line at two places and keep the precision for `/cost` and the sheet — sev: low — frames: docs/design/polish/frames/chat-live-t7.120x40.txt, chat-live-t8.120x40.txt

17. A URL or a path is hard-broken at the prose measure while forty columns of the frame sit empty — at 160x50 the answer column stops at 87 cells (right for prose) and the unbreakable tokens break there too, so `…&st` / `ream=true…` splits a link that was going to be copied — the measure cap exists for readability of sentences, and a bare URL is not a sentence — let a single unbreakable token use the full body width before it breaks — sev: low — frames: docs/design/polish/frames/chat-md.160x50.txt, chat-md.120x40.txt

---

## fixed

Nine rows, in the order they were worked. Every fix carries a named test in
`internal/tui3`; the frames below were captured from `bin/aforge` in a real
terminal on socket `polish-fixchat`, against the same demo home, with an
`-after` suffix so the before frames stand beside them.

**1 — a person's own message is drawn whole.** `briefFoldCut` now refuses every
block that is not a node's instruction, which is the gate `briefFoldHidden` (the
door and the key) already had. The two are the two ends of one fold, and only
one of them was firing.
files: `internal/tui3/brieffold.go`
test: `TestALongMessageIsNeverCutWithoutADoor` (`internal/tui3/brieffold_test.go`)
before: `frames/chat-longmsg.120x40.txt` · after: `frames/chat-longmsg-after.120x40.txt`
(and `.160x50`, `.80x24`, `.60x30`)

**2 — a zero rate draws nothing.** `burnSegment` asks the emptiness law of the
FIGURE it is about to draw rather than of the token count behind it. `$0.00` is
untouched: it is this line's one sanctioned zero.
files: `internal/tui3/render.go`
test: `TestAZeroRateDrawsNothingOnTheLiveStatusLine` (`internal/tui3/liverate_test.go`)
before: `frames/chat-late-t4.120x40.txt`

**3 — the rate and the pulse come from one reading.** New `app.awaitingReply`:
a request is out with nothing back from it. `waitingWords` (the pulse), the burn
segment and the served rider's own figure all take it, so silence is silence on
both rows.
files: `internal/tui3/render.go`
test: `TestTheRateIsSilentWhileThePulseSaysNothingHasComeBack` (`internal/tui3/liverate_test.go`)
before: `frames/chat-flow-t11.120x40.txt`

**4 — a resumed conversation opens with its name.** `app.resumedNote` says which
conversation this is — the session's own name, else the opening of the first
thing the person said — on one row at every width. The path is asked for on
`/status`, and the ladder still ends on it, written against `$HOME`, when there
is no name to give. `internal/manual/chat/sessions-and-rewind.md` gained a
section saying so.
files: `internal/tui3/app.go`, `internal/tui3/render.go` (`tildePath`)
test: `TestAResumedConversationOpensWithItsNameAndNotItsPath` (`internal/tui3/chatnotes_test.go`)
before: `frames/chat-longmsg.120x40.txt`, `frames/chat-md.160x50.txt` ·
after: `frames/chat-longmsg-after.120x40.txt`, `frames/chat-md-after.160x50.txt`

**5 — a notice is wrapped to the column it is drawn in.** The note body was
wrapped at `width-2`, allowing for its own `· ` marker but not for THE INDENT
LAW's gutter, so every row overshot by two cells and was clipped — eating three
characters out of the middle of a path. Now `width - noteLead -
workIndentCols(width)`. Pinned at four widths, and against the rows joining back
to the text they were given.
files: `internal/tui3/render.go`
test: `TestAWrappedNoteFitsTheColumnItIsDrawnIn` (`internal/tui3/chatnotes_test.go`)
after: `frames/chat-status-after.80x24.txt` (a long path across four note rows,
no ellipsis, nothing lost at the joins)

**6 — the rider is on the drop ladder.** `statusLayout` gives up, in order: the
segments ranked under `riderRung`, then the identity rider's widest spelling,
then the segments above it. The bill, the context meter and the watch count stop
disappearing and coming back as the phase words grow.
files: `internal/tui3/render.go` (`riderRung`, `dropSegmentUnder`, `dropKind`)
test: `TestTheRidersSpellingGoesBeforeTheBillOnTheStatusLine` (`internal/tui3/liverate_test.go`)
before: `frames/chat-stream-t7.120x40.txt`

**9 — a note does not join the model's list.** A blank row above a note block, so
this surface's own `· ` line is not read as the model's next bullet; a RUN of
notes stays one block, so the opening frame's three lines do not become three
paragraphs.
files: `internal/tui3/render.go` (`deckRows`)
test: `TestASurfaceNoteNeverReadsAsTheModelsNextBullet` (`internal/tui3/chatnotes_test.go`)
before: `frames/chat-md.120x40.txt` · after: `frames/chat-md-after.120x40.txt`

**10 — the narrow legend still names the door.** `legendRight` speaks at every
width. Under `hudTight` the LEFT end gives up the branch (which the shell prompt
behind the pane still says) and the slot keeps `/ commands`, which is written
nowhere else on a frame that narrow. `internal/manual/chat/screen.md` says so,
and `TestTheLegendDropsTheMicrocopyBeforeTheBranch` was rewritten as
`TestTheLegendDropsTheBranchBeforeTheCommandsDoor` — it pinned the defect.
files: `internal/tui3/render.go`, `internal/tui3/bundle_test.go` (the two tests
that pinned the old order)
test: `TestTheNarrowLegendStillNamesTheCommandsDoor` (`internal/tui3/chatnotes_test.go`)
before: `frames/chat-md.60x30.txt` · after: `frames/chat-md-after.60x30.txt`,
`frames/chat-longmsg-after.60x30.txt`

**bonus, found by 10** — widening the hint slot showed it promising a key the
block above it had already refused: a stuck question (`Memo` false) is asked
with a scope aforge cannot save, the offer leaves `[a]` off, and the slot said
`a always` anyway. It now reads the same field the offer reads. Nobody had seen
it because the slot was silent at the only width that question is met at.
files: `internal/tui3/render.go` (`hintWord`), `internal/manual/chat/screen.md`
test: `TestTheHintSlotNamesTheAlwaysKeyOnlyWhereItWouldAct` (`internal/tui3/chatnotes_test.go`)

### not fixed, and why

- **7 (the phase drawn twice)** — the audit's rule ("the pulse when the turn is
  forming, the rider otherwise") is a real design call about which of two rows
  loses its words in every state, and today's behaviour is pinned by several
  tests in `phase_test.go` that state the opposite ranking as law
  (`TestThePhaseClockOutranksTheWaitAndTheSilence`,
  `TestTheServedSegmentDegradesByWhatItsPartsAreWorth`). Rewriting those is a
  decision, not a polish. Row 6 takes most of its cost off the telemetry.
- **8 (the phase clock runs backwards)** — `internal/tui3/phase.go`, another
  lane's file this wave.
- **11 (a fence's lines are truncated with no door)** — `internal/tui3/codeview.go`.
- **14 (`1 tool call` vs `1 tool`)** — `internal/tui3/workfold.go` and
  `internal/tui3/timestamps.go`.
- **15 (the blockquote bar is the dock's glyph)** — `internal/tui2/tokens/code.go`.
- **12, 13, 16, 17** — `sev: low`, and this lane stopped at the med rows. 16 is
  `internal/tui3/app.go:7617` and is a one-line change for whoever takes it.
