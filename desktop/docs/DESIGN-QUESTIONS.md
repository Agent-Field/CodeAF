# Design questions and assumptions

Open design questions for the designer, with the assumption the app ships
until the designer answers. Every lane that meets something the design
files do not specify adds a row here in the same commit. When the designer
answers, replace the assumption with the answer and mark it **Decided**.

Sources: the claude.ai/design files (Foundations, Components, Conversation,
Shell). On any conflict, the design files win over code and older docs.

## Decided by the owner

| # | Question | Decision |
|---|---|---|
| D1 | Edit a sent message (the design shows a pencil beside Copy) | **No.** Copy only. The pencil is not drawn. |
| D2 | Edit and reorder queued messages | **Yes.** The engine supports edit and reorder; the queue rows show edit, drag and remove. |
| D3 | What a file/diff tab compares against | **Engineering choice:** the commit the conversation started on, so the diff shows what this conversation changed. Falls back to the latest commit when no start commit is recorded. |
| D4 | Pinned models (⌘1–3) | GLM Flash `z-ai/glm-5.3-flash`, DS Flash `deepseek/deepseek-v4.1-flash`, GLM 5.3 `z-ai/glm-5.3`. Every role defaults to DS Flash. |
| D5 | Settings page | Our own page in the design language for now: pinned models, then one row per model role. A full settings design comes later. |
| D6 | Composer focus | No ring, border or fill change on mouse click or typing. A 2px accent ring with a 4px halo shows only on keyboard focus. (The Components "ready to send" specimen shows a halo; the owner's instruction wins.) |

## Decided by the designer (2026-10-09)

| # | Question | Decision |
|---|---|---|
| Q1 | File tab for a file that cannot be shown (binary, or over 1MB) | **Designer: keep.** One muted line naming why ("Binary file" / "Too large to show, 3.4MB") and an "Open in ⌄" dropdown listing the editors found on the engine machine (default editor first) plus "Copy path". Same dropdown replaces "Open in editor ↗" when the engine is remote. |
| Q2 | File tab for a file outside git | **Designer: keep.** File view only. No Changes/File toggle and no +/− counts. |
| Q3 | Terminal colours | **Designer:** keep the ANSI hues, desaturated to about 60% chroma, and only inside terminal and job output (the program's content, not our interface; flattening hides errors). Output inside the conversation's work block flattens to ink. |
| Q4 | Closing a finished job | **Designer: keep.** Close keeps the job's log; it stays listed until removed from its menu ("Remove"). |
| Q5 | "Ask codeaf about this output" target | **Designer: keep.** Starts a new conversation with the output attached. |
| Q6 | Terminal limits | **Designer: keep.** 16 live terminals per conversation; 512KB scrollback each. Not shown in the UI unless hit, then one muted line. |
| Q7 | Terminal shell | **Designer: keep.** The user's shell, interactive, not a login shell, in the project folder on the engine machine. |
| Q8 | History recap writer | **Designer: keep.** A model role ("Titles and summaries", DS Flash by default) writes each conversation's recap when a turn settles. |
| Q9 | Effort row in the model picker for models without effort levels | **Designer: keep.** Hidden. |
| Q10 | Unchosen roles | **Designer: keep.** Stay on DS Flash; they do not follow the Conversation model. |
| Q11 | Task panel button inside the task view | **Designer: keep.** Hidden, as in Conversation 1c. Reopen from the conversation header. |
| Q12 | Task panel open/close motion | **Designer: keep.** No transition specified; the column appears at once. The narrow-width sheet slides and fades. |
| Q13 | "N need you" count | **Designer: keep.** Number of pending questions, the same number the tray shows. |
| Q14 | Message "sending" state | **Designer: keep.** The sent message shows at 60% opacity until the engine records it (plain text sends only). |
| Q15 | Engine data the design shows but the engine does not yet send | **Designer: keep.** Cost per task, "Read at step N", per-step receipts: drawn only when the engine sends them (empty otherwise). |
| Q16 | How "and say why" opens the "Say why (optional)" field on a permission card | **Designer: keep.** The ghost hint is a toggle: pressing it reveals the field under the answers. |
| Q17 | Reason field on irreversible permission cards | **Designer: keep.** Not shown, as in Conversation 1e. An irreversible Deny carries no reason. |
| Q18 | Colour of "Later" / "You decide" on the permission card footer | **Designer:** "Later" and "You decide" use ink-2 on every card (the ink-3 on the clarification card was a design inconsistency). |
| Q19 | Step row ink: Conversation 1a draws settled steps ink-3, Components draws live work-block steps ink-2 | **Designer:** settled steps ink-3, running step ink, failed step ink-2 plus a red 6px dot. Components follows the conversation screens. |
| Q20 | "Holding up <tasks>" footer note on a question (engine data, no design) | **Designer: keep.** Shown as one muted line in the tray foot. |
| Q22 | Ink for queued, stopped and interrupted rows in the task panel | **Designer: keep.** Queued faint as in 1c; stopped and interrupted use the settled-row ink. |
| Q23 | Start commit for a conversation opened before this rule, or one that began on an unborn branch | **Designer: keep.** The start is recorded the first time the desktop opens the conversation in a git workspace; until then (and while the branch has no commit) diffs compare with the latest commit. |
| Q24 | What the UI says when the start commit is gone (history rewritten) | **Designer:** the line reads "Compared with the latest commit. The commit this conversation started on is no longer in history." |
| Q25 | Taking a queued message back (the design draws a remove mark but not what it does) | **Designer: keep.** It really removes the message from the engine, so it never runs. The old line "Removed here only. codeaf still sends a queued message after this turn." is gone. |
| Q26 | A change to a queued message whose turn has already started | **Designer: keep.** The engine refuses (409). The row leaves the queue, and the composer's muted error line says "that message has already been sent". No toast. |
| Q27 | Where a dragged queued row lands | **Designer: keep.** On the row it is dropped on: it takes that row's place and the others shift. No drop line is drawn. Only the visible rows (two, or all when expanded) accept a drop. |
| Q28 | Keyboard reorder | **Designer: keep.** A focused row moves one place with Alt+↑ / Alt+↓ and keeps focus; moving below the second row opens "N more queued". A screen reader hears "Moved to position N of M". |
| Q29 | Editing a queued message that holds a pasted-text card | **Designer: keep.** The field edits the whole stored text, including the `<pasted-text>` block, as plain text. |
| Q31 | Segment label for a pinned model outside the three defaults | **Designer: keep.** The tail of the model name (for example "kimi-k3"). |
| Q32 | Diff "@@ hunk" header inside an edit's diff (the engine builds edit diffs from the edit, not from git) | **Designer: keep.** No hunk header on edit diffs; file/diff tabs (from git) keep it. |
| Q33 | Elapsed time on the live thinking row ("4s") | **Designer: keep.** Not shown until thinking ends; the engine gives no time before then. |
| Q34 | Icons the design uses that the animated icon set lacks: circle-slash, pencil-line, file-code-2 | **Designer: keep.** Nearest set icons: ban, pencil, the plain file icons. |

## Open: for the designer

| # | Question | Assumption the app ships now |
|---|---|---|
| Q30 | ⌘1–3: the Conversation page says they switch pinned models, the Shell spec says ⌘1–9 jump to tabs | ⌘1–3 switch pinned models while a conversation composer is on screen; ⌘4–9 jump to tabs. Tabs 1–3 are reached with ⌃Tab or a click. |
| Q-P1 | Hover delay: Shell 2h says 500ms, the old token said 650ms | 500ms (`interaction.previewOpenDelay`), the same as the tooltip delay. Close grace stays 100ms so the pointer can cross a gap or reach the card. |
| Q-P2 | Hovering the active tab | No card: a preview is for "the thing you'd switch for" (Shell 2h). The old preview on the active tab is gone. |
| Q-P3 | Is the card interactive, and how does the pointer reach it | Yes, because it carries Allow all / Review. The card stays open while the pointer is on it and closes 100ms after it leaves. It is `role="group"` named "Preview of <title>", not a tooltip. Its buttons are not in the Tab order: a keyboard user answers from the tray. |
| Q-P4 | "4 running": what counts | Tasks the engine reports as running (taskState "running"), whole session. With none running but the turn working, the card says "Working". |
| Q-P5 | Which questions a task tab's card asks | Only those whose `blocking.tasks` names the card's task (the link the expanded tasks view reads); a conversation card shows every pending question. A task with none of its own says its own state word, never "Needs you". A set of permissions reads "Allow N actions?" and offers Allow all + Review; a lone permission offers Allow + Review; any other question form offers Review only. Open: a question that names no task (an engine-level ask) appears on the conversation card only. |
| Q-P6 | A conversation that needs you | Same card as a Task that needs you: amber dot instead of the kind icon, "Needs you", the question and the actions (3k draws only the task). |
| Q-P7 | A conversation with no reply yet | "Draft: <text>" when a draft is typed, otherwise only kind and title. A failed conversation uses a red dot and "Failed". |
| Q-P8 | Web card with no screenshot or address (no browser surface exists yet) | The screenshot area keeps its 120px light ground and draws nothing; the address line is omitted. Web, file, diff and terminal tabs are not opened in the live app yet, so these cards ship as specimens plus bodies that read a `target` on the pane (see report). |
| Q-P9 | Terminal and file lines in the card | Last 3 non-empty lines (newest in full ink); first 3 non-empty lines of a file; first 2 changed lines of a diff (design 3k shows 2). ANSI escapes are stripped; the 60% chroma rule is for terminal tabs, not this mono field. |
| Q-P10 | Preview cards on split tabs and on collapsed group pills | Not shown: a split tab's segments and a collapsed group pill open no card. Design draws neither. |
| Q-P11 | Swap animation | No enter animation when a card replaces a neighbour's (data-swap); a first open uses the shared overlay enter. |
| Q-P12 | Allow all fails (engine unreachable, rejected) | The card says why in one danger-colored line ("Not sent. <engine message>"), keeps asking whatever the engine still lists, and leaves Allow all and Review enabled to retry. Open: should the line offer Retry as its own control, as the conversation error does? |
| Q-P13 | Design says "Allow 3 git actions?"; the engine's batch head reads "Allow N actions?" | The shipped wording is the tray's ("Allow N actions?"); the card does not invent a noun the engine did not send. |
| Q-P14 | File, diff, terminal and web cards need a target (path, terminal id, URL) | `Pane.target` is an optional validated field kept across reload (`PaneTarget` in view-state.ts); the kind lanes write it. Until a lane sets it the card is kind and title only, never invented content. |
| Q-P15 | A card whose read fails or is still loading | Kind and title only; no spinner, no error text (a read failure is not the person's to act on). The previous target's content is never shown for a different target. |
