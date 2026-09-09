# Context modal coordinator review

Integration branch `codex/context-modal-20260908`; owner target
`codex/conversation-execution`. This file records independent acceptance, not the
modal worker's implementation account.

## Baseline finding

The supplied `screenshot-current.png` confirms the reported product mismatch at
`b252108d6`: the browser occupies bottom conversation chrome, the earlier chat
error and status remain visually live around it, the unbounded centre column is
sparse on a wide terminal, and the right directory listing has no visible or
functional row target. `/folder` and bare `/attach` enter different modes in
`openContextPick`; `folderPress` deliberately consumes preview-pane clicks.

The reference image is useful for three qualities, not as a literal skin: a
bounded object, clear pane ownership, and an unmistakable active row. Acceptance
requires aforge's own palette, responsive tiers, and a real modal interaction
boundary.

The latest visual review raises that bar beyond putting a border around the old
rows. The parent pane must recede; the active contents pane must keep names and
aligned sizes visually attached; selection, hover, keyboard focus, and persistent
marks must be distinguishable without depending on colour; and the preview must
receive enough of the bounded sheet to read real code or an image. Restrained,
plain-cell file cues are useful, but a private-use glyph or emoji that becomes
tofu is a regression. ASCII and `NO_COLOR` captures therefore remain first-class
acceptance evidence.

## Review gates for the worker result

- `/folder`, `/place`, `/dir`, and bare `/attach` open the same bounded chooser
  in immediate browse mode from the same location rule. An empty recent/project
  list must not refuse a fresh profile.
- The chooser is visually separate from the conversation. Its keyboard, mouse,
  wheel, and backdrop own the interaction until confirm or cancel. Outside
  presses are inert and cannot switch tabs, scroll chat, or trigger actions.
- Draw, caret, hover, press, wheel, breadcrumb, tray, and action targets consume
  geometry recorded by the paint. Review rejects separately reconstructed hit
  rectangles.
- Parent rows, current-directory rows, and directory rows in the right pane all
  hover and activate. A right-pane file becomes the selected preview subject;
  activation/marking remains distinct from hover. Text/image preview bodies are
  scrollable content, not pretend directory rows.
- Raw path bytes remain distinct from scrubbed labels. Late directory/preview
  results cannot overwrite a newer selection, marks, focus, or location.
- Escape and Cancel restore a non-empty draft and prior view with no addition.
  Explicit confirmation alone adds the selected folder/file/mixed context, and
  the next request contains exactly those selections.
- Wide, narrow, ASCII, and NO_COLOR views retain readable names and controls.
  Resize invalidates old screen geometry before another pointer action.
- Dense mixed directories distinguish folder, source/config, image, document,
  media, and other entries with consistent cell-width-safe cues. The cue is not
  the only type signal. Selected-item detail may show reliably cached type,
  size, and mtime; it must not call mtime "last opened" or add per-frame stats.
- Source, JSON, and Markdown previews have bounded syntax colour, dim line
  numbers, safe clipping, and credible multiline strings/comments. Image
  previews retain aspect ratio within their pane and are described honestly as
  portable half-cell output. Empty, unreadable, binary, and unsupported content
  has an explicit fallback rather than dead space.
- The backdrop is coherent on all four sides of the sheet. A paint that fades
  the left side while erasing or leaving live text on the right is not accepted.
- Visual evidence covers `160x44`, `100x32`, and `52x26`, long and Unicode names,
  source, image, hovered right-directory entry, mixed marks, empty/error states,
  and both command doors. Review is side-by-side against the supplied current
  and Yazi captures and records concrete follow-up defects, not title matches.
- Existing explicit `/attach <path>` direct attachment behavior is reported as
  an intentional separate shortcut unless the implementation truly changes it.

## Native-capable fixture and coordinate protocol

`scripts/context-modal-native-fixture.sh setup ROOT` creates siblings, a nested
right-pane directory, Go and text files, long scrollable lists, and a portable
PNG without launching aforge. Its `motion`, `click`, and `wheel` commands emit
real SGR mouse reports suitable for piping to a dedicated tmux pane.

