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

## Open: for the designer

| # | Question | Assumption the app ships now |
|---|---|---|
| Q1 | File tab for a file that cannot be shown (binary, or over 1MB) | One muted line naming why ("Binary file" / "Too large to show, 3.4MB") and an "Open in ⌄" dropdown listing the editors found on the engine machine (default editor first) plus "Copy path". Same dropdown replaces "Open in editor ↗" when the engine is remote. |
| Q2 | File tab for a file outside git | File view only. No Changes/File toggle and no +/− counts. |
| Q3 | Terminal colours | Programs' 16 ANSI colours flatten to ink, ink-2 and ink-3 (bold = ink, dim = ink-3). Red and green output keep no hue, because the design allows colour only on 6px glyphs. |
| Q4 | Closing a finished job | Close keeps the job's log; it stays listed until removed from its menu ("Remove"). |
| Q5 | "Ask codeaf about this output" target | Starts a new conversation with the output attached. |
| Q6 | Terminal limits | 16 live terminals per conversation; 512KB scrollback each. Not shown in the UI unless hit, then one muted line. |
| Q7 | Terminal shell | The user's shell, interactive, not a login shell, in the project folder on the engine machine. |
| Q8 | History recap writer | A model role ("Titles and summaries", DS Flash by default) writes each conversation's recap when a turn settles. |
| Q9 | Effort row in the model picker for models without effort levels | Hidden. |
| Q10 | Unchosen roles | Stay on DS Flash; they do not follow the Conversation model. |
| Q11 | Task panel button inside the task view | Hidden, as in Conversation 1c. Reopen from the conversation header. |
| Q12 | Task panel open/close motion | No transition specified; the column appears at once. The narrow-width sheet slides and fades. |
| Q13 | "N need you" count | Number of pending questions, the same number the tray shows. |
| Q14 | Message "sending" state | The sent message shows at 60% opacity until the engine records it (plain text sends only). |
| Q15 | Engine data the design shows but the engine does not yet send | Cost per task, "Read at step N", per-step receipts: drawn only when the engine sends them (empty otherwise). |
| Q16 | How "and say why" opens the "Say why (optional)" field on a permission card | The ghost hint is a toggle: pressing it reveals the field under the answers. |
| Q17 | Reason field on irreversible permission cards | Not shown, as in Conversation 1e. An irreversible Deny carries no reason. |
| Q18 | Colour of "Later" / "You decide" on the permission card footer | ink-2 as in Components; the clarification card uses ink-3 as in Conversation 1e. |
| Q19 | Step row ink: Conversation 1a draws settled steps ink-3, Components draws live work-block steps ink-2 | Settled steps ink-3, running step ink, failed step ink-2. |
| Q20 | "Holding up <tasks>" footer note on a question (engine data, no design) | Shown as one muted line in the tray foot. |
| Q22 | Ink for queued, stopped and interrupted rows in the task panel | Queued faint as in 1c; stopped and interrupted use the settled-row ink. |
| Q23 | Start commit for a conversation opened before this rule, or one that began on an unborn branch | The start is recorded the first time the desktop opens the conversation in a git workspace; until then (and while the branch has no commit) diffs compare with the latest commit. |
| Q24 | What the UI says when the start commit is gone (history rewritten) | The engine falls back to the latest commit and answers `base.kind: "head"`; no wording is shipped yet because the design has no slot for it. |
| Q25 | Taking a queued message back (the design draws a remove mark but not what it does) | It really removes the message from the engine, so it never runs. The old line "Removed here only. codeaf still sends a queued message after this turn." is gone. |
| Q26 | A change to a queued message whose turn has already started | The engine refuses (409). The row leaves the queue, and the composer's muted error line says "that message has already been sent". No toast. |
| Q27 | Where a dragged queued row lands | On the row it is dropped on: it takes that row's place and the others shift. No drop line is drawn. Only the visible rows (two, or all when expanded) accept a drop. |
| Q28 | Keyboard reorder | A focused row moves one place with Alt+↑ / Alt+↓ and keeps focus; moving below the second row opens "N more queued". A screen reader hears "Moved to position N of M". |
| Q29 | Editing a queued message that holds a pasted-text card | The field edits the whole stored text, including the `<pasted-text>` block, as plain text. |
| Q30 | ⌘1–3: the Conversation page says they switch pinned models, the Shell spec says ⌘1–9 jump to tabs | ⌘1–3 switch pinned models while a conversation composer is on screen; ⌘4–9 jump to tabs. Tabs 1–3 are reached with ⌃Tab or a click. |
| Q31 | Segment label for a pinned model outside the three defaults | The tail of the model name (for example "kimi-k3"). |
| Q32 | Diff "@@ hunk" header inside an edit's diff (the engine builds edit diffs from the edit, not from git) | No hunk header on edit diffs; file/diff tabs (from git) keep it. |
| Q33 | Elapsed time on the live thinking row ("4s") | Not shown until thinking ends; the engine gives no time before then. |
| Q34 | Icons the design uses that the animated icon set lacks: circle-slash, pencil-line, file-code-2 | Nearest set icons: ban, pencil, the plain file icons. |
