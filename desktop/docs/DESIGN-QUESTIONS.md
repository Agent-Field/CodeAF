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

## File lane integration notes

| ID | Question | Working assumption |
|---|---|---|
| Q35 | Diff folds: the file/diff frame (Shell 3e) draws "⋯ N unchanged lines" only between hunks, with the first hunk header at the top and nothing after the last hunk | Folds also draw before the first hunk and after the last (the engine counts those gaps, ENGINE.md), so the reader can open the lines around a change. Each fold opens in place from the file text. |
| Q36 | "Open in ⌄" should list "the editors found on the engine machine, default editor first" (Q1); the engine reports no editor list, only where the file is (`/files/locate`) | The menu lists "Open in editor" (only when the engine runs on this machine), "Copy path" and "Copy relative path". No editor list until the engine can send one. |
| Q37 | The base-gone line: the engine answers `base.kind: head` both when no start point was ever recorded (Q23) and when history was rewritten (Q24) | The Changes view shows "Compared with the latest commit. The commit this conversation started on is no longer in history." whenever the base is the latest commit. For a never-recorded start the sentence is slightly too strong; the designer may want a second sentence. |
| Q38 | A file in git with no changes (a file tab, or a diff tab after the change was undone) | The toggle stays; Changes shows the muted line "No changes." A file tab opens on File, a diff tab on Changes. |
| Q39 | The File view: only "line numbers + text in mono" is drawn | One line-number column (44px, right aligned), text in ink, no syntax colours (the design shows none). Lines cap at 5000 with "Showing the first 5000 of N lines." A cut diff says "The diff is cut at 5000 lines." |
| Q40 | File chip (Interactions): click is the preview sheet, ⌘-click or middle click "opens file tab". Which files, and which tab | A plain click opens the preview sheet for every chip. ⌘-click (Ctrl off the Mac) or a middle click opens a text or code file in a tab: a diff tab for a file the turn edited or wrote, a file tab for any other. Images, PDFs and the rest have no tab and keep the sheet. A second ⌘-click on the same file selects its open tab. Task view and "What changed" rows use the same chip. |
| Q41 | Tab and header glyph "by type: file-code-2, file-json, file-text, image" (3j); the diff tab draws `file-diff` (3e) | The tab glyph is `fileCode` for file tabs and `diff` for diff tabs, whatever the file type; the header glyph is `fileCode`. The animated icon set has file-code, file-text and file-image but no file-json and no file-diff, and a per-type tab glyph needs a change to the shared Tab primitive. |
| Q42 | A file that was deleted (Changes has hunks; File has nothing to read) | File view says "This file was deleted." |
| Q43 | "Open in editor ↗" only when the engine is local and on the app's own host: the app cannot read its own machine name | New native command `host_name` (runs `hostname`), compared with the engine's `host`. In a browser the name is unknown, so the menu shows. The Rust command passes `cargo check`. |
| Q44 | Where the "Open in ⌄" menu sits for a refused file | In the header, where "Open in editor ↗" would be. The body holds only the muted reason ("Binary file", "Too large to show, 3.4 MB"). |
| Q45 | The head at narrow widths (the design draws 1280 only) | The head wraps: name and counts first, the toggle and the handoff on the next row. The file name and folder fade under a mask only while cut. |
| Q46 | Green of "+12": Shell 3e uses oklch(.6 .11 155) in both themes, the app's `success` token is .56 light and .76 dark | New token `diff-count-add` holds the design value for the counts; the add sign and fill follow the design (`success`, `diff-add`). |
| Q47 | Shell 3k: "Web, terminal and file tabs end in a question", but only the terminal ("Ask codeaf about this output") and web (chat-plus) draw it; Shell 3e draws nothing for a file | A file tab has no ask control. Add one ("Ask codeaf about this file", starting a conversation with the file attached) only when the designer draws it. |
| Q48 | The preview sheet is now the click target for a text file chip, and it has no "Open in tab" control | Opening the tab is ⌘-click, middle click, or the sheet's own close and a ⌘-click. No button is added to the sheet. |

## New tab lane notes

