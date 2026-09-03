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
- **Rows twelve, thirteen, sixteen and seventeen** — `sev: low`, and this lane stopped at the med rows. 16 is
  `internal/tui3/app.go:7617` and is a one-line change for whoever takes it.

---

## fixed — the second pass

Frames prefixed `chat2-` were captured from `bin/aforge` in a real terminal on
socket `polish-chat2`, against the same demo home (and, for the home card, a
freshly seeded one carrying the room fixture). Every fix below was REVERTED and
its test watched to fail before the fix was put back.

**Row 8 — the clock in the phase line runs backwards.** IT IS THE READING, not
the drawing. `now.Sub(news.Since)` is faithful; what is wrong is `Since`. Every
posting layer is honest about the stage it announces — `phaseClock.enter`
(`internal/provider/phase.go`) sets `p.since = now` on each phase change, and a
retry builds a whole NEW clock for its attempt — but a person reads one number
and it is "how long have I been waiting", so a phase change halved it while they
watched. The surface now carries the wait's own instant across every phase of
one wait and hands it to both drawing sites, at the one door every phase comes
through.
files: `internal/tui3/phase.go` (`phaseDesk.waits`, `phaseWaiting`,
`PostPhaseNews`)
tests: `TestThePhaseClockNeverCountsBackwardsAcrossOneWait`,
`TestWorkInProgressKeepsItsOwnClockAndDoesNotInheritTheWait`
(`internal/tui3/phase_test.go`)
reverted: the test reproduces the audit's own frame verbatim —
`all lanes slow · still waiting · 10s` nineteen seconds into the wait.
A stage of WORK keeps its own clock: `running go test · 41s` is about the test.

**Row 7 — the phase words are drawn twice.** DECIDED: **the pulse owns the phase
while it is on the frame; the rider takes it up the moment the pulse is not.**
The pulse is where the answer is about to appear, so it is where the eye already
is; and the status line has a whole cluster of telemetry behind the phase on its
own drop ladder (row 6) which the second copy was spending. No state of a turn
is left without a phase: the instant an answer streams, a call spins or the turn
ends, the rider has it.
The two tests the earlier lane declined for did NOT pin the doubling — one pins
the PULSE's own ranking (phase > wait > silence), the other is a pure-function
test of `rowLed(phaseFields(...))`, and both still pass unchanged. What did
break were four tests that read `servedRider()`/`identityParts()` from a fixture
with the pulse showing; they are rewritten onto the new law with the reason in
their comments, through one named helper (`answerArriving`) that says what state
the rider owns the phase in.
files: `internal/tui3/render.go` (`app.ellipsisShowing`,
`app.pulseHoldsThePhase`, `servedRiderAt`), `internal/manual/chat/screen.md`
tests: `TestThePhaseWordsAreDrawnOnOneRowAndNeverTwice`
(`internal/tui3/phase_test.go`); rewritten:
`TestTheRidersSpellingGoesBeforeTheBillOnTheStatusLine`
(`internal/tui3/liverate_test.go`),
`TestTheIdentityClusterShortensItsRiderRatherThanBeingClipped`,
`TestAHiddenRolesPhaseNeverTouchesTheClock`,
`TestAnotherWindowsWorkNeverTakesThisRow` (`internal/tui3/phase_test.go`)
reverted: the test prints both rows saying `paced · retry in 2s`, which is the
audit's frame.
after: `frames/chat2-phase-after.120x40.txt` — the pulse says
`··· connecting · 0.3s` and the status line carries its whole cluster,
`crew balanced · 2 open · ◦ keeping an eye on 3 · <$0.0001 · 5.3k/200k · 3% ·
⠙ working · 1s`, with no phase on it. `frames/chat2-phase-served-after.120x40.txt`
is the other half of the law: the answer is arriving, the pulse is gone, and the
rider has the words.

