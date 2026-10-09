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
| Q30 | ⌘1–3: the Conversation page said they switch pinned models, the Shell spec said ⌘1–9 jump to tabs | **Designer (Interactions, latest):** ⌘1–9 jump to tabs and ⌥⌘1–3 switch pinned models. The keys wiring belongs to the rail-and-keys and composer lanes. |

## Open: for the designer

| # | Question | Assumption the app ships now |
|---|---|---|
| Q35 | Shell 3g and Components "Context menu" draw ⌥⌘W beside "Close other tabs", while 3l and Interactions give ⌥⌘W to "Close and stop" | ⌥⌘W closes and stops. "Close other tabs" shows no shortcut. "Close and stop" is listed in the tab menu only for a tab whose work is running (3g draws the menu of an idle tab without it; 3l says an idle tab has nothing to stop). |
| Q36 | The tab menu has no "Rename tab" in 3g or Interactions, but the "Rename a conversation" flow says "or use the menu" | "Rename tab" stays in the menu, under Duplicate, so keyboard and trackpad users have a path that does not need a double-click. |
| Q37 | "Reopen closed tab" is no longer in the tab menu (3g, Interactions) | It stays on ⌘⇧T and in the strip's overflow menu. |
| Q38 | Where the Inbox tab comes from: Interactions says the rail's Inbox row opens it; 3l calls it "the pinned tab" and says closed running work lists there | The Inbox tab is created the first time work outlives its tab or an open tab needs the person, pinned first in the strip, with a dot only while something needs you. The rail lane can open it with the `open-inbox` workspace action. |
| Q39 | "Copy link" (⌘⇧C) and "Move to new window" are drawn enabled | Both are listed and disabled. Copy link needs a codeaf link scheme the app does not register, and a second window needs a Tauri multi-window bridge. Neither exists in the engine bridge today. |
