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
| Q30 | RESOLVED by the latest Interactions page: ⌘1–9 jump to tabs and ⌥⌘1–3 switch pinned models, so the two no longer share keys | ⌘1–9 jump to tabs; ⌥⌘1–3 pick pinned models. No conflict remains. |
| OV1 | The earlier Shell spec opened the overview with ⌘↑; the latest Shell and Interactions pages say ⌘⇧\ (or a pinch out) and give ⌘↑/⌘↓ to stepping between messages | ⌘⇧\ (Ctrl Shift A off the Mac) and the grid icon open it; the overview no longer listens for ⌘↑. |
| OV2 | Pinch out opens the overview (Shell 2h) | Not wired: the browser and the webview report no reliable pinch event. The grid icon and the keys open it. |
| OV3 | A section for pinned tabs (the design shows only a group section and "Other tabs") | A "Pinned" section comes first when any tab is pinned, then each group in strip order, then "Other tabs". The filmstrip uses the same order. |
| OV4 | Card state line shows "4 running" and "step 7" (engine detail), and the footer shows "2m · Flash" (age and model) | The state line shows only Working, Needs you or Failed from the conversation summary; the footer shows the age only. The summary carries no running count, step or model, and the card never invents them. |
| OV5 | Card body per kind (diff lines, terminal lines, a web page thumbnail) | The body is the kind's `preview` renderer when the kind has one. Until the hover-preview lane fills it, the body is the draft, else the latest answer's first line, else the muted line "No work yet". A split card lists its pane titles. No kind draws a thumbnail yet (only a web tab may, and none is backed). |
| OV6 | What the cursor looks like in the grid (the design draws only hover and the active ring) | The cursor card takes the hover fill (`--field`), with no outline. Arrow keys move it, ↵ opens it, ⌘W / Ctrl W closes it. Clicking a card always opens it. |
| OV7 | What a click on a filmstrip neighbour does | It moves that card to the centre; a click on the centre card opens it. |
| OV8 | Which filmstrip panes are live (the design shows every pane live) | The centre card and three on each side mount the real pane, inert and aria-hidden, with no-op actions; cards further out draw an empty canvas, which is masked or off screen. Panes with no saved session show their empty state, because the filmstrip never fetches or invents content. |
| OV9 | The overview ground and the top bar on non-Mac platforms (the design draws macOS traffic lights) | A full-window layer on `--frame`. On the Mac a 68px gutter keeps the native traffic lights clear; elsewhere there is no gutter. The bar is a drag region. |
| OV10 | Which view opens (Grid or Filmstrip) | The last one used, kept in this browser only. |
| OV11 | "Open as split" for a group of fewer than two plain tabs, or a pinned or already split member | The button is shown but disabled, matching the group's menu item. At most four tabs merge. |
| OV12 | The Search field's focus ring (the design draws none) | None after a click; the shared 2px ring only for keyboard focus, as for the composer (D6). |
| OV13 | ⌘W inside the overview from the native menu | The browser key closes the card under the cursor. The native menu's Close item still closes the active tab and the overview (rail-and-keys lane owns it). |
| OV14 | Interactions: an overview card "⌘-click / middle" = "Background tab", but every card is already an open tab | Not wired. A ⌘-click opens the tab like a plain click. Open question for the designer: should it select the card without leaving the overview? |
| OV15 | Card footer "2m · DS Flash" (age and model name) | Age only. The snapshot carries the model id, but `TabSummary` has no model field and no shared display-name helper exists outside the composer, so the card never guesses one. Needs `model` on `TabSummary` (conversation lane). |
| OV16 | "Drag regroups" on an overview card, no drop targets drawn | A card drags onto a group section to join it, or onto "Other tabs" to leave its group. Pinned is not a drop target. Keyboard equivalent: the right-click menu "Move to group". |
