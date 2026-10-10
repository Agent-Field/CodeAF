# Conversation surface — build spec

This is the design the conversation rebuild follows. It replaces the "work
document" wireframe language. Where `AGENTS.md` and this file disagree about the
chat surface, this file wins and `AGENTS.md` is updated to match.

Design language: a commercial browser built by Apple, drawn by the Arc team.
Quiet, warm, typographic. Content first. Chrome appears when it is needed and
leaves when it is not. No labels that describe the machine ("Engine connected",
"Original instruction", "Captured plan · read-only", "UI preview", "sample").

## 1. The page

```
┌ tab strip ───────────────────────────────────────────────────────────┐
│                                                     │ Tasks   3/5  × │
│        ‹ Repo audit  /  Count words                 │ ✓ List files   │
│                                                     │ ◐ Count words  │
│                         ╭──────────────────────╮    │ ○ Summary      │
│                         │ the person's message │    │                │
│                         ╰──────────────────────╯    │                │
│   ⌄ Worked · 3 steps                                │                │
│   Assistant prose in Markdown, full column width.   │                │
│                                                     │                │
│   ✓ List files — 4 files                     ›      │                │
│                                                     │                │
│   ╭──────────────────────────────────────────────╮  │                │
│   │ Message codeaf…                          ⏎   │  │                │
│   ╰──────────────────────────────────────────────╯  │                │
└─────────────────────────────────────────────────────┴────────────────┘
```

- Reading column: max width token (≈ 720px), centred in the pane.
- The task panel is a right column, only present when the conversation has tasks.
- No header row. The breadcrumb exists only inside a task (§5).

## 2. Turns

- **User message**: right-aligned soft block (`surface-sunken`-ish token fill,
  large radius, no border), max 80% of column. Literal text, `white-space: pre-wrap`,
  never Markdown. Long messages clamp to 8 lines with "Show more".
- **Assistant reply**: plain prose, no bubble, full column width, shared `Markdown`.
- **Folding**: every turn has a fold chevron that appears in the left gutter on
  hover/focus (always visible on coarse pointers). Folded = the user message on
  one line + the digest in muted text on one line. Click anywhere on a folded turn
  unfolds it. Keyboard: the chevron is a button with `aria-expanded`. A pasted
  block never shows its `<pasted-text>` tag in a folded line, queued row, tab
  label or Up recall (one-line readers use `plainMessage`).
