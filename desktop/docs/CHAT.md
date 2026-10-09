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
  unfolds it. Keyboard: the chevron is a button with `aria-expanded`.
- Turn spacing is generous (token `space-xl` between turns); items inside a turn
  are tight.
- New turn arrives → auto-scroll only when the reader was already at the bottom.
  Otherwise show a small "↓ New messages" pill above the composer.

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
- **Question / approval** (`questions` from the snapshot): a card at the end of the
  conversation, above the composer: the ask, option buttons, optional free text.
  Uses `answerEngine`. This is the ONLY card-like element in the conversation.

## 4. Composer

- Empty conversation: the composer sits vertically centred with a single quiet
  greeting line above it (the workspace folder name in muted text, nothing else).
  After the first send it docks to the bottom (shared layout motion, reduced-motion
  instant).
- One rounded field (radius-lg, hairline border, overlay shadow token on focus only).
  Autosizing textarea, 1 → 10 lines then scroll. Placeholder: `Ask codeaf` (`Steer, or queue a message` while working).
- Bottom row inside the field: left = Attach (paperclip IconButton) and the model
  label (muted, e.g. `DeepSeek v4.1 Flash`, a Select only when real routing exists);
  right = Send (arrow-up, primary round IconButton). While running: Send becomes
  Stop (square) when the field is empty; with text, Enter **steers** and the button
  reads Steer; menu (`…`) offers Queue.
- Enter sends, Shift+Enter newline, IME composition never sends, Escape blurs (and
  closes menus first). ↑ in an empty field recalls the last message for editing.
- Drafts persist per tab (and per task route).

## 5. Tasks

- **Panel** (right column, width token ≈ 280px): header `Tasks` + `3 of 5` muted +
  close IconButton. Rows: still state mark (○ queued, ◐ running, ✓ done, × failed,
  – stopped/cancelled muted), title, nothing else. Nesting by indentation only (no
  connector lines). Parents have a disclosure chevron. Running row shows the live
  step in one muted line below the title. Hover reveals nothing extra. Plan errors
  show one muted line at the top.
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
- If the engine is unreachable, the composer shows one muted line
  `codeaf engine is not running` with a Retry button; the draft stays.
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