Coordinates are derived from the *captured frame*, never guessed from terminal
width. For each wide and narrow capture, record the visible outer sheet
`left/top/right/bottom`, then read the separators and visible rows from that same
paint:

1. `parent_x`, `current_x`, and `preview_x` are interior cells midway between
   the painted vertical separators; `row_y` is the centre of the exact visible
   entry row being exercised.
2. Emit motion and assert only that painted target lifts; emit press/release at
   the identical coordinate and assert the action agrees with the hover.
3. For the right-pane nested directory, click its captured row and assert the
   breadcrumb/location changes to `child folder/nested`. For `deep.go`, assert
   it becomes the preview subject and source lines appear before any mark/add.
4. Wheel over a list coordinate and a source/image preview coordinate
   separately, asserting only the pane under the captured pointer moves.
5. Click one cell outside each sheet edge and over a visible underlying tab/chat
   target; assert modal state, room, transcript scroll, and draft are unchanged.
6. After resize, capture again and recompute from new painted separators/rows;
   never reuse pre-resize coordinates.

Portable native review must retain raw `.ans`/plain captures and rendered PNGs
for both command doors at wide and narrow sizes, plus before/after captures for
each cross-column activation. Spark output proves terminal behavior only; root's
Mac review is the required graphics and pointer gate.

The fixture also includes multiline Go, JSON, Markdown, a small real-colour PNG,
an unsupported binary/media sample, dense rows, long/Unicode paths, an empty
directory, and an unreadable directory (where the platform permits chmod 000).
These exist to keep the visual pass reproducible, not to prescribe row order.

## Integration constraints

Preserve `2a02ac0bb` from `origin/codex/conversation-execution`, including its
`pulsemoney_test.go` and PR 653 entry. Avoid the test-speed wave's
`tui3_test.go`, Makefile, and harness scripts. The final full `internal/tui3`
run uses `scripts/one-suite.sh`, `GOMAXPROCS=4`, `GOFLAGS=-p=2`, and a 20-minute
timeout. No dev/main/staging update, Mac access, installed-binary replacement,
engine restart, or readiness marker is authorized here.

## Checkpoint review — worker `f324f98c8`

The first two worker commits are a meaningful modal conversion but are not yet
acceptable as the finished visual correction:

- `contextModalOver` currently writes `left spaces + sheet` for covered rows and
  drops the faded suffix to the right. That is the asymmetric backdrop defect
  called out in the latest review; compositing must preserve the faded left and
  right slices around the bounded sheet (or intentionally paint one coherent
  opaque backdrop).
- `folderpane.go` still documents and implements the older "no borders, no rules
  between columns, no colour behind anything" direction. That cannot constrain
  this correction. The rendered evidence must decide whether light separators
  and restrained row backgrounds are needed for pane identity and selection.
- No purposeful file-type cue or selected-item metadata is evident at this
  checkpoint. The final pass needs cell-safe glyph/fallback coverage and must
  keep unknown metadata absent.
- The late-preview identity guard now in the worker tree is the right shape: a
  row hit map cannot be reused after the preview key changes. It still needs the
  real SGR/resize evidence promised above.

These are review findings against an active worker tree, not coordinator edits
to picker implementation. Merge waits for a clean, pushed worker checkpoint and
the promised rendered captures.

## Checkpoint review — active visual pass after `1eaa7de2b`

The worker tree now contains a material, still-uncommitted visual pass: cell-safe
type marks with an ASCII tier, distinct cursor and hover bands, selected-item
size/modification facts, whole-block syntax highlighting, and a compositor fix
that retains both backdrop sides. Those changes address the earlier review in
code, but they are not an integratable result until committed and pushed.