- **Long history**: settled turns fold on their own (the latest stays open; the
  reader's own fold or unfold always wins). Beyond 12 turns (`turnFold.limit`) the
  oldest fold into one "N earlier turns" group that opens in place, each turn
  already a folded line. The "Earlier messages summarized" divider draws only after
  the engine has compacted the conversation, never because the history is long.
  ⌘↑ / ⌘↓ (Ctrl on Linux) step between turns, landing 72px below the top; Esc
  returns to the latest message. Folding keeps the row the reader is on in place.
- Turn spacing is generous (token `space-xl` between turns); items inside a turn
  are tight.
- New turn arrives → auto-scroll only when the reader was already at the bottom.
  Otherwise, once the reader is more than 48px from the end and something is
  live or new, the **Latest** pill appears above the composer; it jumps to the end.
  While the engine works it reads `Latest · Working 1m 13s` (with the shimmer),
  otherwise just `Latest`. It never shows when anchored.
- **Scroll edges**: the top of the reading column fades in once the reader has
  scrolled (clear at 40px, solid at 96px). The dock fades to the canvas over 36px.

## 3. Items inside a reply

- **Tools** (`kind: 'tools'`): ONE quiet line: `⌄ Worked · 3 steps` (running:
  `Running bash · <hint>` with the shared still state indicator). Expanding lists
  each step as one row: tool icon (central registry, by tool family), the hint,
  state mark. A row expands to show args + output in monospace, capped height,
  "Show full output" fetches via `readToolResult` by callId. Failed steps show the
  warning colour on the mark only.
- **Thinking / reasoning**: one line `Thought for a moment` (muted), expandable to the
  text in muted prose. While streaming: `Thinking…` with the still indicator.
- **Task notice** (`kind: 'task'`): a compact row: state mark, task title, short
  summary (≤ 1 line, muted), chevron `›`. Click opens the task (§5). The full aside
  body is never dumped inline; it is available by expanding the row.
- **Note**: centred muted small text between rules (compaction etc).
- **Error**: inline warning text with a Retry button that resends the last user
  message (drafts are never lost).
- **Question / approval** (`questions` from the snapshot): the decision tray above
  the composer (§4.1): the ask, option buttons, optional free text. Uses
  `answerEngine`. In the flow only a quiet receipt line marks where it was asked.

## 4. Composer

- Empty conversation: `EmptyStart` draws one title, "What are we building?", and
  the composer sits centred under it. After the first send it docks to the bottom
  (shared layout motion, reduced-motion instant).
- One rounded field (`radius-dock`, `sh-2` dock shadow, accent halo on focus).
  Autosizing textarea, 1 → 8 lines, then it scrolls under a fade at the top edge.
  Placeholder: `Ask codeaf` (`Steer, or queue a message` while working). The
  design's `@` file hint is withheld: there is no @-file reference yet.
- **Long paste**: a paste over 12 lines (`PASTE_CARD_LINES`) does not fill the field.
  It becomes a card above the text (line count, remove). On send each card travels
  as a `<pasted-text lines="N">…</pasted-text>` block ahead of the typed text; the
  sent bubble reads the block back into a card.
- Bottom row inside the field: left = Attach (paperclip IconButton) and the model
  label (muted, e.g. `DeepSeek v4.1 Flash`, opening an honest one-entry popover);
  right = Send (arrow-up, primary round IconButton). While running: Send becomes
  Stop (square) when the field is empty; with text, the row shows **Queue** (with
  its shortcut, ⌥↵ on Mac, Alt Enter elsewhere) beside an accent **Steer** pill, and
  Enter steers. There is no send-options menu. The row wraps so both stay inside
  320px.
- Enter sends (steers while running), Shift+Enter newline, Alt/⌥ Enter queues,
  IME composition never sends, Escape blurs (and closes menus first). ↑ in an
  empty field recalls the last message for editing; a pasted block returns as its
  card. Shortcut labels come from `keyboard.ts`.
- Drafts persist per tab (and per task route).

### 4.1 Decision tray

- One card above the composer pages through the waiting questions ("N of M"). Amber
  head with the task crumb; body at most 40vh, scrolling under a bottom fade.
  With focus inside the card, ← and → move to the previous and next question and
  stop at the ends (the arrow that cannot move stays at 40% opacity). A text
  field that already holds characters keeps those keys for the caret; an empty
  one does not. A screen reader hears "Question N of M".
- When the reader is scrolled away from the tray, it shrinks to a 40px compact bar
  ("N need you", a one-line summary, Review) and returns on Review.
- Choice cards: one radio card per option, the engine's pick first with a
  "Suggested" tag, then Choose. The countdown ("Picks Suggested in 12s") and Hold
  appear only when the question has a deadline; typing or Hold stops the clock.
- A batch of permissions is one card, "Allow N actions?", with a collapsible command
  list, Allow all and Deny all. **One by one** turns the same card into a pager.
- A withdrawn question's receipt reads "No longer needed. The turn moved on."

## 5. Tasks

- **Panel** (right column, width token `panel` 300px): header `Tasks` + `3 of 5` muted +
  Expand tasks and close IconButtons, then the five-colour progress strip. One 30px
  line per task: a 6px status mark drawn by the shared `StatusMark`, title, done/total
  count on parents, duration. Nesting by indentation with 1px hairline guide lines
  that join an open parent to its children (the tree's only lines; no node cards or
  graph). Parents have a disclosure chevron. Queued rows lead with what they wait on.
  The list has edge masks and a "Finished" fold. Hover shows row actions. Plan errors
  show one muted line at the top.
- **Expanded tasks** (Expand tasks): a tab-local route stop, hash `#tasks`, so Back,
  Forward and reload treat it like a task. It shows filter tabs with counts, the
  progress strip, search, groups (done of total, duration), and rows with the live
  command or the waiting reason and a state word column. The chosen row persists and
  fills the right-hand detail pane: parent crumb, title, state with step and age, the
  pending question (through the tray's question card), Model, Steps, Cost and Started
  only when the engine provides them, Instructions, Open task.
- The panel opens automatically the first time a conversation gets tasks; closing
  it is remembered per tab. A `Tasks · 3/5` toggle button lives in the composer's
  bottom row only when tasks exist and the panel is closed. Narrow widths
  (< planSplit breakpoint): the panel becomes an overlay sheet from the right.
- **Task view**: clicking a task (row or notice) navigates the tab to it. The task
  is drawn as a conversation with the same components:
  - user message = the task's Description (task-authored, Markdown allowed),
  - tools group = its Steps (+ the Live step while running),
  - assistant reply = its Result (Markdown); Checks as a quiet list under it,
  - child tasks = task notice rows,
  - Notes = notes.
  Data from `readTaskPage`; refresh while the task is running.
- **Breadcrumb** (task view only): a slim line at the top of the reading column:
  `‹ Conversation title / Parent / Task`. Each segment is a link; `‹` is Back.
  Cmd/Ctrl+[ and Cmd/Ctrl+] go back/forward. Modifier-click on a task opens it in a
  background tab. The composer in a task view is disabled with the placeholder
  `Message the main conversation to change this task` and a button that returns there
  (engine has no per-task steering API).

## 6. Engine attachment and AI calls

- No "Connect engine" button. A new tab is quiet; the first Send creates the
  session (`connectEngine()`), then sends. Saved tabs reattach automatically on open
  (`connectEngine(sessionFile)`). No provider call happens until the person sends.
- If the engine is unreachable, one muted line under the header reads
  `Reconnecting to the engine…` while the client retries. After 30s it reads
  `Can't reach the engine` with a quiet Retry. Nothing turns red. The line
  disappears when the engine answers. A plain-text send is held in that pane, drawn at 60% opacity, and goes out in order when the engine answers. A draft that was not sent stays. A send with files stays in the composer.
- **Separate AI calls** — exactly one, and it is the engine's: the conversation
  title, generated asynchronously after the first message by the engine's title
  lane on the auxiliary role (same fixed model), delivered via the snapshot `title`.
  Tab label = manual name ‖ engine title ‖ first line of the first message (≤ 40
  chars) ‖ `New conversation`. The UI makes no AI calls of its own: digests are the
  first line of the final answer, task summaries come from task records.
- Samples, prototype documents and the "UI preview" selector are removed from the
  workspace. Fixtures live only in tests and in the Design system specimen, which
  says "Specimen".

## 7. Tab strip and chrome

- Tab label: title only. No "· preview", no status text. One still state mark at
  the leading edge (working / needs you / failed); completed shows nothing.
- Sidebar: brand + search, then navigation items. Remove the duplicate icon row
  and the "Personal space" caption.

## 8. Quality bar

Low cyclomatic complexity, small files (< 250 lines), readable formatting
(one statement per line), no inline styles, tokens only, shared primitives only,
icons only through `Icon.tsx`. Every control has hover/pressed/focus/disabled.
Light, Dark, 320 → 1440px, reduced motion.