| ID | Question | Assumption |
|---|---|---|
| NT1 | New tab (3f): a URL typed or pasted ("Pasting a URL opens a web tab") | Web is a design-only kind (no browser surface is backed), so a URL is treated as a question: the first row reads Ask “https://…” in a new conversation. When a web tab is backed, a URL row replaces it. |
| NT2 | New tab (3f, 4c): the "From history" section with past conversations from the engine's session list, a one-line digest and "See all N in History ⌘↵" | The engine bridge has no session-list endpoint, so the section is not drawn. Past conversations the person still has as open or recently closed tabs appear under Matching. Drawn when the bridge lists sessions and the History tab exists. |
| NT3 | New tab: the "closed 1h ago" age on a recently closed row | Closed tabs carry no close time, so the row says "closed" with no age. Showing the age means stamping the closed list, which the closing lane owns. |
| NT4 | New tab: the "New terminal ⌃`" row | Drawn only while the terminal kind is backed (the terminal tab is a specimen today), so Start holds only "Open file…". The ⌃` key is the design's; the key belongs to the keys lane and is not wired here. |
| NT5 | New tab: what "Open file… ⌘O" does (the design draws the row and key only) | Picking it keeps the field open, focused, with the caption "Type part of a file name."; matching files then list. No native picker: the files live on the engine machine, which a local dialog cannot browse. The ⌘O key is not wired here. |
| NT6 | New tab: which session searches files, since a new tab has no session and makes no engine call | The field searches through the first saved conversation in the workspace; a file tab then reads through the same session. With none saved yet (before the first send) no file rows are drawn. |
| NT7 | New tab: the empty field | Shows the hint "↵ to start a conversation" as in the specimen and lists only the Start rows. Enter on the empty field does what the highlighted Start row does; it never starts an empty conversation. |
| NT8 | New tab: Esc with text typed | The first Esc clears the text; Esc on the empty field closes the tab (the design states only the second). |
| NT9 | New tab: choosing an open tab row | Jumps to that tab and closes the new tab, so a jump leaves no empty field behind. |
| NT10 | New tab: choosing a recently closed row | Superseded by TI10: the closed tab comes back whole where it stood, and the field goes. |
| NT11 | New tab: Enter on the first row while the engine is away | The tab becomes a conversation anyway and keeps the typed words as its draft, with the composer's own Retry line. Nothing is lost. |
| NT12 | New tab: a file row | The tab becomes kind `file` with its path and the session that can read it. The file viewer is not backed yet, so the tab shows the file placeholder until the file lane lands. `path` was added to the persisted tab view for this. |
| NT13 | New tab rows: the design draws square-terminal and file-code-2 at 15px | The set's terminal and file-code icons at 15px (a size on the row only). |
| NT14 | New tab rows: hover versus selection | Pointer movement moves the highlight (one fill, field), so hover and selection never differ. Press uses field-2 for 80ms. |
| NT15 | New tab field: the design draws the card at 600px of content plus 6px padding | The card is 612px wide in total (600 plus padding), shrinking to the card width minus a 16px gutter on narrow windows. |

## Split lane notes

| ID | Question | Assumption |
|---|---|---|
| Q-split-1 | Pane menu "Swap" (Interactions: Close pane · Swap · Maximize) does not say with which pane when a split has 3 or 4 | With two panes "Swap" trades them. With three or four it opens a "Swap with" submenu listing the other panes by title. |
| Q-split-2 | "Maximize" has no drawn state or exit | The pane fills the card, the others stay mounted but hidden (drafts and running work carry on), and the same menu item reads "Restore panes". It is a view only: not saved, and it ends on a tab switch or when the pane count changes. |
| Q-split-3 | Dropping a tab on the content edge of a tab that is pinned or already a 4-pane split | No zones are drawn and the drop does nothing (a pinned tab cannot merge; capacity is 4 panes). The design draws no refusal state. |

## Terminal lane notes

| ID | Question | Assumption |
|---|---|---|
| Q35 | Terminal tab icons: Shell 3c draws a square for Stop and a square-terminal glyph on the tab; the animated icon set has neither | Stop uses the circle-stop icon and the tab uses the plain terminal icon (nearest set icons, as Q34). The lucide `square` and `square-terminal` names are one line each in `Icon.tsx` once the set has them. |
| Q36 | Where a short job log sits in the field: 3c draws the lines at the bottom edge, a live shell fills from the top | A job log reads from the bottom edge until it fills the field (one screenful of line feeds before the first byte); an interactive shell starts at the top, where its prompt is. |
| Q37 | Which tokens give the ANSI hues (Q3 says "about 60% chroma" but not from what) | Red = danger, green = success, yellow = amber, blue = accent, magenta and cyan = the accent's hue turned 60 degrees either way; black = ink-3, white = ink-2, bright white = ink; bright colours mix 22% toward ink. Each hue keeps 60% of its token's chroma and moves in lightness only until it reads at 4.5:1 on the field. The 256-colour cube gets the same 60%; a program's 24-bit colours are drawn as sent. |
| Q38 | Closing a terminal tab: Q4 settles finished jobs, not a live shell | Closing the tab detaches (house rule). A running shell or job keeps running on the engine until Stop or Remove; each counts toward the 16 of Q6. |
| Q39 | Where Q6's "one muted line when hit" appears | The tab still opens and holds one muted line with a "Try again" button; there is no toast. The words are the design's edge state, "16 terminals are open in this conversation. Close one to start another." (the number comes from the engine's 409 sentence, "16 terminals are already running; close one first", which the desktop rewords). |
| Q40 | The header menu (3c draws only the three icons) | Stop, Run again (a finished job only: the same command as a new job tab), Copy output, and Remove job (Remove terminal for a shell). Remove ends it if running, forgets it and its log, and closes the tab. Stop is also the square button; Copy output copies the selection, else the recent plain output. |
| Q41 | What "Ask codeaf about this output" sends and what the new tab is called | The selected text if any, else the last 64KB of plain output, attached as a file. An empty question is allowed (the engine asks "What is going on in this output?"). The new conversation opens in the foreground, named with the question until the engine's title arrives. |
| Q42 | Header text for a shell | Title is the engine's name for it (the shell, or the job's label or command), then "path · terminal" or "path · job"; the home folder shows as ~. |
| Q43 | Keys: "New terminal ⌃`" | Control + backtick on every platform opens a shell under the conversation in front. Inside a terminal, Tab goes to the shell; ⌃Tab and ⌃⇧Tab still switch tabs, and ⌃` opens another. |
| Q44 | Scrollbar and cursor in the terminal field | No scrollbar is drawn (wheel and keys still scroll); the block cursor does not blink (no idle motion). |
| Q45 | Components "Finished job · tab menu" lists Open log · Run again · Close tab ⌘W · Remove job, but the tab context menu belongs to the menus lane and "Open log" has no meaning distinct from selecting the tab | Run again and Remove job live in the terminal header's More menu (this lane). The tab context menu rows wait for the menus lane to read the kind; Open log is taken to mean "select this tab" until the designer says otherwise. |
| Q46 | Where "… earlier output trimmed to the last 512 KB" draws | As the first, dim line of the log, whenever the engine's stream says its kept window no longer starts at byte 0 (`cut`). 512 KB is the engine's `scrollbackBytes`. |
| Q47 | Terminal tab state glyph (3j lists the kind with needs-you and failed) | Failed (red 6px dot) when the program exited with a non-zero code; running stays silent; a terminal never needs you (no engine question is tied to a terminal). A tab learns the exit when its pane has been shown in this window; background terminals have no engine summary feed yet. |

