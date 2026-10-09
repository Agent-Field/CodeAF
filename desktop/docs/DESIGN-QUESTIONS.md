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