The evidence directory is not yet acceptance-complete. In particular,
`evidence/owner-review/` currently contains only ANSI and plain-text captures;
there are no rendered PNGs for visual inspection. The current named scenes also
do not independently demonstrate both `/folder` and bare `/attach` as the same
modal, an actual image preview, persistent mixed marks, an empty-directory
fallback, or the requested wide/narrow command-door matrix. The 52×26 text
capture is useful for structural fallback but still needs a rendered image and
an eyes-on judgment for clipping and hierarchy.

Before integration, require a clean pushed worker head plus rendered PNGs made
from the captured terminal frames. The coordinator must inspect those images,
exercise the native-capable fixture independently, and record concrete results
for right-pane directory and file activation, per-pane wheel routing, hover,
outside click capture, resize geometry invalidation, draft-preserving cancel,
mixed next-request context, and background room/timer continuity. ANSI style
codes and unit assertions are supporting evidence, not a substitute for that
visual and pointer pass.

## Independent integrated acceptance — `d121363af` plus change entry

The worker's clean `30b472c1f` was merged only after fetching the owner target
at `2a02ac0bb`; the midnight pulse fixture and its PR 653 entry remain in the
history. Independent review then found and fixed two product defects that the
worker evidence had not closed: an empty selected folder left a blank preview,
and a right-pane directory press merely moved it into the middle column instead
of drilling down. Empty previews now say `nothing below here`, and one press on
the painted `deeper/` row changes the breadcrumb to `nested › deeper`. A file
press remains selection/preview only; confirmation is still separate.

The first locked full `internal/tui3` run exposed four regressions. Three were
stale new-test assumptions about modal-relative breadcrumb coordinates, the
search box remaining empty after opening a result, and an empty preview being
blank. The fourth was real: whole-file Chroma highlighting used a hand-built
preview's unsanitized source and preserved `[2J` after dropping ESC. The source
is now made drawable before lexing. The four focused tests and the relevant
race subset passed, followed by a green locked full suite.

### Visual and pointer receipts

`reports/context-modal-evidence/` preserves each inspected frame as raw ANSI,
plain text, and a rendered PNG. `folder-wide` and `attach-wide` have matching
sheet geometry and browse state at 160×44; `folder-mid` is 100×32 and
`attach-narrow` is 52×26. `right-directory-open-fixed` shows the one-press
breadcrumb change, `source-before-wheel` and `preview-wheel` show source and
preview-only scrolling, `image-selected` shows the aspect-fitted half-cell PNG,
and `empty-fixed` shows the explicit empty state. The selected row band stops at
its own column, the right and left backdrop survive, and the compact narrow tier
keeps title, breadcrumb, list, primary action, and cancel legible.

The isolated native fixture is created without launching aforge:

```sh
scripts/context-modal-native-fixture.sh setup /tmp/aforge-context-native
```

Build a review-only binary in this worktree with `make build`, launch it from
the printed `modal project` directory with an isolated `AFORGE_HOME`, and
capture 160×44, 100×32, and 52×26 frames for `/folder` and bare `/attach`.
Resolve pointer cells from the freshly painted separators and row labels, then
emit genuine SGR reports with:

```sh
scripts/context-modal-native-fixture.sh motion COL ROW
scripts/context-modal-native-fixture.sh click COL ROW
scripts/context-modal-native-fixture.sh wheel COL ROW up
```

Expected native captures mirror the committed PNG names. Root must additionally
verify real Mac pointer motion/press/release, resize-recomputed coordinates,
outside click capture, draft-preserving Escape, mixed marks reaching exactly the
next request, and room/timer/background continuity. Spark proves terminal
composition only; portable images remain half-cell resolution and no native
graphics protocol is claimed.

### Checks

- focused modal/manual tests: green
- relevant `-race` subset: green
- locked `go test -timeout 20m ./internal/tui3`: green in 568.581s
- `make test-laws`: green, 55 law files in 16 packages
- manual, untagged e2e word gate, and `internal/tui2/prose`: green
- `make test-packed-manual`: green
- `make changelog-check`: green with PR 659 entry
- `make build`: green, only this worktree's `bin/aforge`

No known-red entry was added. No Mac path, owner profile, installed binary,
engine, dev, main, or staging state was touched.