## Rail and shortcut reconciliation

| ID | Question | Decision / assumption |
|---|---|---|
| D4 | Pinned models (⌥⌘1–3) | GLM Flash `z-ai/glm-5.3-flash`, DS Flash `deepseek/deepseek-v4.1-flash`, GLM 5.3 `z-ai/glm-5.3`. Every role defaults to DS Flash. |
| Q30 | ⌘1–3 as pinned models or as tab jumps | **Designer (Interactions, Shortcuts): ⌘1–9 jump to tabs everywhere; ⌥⌘1–3 switch the pinned models; ⌘/ opens all models.** Linux spells them Ctrl 1–9 and Ctrl Alt 1–3. |
| R1 | Linux spellings of ⌥⌘1–3 (pinned models) and ⌘⇧\ (tab overview): the design names only the Mac chords | Ctrl Alt 1–3 and Ctrl Shift A. Several Linux desktops reserve Ctrl Alt digits for workspaces; if so the Models popover is the way in. |
| R2 | ⌘B for the rail: the design lists only ⌘S | ⌘/Ctrl B keeps working beside ⌘S, because it was the key before the shell design. Say if it should go. |
| R3 | The rail the design draws holds Inbox, Now, Places and All places; the engine has no places or inbox endpoint yet | The rail ships Workspace, Activity, Settings and Design system as 32px rail rows (Places `rr` geometry), with the palette field above them. ⌘0, ⌘P, ⌘⇧P and ⌃1–9 are not bound until places exist. |
| R4 | Focus mode: the top 8px brings the strip back (Shell 2h) with no dwell named, while the rail's left-edge peek waits 300ms (Places 9e) | The strip comes back at once on the top 8px; the rail only after resting 300ms on the left 8px. Both are overlays tinted with the frame at 88% and go away when the pointer leaves them; a menu opened from the revealed strip keeps it up until the menu closes. |