**Row 11 — a fence's lines are truncated with no door.** DECIDED: **wrap, at
every width**, through the machinery the phone tier already had. prose/code.go
truncates and says why — "a caller that can scroll should do the cropping" — and
the second half of that sentence is the premise: THERE IS NO HORIZONTAL SCROLL
ANYWHERE ON THIS SURFACE, at 160 columns any more than at 44. A door
(`▸ N long lines · ctrl+e`) was the alternative and it is worse: a line of code
in an answer somebody is reading is the thing they came for, not something to
hide behind a key. It was also worse than the audit had it — at 80 columns the
tail simply stopped, with no ellipsis anywhere on the row.
`phoneMarkdown` is now `segmentedMarkdown` with one flag: fences are re-laid-out
at every width, tables only at the phone tier (a grid that CAN be a grid should
stay one).
files: `internal/tui3/markdown.go` (`segmentedMarkdown`, `renderMarkdownWithCode`),
`internal/tui3/copymode.go` (`copyCodeRow`, `copyRails` — a wrapped row broke the
run `a` selects, and `↳ ` would have reached the clipboard),
`internal/manual/chat/screen.md`
tests: `TestALineOfCodeIsWrappedRatherThanCutAtEveryWidth`,
`TestAWrappedCodeRowIsMarkedAndAnUnwrappedOneIsNot`,
`TestCopyModeTakesAWrappedFenceWholeAndPastesNoMarkers`
(`internal/tui3/markdownwrap_test.go`)
reverted: all three fail; the first prints the 80-column row losing `-=`.
before: `frames/chat-md.120x40.txt` · after: `frames/chat2-md-after.120x40.txt`
(`↳ ▏ } }` under a 116-cell Go line), `.160x50` (whole, unchanged),
`.80x34` and `.60x40` (two continuation rows each, nothing lost) —
`.80x24`/`.60x30` are the same conversation at the audit's own heights, where the
fence sits below the fold.

**Row 14 — `1 tool call` versus `1 tool`.** It was THREE spellings, not two: the
rewind sheet said `N tools` as well. One function now spells it, and the noun is
the CALL because that is what is counted — a turn that ran `bash` four times made
four calls and used one tool.
files: `internal/tui3/timestamps.go` (`toolCallWord`),
`internal/tui3/workfold.go`, `internal/tui3/rewindsheet.go`,
`internal/manual/chat/screen.md`, `internal/manual/chat/sessions-and-rewind.md`
test: `TestOneTurnCountsItsToolCallsInOneWord` (`internal/tui3/workfold_test.go`)
reverted (one site only, which is the real defect): the failure prints
`▸ worked 47s · thought 6.0s · 2 tool calls · ctrl+e` over
`· 19:01 · 47s · 2 tools ·`.

### not fixed, and why

- **Row 12, 13, 15, 16, 17** — untouched by this lane. 15 (the blockquote bar) is
  `internal/tui2/tokens/code.go` and costs five sites in a component library the
  **resident** also draws with (`glyph.go`'s ASCII/nerdfont table, `code_test.go`
  pinning the byte it shares with the spawn tree, `glyphvocab_test.go`'s
  description, `prose/render.go`, `prose_test.go`). Changing a shared glyph for
  one surface's sake is a decision for whoever owns that library. The audit's own
  alternative — give the DOCK something else — is inside `internal/tui3` and is
  the cheaper half if anyone wants it.

---

## fixed — the blockquote's gutter

**Row 15 — a blockquote is marked with a margin, not with a rule.**
THE SHARED-LIBRARY QUESTION WAS SETTLED BY LOOKING, and the answer is that there
is only one reader. `internal/tui2/prose` — the only thing that draws
`GlyphProseQuote` — is imported by `internal/session`, `internal/tui2/tokens`'s
own styler and `internal/tui3`, and by NOTHING in `internal/head` or
`internal/resident`; neither `GlyphProseQuote` nor `GProseQuote` appears anywhere
in the resident, whose only `tui2/tokens` import is one test file. The resident's
spawn tree draws `GlyphTreeVert`, a DIFFERENT named slot that happened to share
the byte, and it is untouched — so the shared constant could move without the
call site growing a choice about who was asking.
`GlyphProseQuote` is `▏` now, the byte the code fence's own gutter already
carries: an eighth of a cell, which is a margin and cannot be mistaken for a
border. It was `│`, the box-drawing rule this surface's vertical rules are made
of, drawn at the LEFT margin of the feed while the margin column's divider ran
down the RIGHT of the very same rows (`chat-md.120x40.txt`, lines 19-20 against
the column at 91).
The anti-drift pins moved with it rather than being weakened: `code_test.go`'s
`TestProseGlyphsShareTheirBytes` names the fence's gutter as the quote's twin
instead of the tree's trunk, `glyph.go`'s width table carries `'▏'`,
`glyphvocab_test.go`'s deliberate-sharing entry says what the two now share, and
`testdata/glyph_parity.golden` was regenerated with `-update-glyph-parity`.
Files: `internal/tui2/tokens/code.go`, `internal/tui2/tokens/glyph.go`,
`internal/tui2/tokens/code_test.go`, `internal/tui2/tokens/glyphvocab_test.go`,
`internal/tui2/tokens/testdata/glyph_parity.golden`.
Tests: `TestAQuotedPassageIsMarkedWithAMarginAndNotWithARule`,
`TestTheQuoteBarSharesTheFencesGutterAndNotTheTreesTrunk`
(`internal/tui3/quotebar_test.go`).
Reverted: the first prints the quote drawn as
`│ a quoted sentence long enough that it has to wrap` and says it drew no gutter;
the second names `"│"` as the spawn tree's trunk.

