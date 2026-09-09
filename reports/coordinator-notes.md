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