## Tab order, groups and reopen (tab integrity)

Sources: Shell 2b (eighteen tabs: loose tabs drawn on both sides of the collapsed group "Release v2.4 3"), Shell
"Groups, split, overview, rail" ("You make one by dragging a tab onto a tab (the target shows "Group"), or by
⌘-selecting and pressing ⌘G … Tasks opened from a conversation join its group automatically"), Shell 3g (Add to group
▸ "New group… ⌘G"), Interactions (Tab: "drag reorders, onto a tab groups"; Group label: "Drag moves the whole group";
Shortcuts: "⌘G Group selected tabs", "⌘⇧T Reopen closed tab"). The tabs audit (findings 3, 4, 5) named the defects.

| # | Where the design is silent or the code disagreed | What ships |
| --- | --- | --- |
| TI1 | Strip order. 2b draws a group between loose tabs; the strip drew pinned, then every loose tab, then every group | `state.tabs` IS the strip order, as in a browser. The reducer keeps two laws after every action: pinned tabs first and in no group, each group's members one run. The strip, ← →, ⌘1–9, Close's next tab and the overview's group order all read that one order; no renderer re-sorts. A save from before the laws loads in the order its strip drew. |
| TI2 | Where a new tab, a new group and a joining tab land | A new tab: last. A new tab in a group, "Add to group" and a drop on a group label: the end of that group. A new group: where the first of its tabs stood. "No group": just after the group it leaves. |
| TI3 | What a drop at a group's outer edge means (it used to always join) | A tab dropped between two members joins. At the outer edge of a group, only a member stays in: a loose tab dropped after a group's last member sits after the group. Joining is the middle of a member (the "Group" target) or the label. The last member of a group carries the group with it. |
| TI4 | Pinning during a drag | A drag never pins or unpins. A loose tab dropped on a pinned tab goes to the head of the loose tabs; a pinned tab stays among the pinned. Pin and Unpin are the menu's. |
| TI5 | "Drag moves the whole group": where it may go | Before or after a loose tab, before or after a whole other group (dropped on its label, before it), never among the pinned. Groups never nest, so a dragged group has no middle target. No keyboard path is drawn; the menu's per-tab "Move to group" stays the keyboard way to regroup. |
| TI6 | "⌘-selecting": what a ⌘-click does to a tab and how a pick looks | ⌘-click (Ctrl-click on Linux) picks or unpicks a tab without selecting it; the active tab is always part of the selection. A picked tab takes the field fill and "Selected" for a screen reader. A plain click, and ⌘G, drop the picks. Picks are never saved. |
| TI7 | ⌘G ("New group… ⌘G") with or without picks, and its name | Groups the active tab with every picked tab where the first of them stands, under the next free "New group" name, without a naming dialog (the same as Create group; Rename is on the label menu). With nothing picked it groups the active tab alone. The Inbox never joins a group. |
| TI8 | Which tasks "join its group automatically" | A task tab opened from a conversation pane joins that tab's group, at the end. A task opened from a loose conversation opens last, as before. |
| TI9 | ⌘⇧T placement (it appended at the end and dropped a group closing had emptied) | Close records where the tab stood (the tab after it, the tab before it, its group) in the closed list, which is saved. Reopen puts it before the tab that followed it, else after the tab that preceded it, else last; never inside another group's run; in its group, made again with its name if closing emptied it, and opened if collapsed. A pane closed out of a split reopens just after the split. The closing toast's Undo is the same action with the place it remembered. |
| TI10 | Reopening a closed split from the new-tab field (it kept one pane) | The closed tab comes back whole under its own ids, exactly as ⌘⇧T would: every pane with its draft, session and terminal binding, the layout, the dividers and the focused pane, where it stood and in its group. The field goes and is not a closed tab. A field that is itself a pane of a split takes in a closed plain tab as that pane; a closed split reopens as its own tab and the field pane leaves the split. (Supersedes NT10's "the new tab becomes that tab".) |
| TI11 | An emptied group name | Takes the next "New group" name no other group has. |
| TI12 | The group suggestion pill (2b "Group the 3 bench tabs as Benchmarks?") | Absent. "3 or more tabs on one repo or topic" needs a repo or topic for every tab and a name for the set; no tab carries a repo and the desktop makes no AI call of its own. It waits for an engine source. |