---

## fixed — the width, the gutter, the link and the bill

Frames prefixed `km-` were captured from `bin/aforge` in a real terminal on socket
`polish-km`, against a demo home freshly seeded by `cmd/aforge-demo-home` — the
seeder gained the fixture these rows need, so every frame below is reproducible
from a clean checkout rather than from a home somebody built by hand. **Every fix
was REVERTED and its test watched to fail before the fix went back**, and the
revert used is named on each row. Two tests did NOT discriminate on their first
draft and are named as such; both were rewritten until they did.

**A note on how the widths were MEASURED, because the audit's own numbers for row
13 are wrong and this is why.** `tmux capture-pane` writes one entry per grid
CELL, so a wide glyph is one character in the text and two columns on the screen,
and a padding cell comes back as a space. Counting `│`'s rune index in the
captured text therefore says nothing about the column it is drawn in — it is off
by the number of wide and zero-width runes in front of it, which is exactly the
thing being measured. The readings below come from tmux itself:
`copy-mode` + `search-forward "│"` + `#{copy_cursor_x}`, which is the grid
column. On that reading the flags row bends and the ZWJ row does not — the
opposite of what row 13 says.

**Row 12 — a person's own words keep the divider's cell.** `railJoin` pads a
conversation row out to `bodyWidth` and writes the seam in the very next column,
so a body row measuring the column exactly ends with its last letter against the
rule. A person's message is the block this happens to, because it is wrapped to
the COLUMN while the model's prose is wrapped to `prose.DefaultMeasure`, several
cells short of it. `userRailGutter` is that one cell, and `userBodyCols` is the
one function both ends of the fold ask for it — the paint, and the door that
counts the rows the paint would make.
files: `internal/tui3/render.go` (`userRailGutter`, `userBodyCols`),
`internal/tui3/brieffold.go` (one call, the twin `userLead`'s own note names)
tests: `TestAPersonsWrappedWordsNeverTouchTheRail`,
`TestTheBriefFoldCountsTheLinesThePaintActuallyMakes`
(`internal/tui3/usergutter_test.go`) — the first walks a ZWJ family, a VS16
heart, a regional-indicator flag, full-width CJK and a combining acute at five
widths and asserts in DISPLAY CELLS, never by slicing `[]rune`
reverted: the paint back on `width-userLeadCols` — `a zwj family at 100 columns:
row 1 of the message is 76 cells wide in a 76-cell column, so it ends against the
rail with no gutter`. **The second test did not discriminate on its first
draft**: it compared the door against an arithmetic it wrote out itself, and it
passed with the fold reverted and the paint fixed. It now takes the painted rows
from the renderer, and skips any width at which the extra cell does not change
the row count — with the fold alone reverted it prints `the paint makes 5 rows of
the message and shows 3 of them, so the door hides 2 — and it says 1`.
before: `frames/km-widths-before.160x50.txt` (a user row of 130 cells with the
rail at 130) and `.120x40.txt` (90 against 90) ·
after: `km-widths-after.160x50.txt` (124) and `.120x40.txt` (89), `.80x24`,
`.60x30` unchanged in shape.

**Row 17 — a link is copied, not read along.** The measure is the length a
SENTENCE is read at, and `wrapper.commit` broke every over-long word at it. That
did two wrong things at once: it broke a token that must not be broken, and it
broke it EARLY — at 160 columns the answer column is 130 cells and the measure is
88, so a link that would have fitted whole came out as `…&st` / `ream=true…` with
forty columns of the frame standing empty. The wrapper now carries two widths:
the measure every row of prose is wrapped to, and the COLUMN, which is the
ceiling an unbreakable token may take before it is broken.
files: `internal/tui2/prose/wrap.go` (`wrapper.ceiling`),
`internal/tui2/prose/render.go`, `internal/tui2/prose/prose.go`,
`internal/tui2/prose/doc.go`
tests: `TestALinkTooLongForTheMeasureTakesTheWholeColumnBeforeItBreaks`,
`TestOnlyTheUnbreakableTokenPassesTheReadingMeasure`,
`TestATokenLongerThanTheColumnIsStillBrokenAtTheColumn`
(`internal/tui2/prose/unbroken_test.go`); each case refuses to run unless it
discriminates — a width no wider than the measure, or a token the measure already
fits, is a `t.Fatalf` and not a pass
reverted: the ceiling back to the measure — the first prints the link split at
`…&st` / `ream=true`, and the third prints `the first piece is 88 cells in a
100-cell column`. The middle one is the guard against OVER-application and was
watched to fail against `newWrapper(figureWidth, figureWidth, …)`, which puts
ordinary prose on 152-cell rows.
before: `frames/km-widths-before.160x50.txt` (the link cut at 88 with the rail at
130) · after: `km-widths-after.160x50.txt` — the link whole at 102 cells and the
path whole at 97.
**And what it does NOT do:** where the token is longer than the column it now
breaks AT the column, flush against the divider (`km-widths-after.120x40.txt`,
90 cells against a rail at 90). That is where every fence, table and blockquote
on this surface already breaks; giving the whole conversation body the divider's
cell — row 12's gutter generalised — is a follow-up for whoever owns
`app.bodyWidth`.

**Row 16 — the bill reserves the room it is going to need.** THE `$0.00`
EXCEPTION IS UNTOUCHED AND NOT RE-LITIGATED: it is still this line's one
sanctioned zero, and this is the same idea carried one step further. The
exception held the segment's PRESENCE still and let its WIDTH move, so one turn
walked `$0.00` (five cells) → `<$0.0001` (eight) → `$0.0052` (seven) → `$0.01`
(five) and shoved everything beside it two and three columns each way. `costCell`
right-aligns the figure inside the room its own spellings need — the sub-cent
floor's eight cells, asked of `subCent` rather than counted a second time, and
more only when the bill has genuinely grown past it. Nothing new is drawn and
nothing stands in for anything unknown.
files: `internal/tui3/render.go` (`costFloorCells`, `costCell`, `splitReserve`,
`paintPart`, `paintParts`)
tests: `TestTheMoneySegmentReservesTheRoomItWillNeedAndNeverShovesTheCluster`,
`TestTheBillIsRightAlignedInTheRoomItsOwnSpellingsNeed`
(`internal/tui3/costcell_test.go`)
reverted: `add(segCost, dollars(…))` — the row test prints `the money segment ran
from column 140 to column 148 at $0.000000 and from column 137 to column 148 at
$0.000004, so the row moved around it`, with both rows underneath it; and
`costCell` reduced to `dollars` makes the unit test print `$0.000000 draws as
"$0.00", 5 cells, in a segment reserving 8`. **The row test did not discriminate
on its first draft**: the telemetry cluster is flushed RIGHT, so a widening
segment pushes everything to its LEFT and the state word at the end never moves —
watching only the right-hand end passed against the reverted fix. It asserts both
edges now, and says so in its own comment.
**Two things the reservation broke and how they were put right.** The padding was
painted INSIDE the figure's span at first, and
`TestTelemetryFadesWithAgeSoStaleNumbersStopCompeting` and
`TestAWaitingQuestionRoutesTheHueAndQuietsEverythingElse` read exactly that span
— `"…\x1b[38;5;243m   $0.10\x1b[39m…"` where they wanted `dim("$0.10")`. The room
is space: it has no ink, and it is not part of what the age ramp, the pointer or
the bound are talking about, so `splitReserve` takes it off in the paint and the
width arithmetic above still sees every cell of it. And the reservation cost
allocations on a hot path — `TestOneScreenScrollOfFourThousandLinesStaysInsideTheAllocationLaw`
went to 224 against a ceiling of 220. **The ceiling did not move.** `paintParts`
was appending with `+=` — a fresh string per segment and per join, four a segment
— and re-painting the same three-cell separator for every one of them; it builds
through two `strings.Builder`s sized from the width the caller is about to
measure, with one separator for the row. The scroll is back to **218**, which is
the figure it stood at before this lane touched anything.
three tests moved onto the new law rather than being weakened:
`TestTheFourSpendSurfacesRenderOneFigure` and
`TestTheMoneySegmentCarriesWhatTheRunningWorkIsSpending` compare the FIGURE
(`splitReserve`) instead of the segment, and
`TestTheWideStatusRowIsByteForByteWhatItWas` carries the new literal — the same
length, the same segments, three cells moved from the gap to the other side of
the crew word.

### NOT FIXED — row 13, and the one measure behind it

**The measure is right and the layer under it is on a different one, and the fix
is one token in `internal/tui3/task.go`, which this lane does not hold.**

`ansi.StringWidth` is `ansi.GraphemeWidth.StringWidth`. Everything in
`internal/tui3` lays out with it, `app.railJoin` pads the conversation row to
`bodyWidth` with it — and the renderer underneath composes its cell grid with
`ansi.WcWidth`, ultraviolet's default, which it only leaves for grapheme widths
if the terminal answers mode 2027. tmux answers no. The two disagree on exactly
two things, and they are the two the frame bends on:

| | `ansi.StringWidth` | `ansi.WcWidth` | tmux 3.4 draws |
| --- | --- | --- | --- |
| `❤️` (VS16) | 2 | **1** | 2 |
| `🇯🇵` (flag) | 2 | **1** | 2 |
| `👩‍👩‍👧‍👦` (ZWJ) | 2 | 2 | 4 |
| `🎉`, `日`, `é` | 2, 2, 1 | 2, 2, 1 | 2, 2, 1 |

`ansi.StringWidth(row) - ansi.WcWidth.StringWidth(row)` predicts the bend
EXACTLY, on every row of the fixture and at every width: the rail sits at grid
column 91 on every row of `km-widths-before.120x40` except the two-heart row and
the two-flag row, which sit at 89. It is not the ZWJ sequence and it is not four
cells; the audit's row was read off rune indexes in a capture, which is the one
reading that cannot answer this.

**Proved, not argued.** `railJoin`'s `ansi.StringWidth(text)` was changed to
`ansi.WcWidth.StringWidth(text)`, the binary rebuilt, and the rail measured again
through tmux's own copy-mode cursor: **column 91 on all twenty rows**, the heart
and flag rows included. The patch was then reverted and `task.go` is byte-clean.
That is the whole change — one selector, and the doc comment above it, which
already says the right thing about why the measure matters and names the wrong
function.

Whoever takes it should know that the same substitution belongs everywhere in the
package that `ansi.StringWidth` decides a layout, `render.go`'s own `wrap` and
`fitWidth` included — the rail is only where the disagreement is visible as a
bent line. A named test wants the sequences in the table above and must assert by
display cell.

**And the fixture is now in the tree.** `cmd/aforge-demo-home/seed_talk.go` seeds
a ZWJ family, a VS16 pair, a flag pair, a CJK run and a combining acute one to a
line under a plain ASCII control line, plus an unbreakable link and path and a
question tuned to fill the column at 120 AND at 160. `frames/km-widths-*` are
captured from it.


## fixed — row 13, which the measure lane declined and proved

**Row 13 — the rail bends under a variation-selector emoji and a flag, and NOT
under a ZWJ family.** The declining lane's mechanism was right and its proof
stands: the layout measures with `ansi.StringWidth` (grapheme widths) while the
renderer beneath composes its grid with `ansi.WcWidth`, and the two disagree
about exactly two things.

**What I did NOT do is hardcode `ansi.WcWidth`,** which was the proven one-line
fix. It is right on tmux and WRONG on a terminal that answers mode 2027, where
the renderer switches to grapheme widths and the layout would then be the one
that is out of step — the same defect with its sign flipped. bubbletea passes
the terminal's answer through to this model as a `tea.ModeReportMsg` after
acting on it itself, so the surface can hold the same fact the renderer acted on
instead of guessing: `internal/tui3/cellwidth.go`'s `cellRuler`, fed in
`app.Update`, read at `task.go`'s `railJoin`. It starts at `ansi.WcWidth`
because that is what the renderer starts at, so a frame drawn before the
handshake finishes is measured the way it is drawn.

Test: `TestTheRailStandsInOneColumnWhateverIsWrittenBesideIt`
(`railbend_test.go`), which asserts the thing a person asserts by looking —
every row puts the rail in the same column — **under BOTH readings a terminal
can give**. Verified by reverting to each hardcoding in turn: `ansi.StringWidth`
fails the never-answered half at column 88 against 90, `ansi.WcWidth` fails the
mode-2027 half at 92 against 90. Neither hardcoding can pass it, which is the
whole reason it is written twice.

The substitution belongs at every layout site in this package that has to line
up with a drawn column; the rail is done because it is the one where being wrong
is a bent line down the frame.

### The manual, in the same change
`internal/manual/chat/tasks.md` — `**Nothing is folded away.**` predated the
family fold and was false; and the foot was described as counting rows when it
counts WORK, which is the whole of row T13. `screen.md` — the unbreakable-token
rule. Both went into EXISTING sections; no new `## ` heading, which has hijacked
retrieval three times in this wave.
