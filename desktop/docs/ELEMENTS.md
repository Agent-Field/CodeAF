# codeaf conversation — element catalog

Every element the desktop conversation draws, the engine data behind it, its
states, and how it behaves. Written for two readers: the build lanes, and a
designer who will draw each element. `docs/CHAT.md` holds the page-level spec;
this file is the inventory beneath it. Field names are the engine's wire names
(`DisplayEntry` and `PlanTaskRow` have no json tags, so they are PascalCase).

Design language: an Apple-built browser drawn by the Arc team. Quiet, warm,
typographic. The person's words and the final answer are the loudest things on
the page; the machinery is present, legible, and folded away until asked for.

---

## 0. Two rhythms

Everything on the page belongs to one of two rhythms. A designer should be able
to tell which one an element is in from across the room.

| | Conversation rhythm | Work rhythm |
|---|---|---|
| What | the person's messages, interim updates addressed to the person, the final answer, deliverables (images, changed files, task results), questions | steps, tool calls, thinking, narration, system notes, task step logs |
| Ink | full text colour | muted text colour, one step quieter |
| Size | prose 13px / 1.6 | label 12px / 1.45; commands in mono 12px |
| Spacing | 28–36px between turns; 12–16px between blocks inside an answer | 4–6px between rows; 10px around a work block |
| Indent | column edge | indented 20px with a 1px guide line at the left |
| Default | open | folded once settled; live while running |

Tokens to add: `space-turn` (32px), `space-answer` (14px), `space-work-row` (4px),
`space-work-block` (10px), `work-indent` (20px), `work-guide` (border colour at 50%).

---

## 1. The turn

A turn is everything from one person message to the next.

```
 [User message]                                   conversation rhythm
 ┊ Work block (folded: "Worked 42s · 4 steps")      work rhythm
 [Interim update]  (0..n, addressed to the person)  conversation rhythm
 ┊ Work block                                       work rhythm
 [Deliverables]   images · changed files · tasks    conversation rhythm
 [Final answer]                                     conversation rhythm
 [Turn footer]    copy · stopped/failed + retry     quiet
```

A turn folds to one line from its gutter chevron: the person's words, then the
digest of the final answer, both on one line.

---

## 2. Conversation elements

### 2.1 User message
- Data: `Role:"user"`, `Text` (literal; never Markdown), `ImageRefs[]` (paths of
  attached pictures), attached files (today only as the sentence
  `attached file: <path>` appended to `Text`; engine work item E3 makes it structured).
- Look: right-aligned soft bubble (`--bubble`), radius 16, max 80% of the column.
  Attachments sit above the text inside the bubble: picture thumbnails
  (64px square, rounded 8, grid), file chips (§5.1).
- Long text: clamp 8 lines + "Show more".
- States: sent · queued (drawn above the composer in muted ink with a queued
  mark until its turn starts) · sending (optimistic, 60% opacity until the
  snapshot has it).
- Hover: Copy, Edit-and-resend (later).

### 2.2 Steer (message typed into a running turn)
- Data: user entry with `Steer {At, Consumed, Landing}`; live events
  `SteerAccepted` → `SteerConsumed` | `SteerFellThrough` (raw kinds 41–43).
- Look: not a bubble. A small right-aligned line inside the running work block,
  with an elbow mark: `↳ also check the tests` and, while pending, the engine's
  landing clause in muted text: "waiting for the running step" / "stopped the
  reply here". On consume the clause fades. Fell through → it becomes the next
  turn's user message.

### 2.3 Interim update (the AI talking mid-work)
- Data: assistant entry with `Addressed:true` (the model wrote `[update]`).
  `Interrupted:true` → cut off.
- Look: conversation rhythm, full ink, Markdown, but preceded by a tiny muted
  eyebrow "Update" and a 2px left rule in the accent at 30%. Not a bubble.
- Cut off: ends with a muted "— cut off" suffix.

### 2.4 Narration (the AI thinking out loud between tool calls)
- Data: assistant entry with `Answer:false, Addressed:false` that is followed by tool calls.
- Rule: never drawn as an answer. It becomes the TITLE of the step that follows
  (§3.2). If no step follows (rare), it is drawn as a work-rhythm line.

### 2.5 Final answer
- Data: assistant entry with `Answer:true` and not `Addressed`. A turn may hold
  more than one; each is drawn; the last one supplies the digest.
- Look: plain Markdown, full ink, no bubble, column width.
- Streaming: text grows in place; no cursor glyph; the work block above shows
  "Writing…" until `AssistantDone`.
- Hover: Copy (bottom-left, appears on hover/focus).

### 2.6 Markdown inside answers
Headings (h1–h3 restrained), paragraphs, lists, task lists (read-only marks),
tables (own horizontal scroll), block quotes, inline code, fenced code (language
label + Copy, wraps), links (§5.2), file references (§5.1), images (§5.3),
horizontal rule. Raw HTML stays off. Mermaid/math: not now.

### 2.7 Turn footer
- Stopped: muted line with the cancelled mark: "Stopped".
- Failed: warning text + "Retry" (resends the last message; draft kept).
- Retrying (raw kind 33): muted "Retrying — the provider was busy" while it retries; withdraws drawn text.
- Copy answer, and (hover) "Worked 42s" link that opens the work block.

---

## 3. Work elements

### 3.1 Work block
- Data: every non-conversation item between two conversation items: narration,
  tool entries, thinking, notes, retries.
- Folded (settled): one quiet line, work rhythm:
  `▸ Worked 42s · thought 6s · 4 steps · 9 calls · 1 failed`
  (only the parts that exist — emptiness law).
- Live (running): open, showing the steps so far and the current step at the
  bottom with its live caption and a still running mark; collapses itself when
  the answer starts streaming.
- Stays outside the fold: tool rows waiting on a decision, failed steps in the
  last step, steers.

### 3.2 Step (one batch of parallel tool calls)
- Data: consecutive tool entries; the batch anchor carries `Caption` +
  `CaptionCategory`; `Took` per call.
- Title precedence: the narration line just before it (§2.4) → `Caption` → a
  composed title from the tools ("Read 3 files in internal/x", "Ran 2 commands").
  Past tense once finished. A narration of one word ("done", the word a landing
  asks the model to say back) is not a title; the next source titles the step.
- Duration: a batch's calls run side by side, so the step shows its longest
  call's `Took`; each call row shows its own. A call the record kept no `Took`
  for shows none.
- Category icon (one per step) from `CaptionCategory`: search · read · edit ·
  create · run · test · browse · transfer · communicate · coordinate · plan ·
  wait · work.
- Row: icon · title · right: duration (`Took`, e.g. "3.2s") · state mark.
- Expands to its calls (§3.3).

### 3.3 Tool call row
- Data: `Tool`, `Hint`, `Args` (JSON, capped), `Output` (capped), `CallID`,
  `Answered`, `Failed`, `Interrupted`, `Took`; live: `ToolForming` →
  `ToolAnnounced` → `ToolBegin` → `ToolFinished` → `ToolEnd`/`ToolFailed`.
- Row: target + stat + state + duration. Target by tool:

| Tool | Target drawn as | Stat | Expanded detail |
|---|---|---|---|
| read | file chip (§5.1) | lines read | excerpt, mono, 30 rows |
| write | file chip | `+N lines` | content preview, 20 rows |
| edit | file chip | `+N −M` (computed from Args pairs; `+` suffix when capped) | unified diff, 2 lines context, 40 rows |
| bash | `$ command` mono, first line | state word and time; exit code in the output box footer | output on the terminal field (command shown once, in the row); footer: exit code, "Show full output" |
| grep / find / ls | pattern / path | match count if known | result list |
| web_search | query in quotes | — | result list as link chips (§5.2) |
| web_fetch | link chip | — | fetched title + excerpt |
| generate_image | prompt excerpt | size | the image (promoted, §5.3) |
| view_image | file chip | — | thumbnail + the model's answer |
| propose_task / tasks | task title | — | task notice (§4.5) |
| manual, ask, jobs, settings… | tool name in words | — | args/output |

- States: forming (dim, "preparing…"), waiting for approval (amber mark, stays
  unfolded, links to the decision tray), running (still running mark +
  elapsed), done (no mark), failed (warning mark, `Failed`), stopped (cancelled
  mark, `Interrupted`), refused (cancelled mark + "refused").

### 3.4 Thinking
- Data: live only (`Thinking`, `Reasoning` events). Not replayed after reload —
  the record keeps it hidden.
- Live: a muted italic block showing the last 3 lines, top fading, under the
  label "Thinking". On the first non-reasoning event it collapses to
  "Thought for 6s" inside the work block; expandable while the page lives.
- After reload: nothing (honest).

### 3.5 System notes
- Compaction (`Role:"note"`): centred divider with "Earlier messages summarized".
- Notice (raw kind 18), harness/orchestration notes: one muted work-rhythm line.
- Session notes to the model (`Role:"aside"`, e.g. "while you worked: A note from
  the session, not from the person: …"): drawn in a person's words. The batching
  label, the lead that instructs the model and `file://` transcript links are
  never drawn; a note of more than one line shows its first line and folds the
  rest under Show.
- Job finished (`AsideKind:"job"`): muted row "Background job finished ·
  `AsideTitle`", expandable body.
- Watch fired (`AsideKind:"watch"`): muted row "Watch fired", expandable.
- Resume (`AsideKind:"resume"`): muted line.
- Task report (`AsideKind:"task"`, `TaskIDs`): task notice (§4.5).

---

## 4. Tasks

### 4.1 Data
- `PlanTaskRow`: `ID, Title, Status (pending|ready|claimed|running|done|failed|cancelled|paused),
  Hold, Stopped, Interrupted, Seat (plan|work|check|probe), Parent, Waits[],
  Steps, USD, Model, Tokens, Started, Ended, Note, Live{Step, Command, Since},
  LiveParts, Archived`; the run root alone carries `Done, Running, Queued, Failed, Total`.
- `PlanTaskPage`: `Row, Description (the brief), Result, Checks[], Notes[{Author, Person, Body, At}],
  Steps[PlanStep], Live, Children[] (all depths, Depth set), WaitRows[], Program`.
- `PlanStep`: `step, command, observation (≤2KB head), full_output (path),
  children[] (tasks it created), not_run, refused, parts[] (display cuts)`.
- Person-facing state words (engine `StateWord`): queued · running · done ·
  incomplete · stopped · interrupted · your call. Plus paused, and "waiting on X" from `Waits`.

### 4.2 Task tree (right panel)
- Header: "Tasks" · progress strip (segments: done / running / queued / failed,
  failures at the end) · "3 of 14" · close.
- Tree: indentation 14px per level with 1px vertical guide lines (Arc sidebar
  tree), disclosure chevrons on parents, families with running work first.
- Row (28px): state glyph · title · right-aligned age (`40s`, `2m`; blank while queued).
  - running: a second line, mono, muted: the live command (`go test ./…`) with its own clock.
  - queued with waits: second line "waits on Parse config".
  - your call: amber glyph, title in full ink.
  - paused: pause glyph.
  - selected: soft selected surface; current task view.
- Hover actions (and context menu): Open in new tab · Pause/Resume · Stop.
- Empty: the panel does not exist.
- Narrow window: a sheet from the right with a backdrop.

### 4.3 Task view (a task is a conversation)
```
 ← Conversation / Parent / Task                         breadcrumb
 Task title                                             heading
 ● Running · step 7 · 2m 14s · DeepSeek v4.1 Flash      state line (only known parts)
 [Pause] [Stop]                                         quiet actions
 ─
 Result (Markdown) …                                    when done — first, it is what the person came for
 Changed files: [chip] [chip]                           when known
 ┊ Work log: 14 commands · 1 refused                    work rhythm, folded when done, open while running
 ┊   $ go test ./...        ✓ 3.1s                      one line per step
 ┊   $ sed -n 1,40p x.go    ✓ 0.1s
 ┊   ● $ make build         running 12s                 live step pinned last
 [Note bubble from you]  "also cover the empty case"    person notes, conversation rhythm
 Note from worker (muted left)                          other notes
 ▸ Instructions                                          the brief, folded
 Under it: child task rows · Waits on: rows
 ─
 [ Note to this task…                     Send ]        task composer
```
- Steps are always bash; draw them as a terminal log, not as tool cards: `$`
  + the display command (apply `parts` cuts: drop `cd <run copy> &&` and plandb
  record shims), exit mark, duration. Expand a line for its output head and
  "Show full output". `refused` steps: cancelled mark + "refused". `not_run`
  without refusal: hidden.
- Notes interleave after steps (no timestamps on steps), person notes as bubbles.

### 4.4 Task composer (talking to a running task)
- Engine seam: `PlanNote(id, text)` — a note the worker reads at its next step
  (`deliverNotes` splices it in as a steer). Also `PlanAmend` (rewrite the brief
  for the next worker), `PlanPause`, `PlanResume`, `PlanCancel`.
- Placeholder: "Note to this task". After send: the note appears as a bubble
  with a muted receipt "Delivered at its next step" → "Read at step 8" once the
  worker picks it up.
- Ended task: composer replaced by "This task has finished" + "Message the conversation".
- Menu: "Change the brief" (amend), "Pause", "Stop".

### 4.5 Task notice (in the conversation)
- Data: task aside (`AsideKind:"task"`, `TaskIDs`) or a proposal/update event.
- One line: state glyph · title (medium) · one-line summary (muted) · chevron on hover.
  Narrow: the title stays whole and wraps; the summary takes a line of its own
  and ends in an ellipsis rather than squeezing the title.
  Click opens the task in this tab; Cmd/Ctrl-click opens a background tab.
- Running tasks: the notice shows the live step in mono muted under the title.

---

## 5. Assets and references

### 5.1 File chip
- Sources: tool `Args.path` (read/write/edit/view_image/generate_image),
  `ImageRefs`, task `Changed`, Markdown links and inline code that resolve to an
  existing workspace path (checked through the engine, never guessed).
- Look: inline pill, height 20, radius 6, field fill: type icon · file name ·
  muted parent dir (middle-truncated) · optional stat (`+12 −3`).
- Click: preview sheet (right side, overlay surface): text with line numbers,
  image, PDF, or "Preview not available" + Open.
- Menu: Open in editor · Reveal in Finder/Files · Copy path · Copy relative path.
- States: exists · missing (muted, strike of the icon only, "not found") ·
  outside the workspace (muted, "outside this workspace", no preview).
- Engine needs: `GET /files?path=` (FetchFile: workspace-confined, symlink-safe,
  16MB cap, MIME by extension) and `POST /files/stat` (StatPaths, 64 per call);
  native `open_path` / `reveal_path` commands confined to the workspace.

### 5.2 Link chip
- Sources: Markdown links (http/https), web_fetch/web_search targets.
- Look: inline pill: site icon (16px) · page title if known, else domain · muted domain.
  Bare URLs in prose become chips; links with custom text stay text links with a
  small site icon before them.
- Site icon: a monogram tile (first letter of the domain on a hue derived from
  the domain) by default. A real favicon only for domains the engine itself
  already contacted in this conversation (web_fetch/web_search), served by the
  bridge (`GET /favicon?domain=`), cached; the renderer never fetches remote content.
- Click: opens in the default browser (native bridge). Hover: full URL.

### 5.3 Images in the conversation
- Generated images (`generate_image`) are deliverables: the step stays in the
  work block, but the image is promoted into conversation rhythm just above the
  final answer as a figure: rounded 12, max 480px wide, caption = prompt excerpt
  + "generated · 1024×1024" muted.
- Several images: a 2-up grid; click any → lightbox (zoom, next/prev, Copy,
  Reveal, Open).
- Images the model only looked at (`view_image`, `read`): stay in the work
  block as 48px thumbnails.
- Person's pictures: thumbnails inside their bubble.
- Markdown images in answers: local workspace path → fetched through the
  bridge and shown; remote URL → a link chip "Image: alt" (no remote fetch).
- Loading: a neutral field-coloured box at the right aspect ratio; failed: file
  chip in missing state.

### 5.4 Changes summary (end of turn)
- Data: union of write/edit `Args.path` in the turn + task `Changed`.
- Look: one conversation-rhythm row after the answer: "Changed 3 files +42 −7"
  and the file chips; expand → per-file diffs.

### 5.5 Other media
- Audio (`speak`, `generate_music`): a slim player row (play, scrubber, time, file chip).
- Video: poster frame + play, opens in the lightbox.
- PDF / documents: file chip; preview sheet renders the first page.

---

## 6. Questions and approvals

### 6.1 Data
`Question {Kind (consent|task|standing|connect|harness|subharness|subharness-ask|landing|conflict|fuel|ask),
Ask (permission|choice|judgement|clarification|confirmation|landing|assumption|ratify),
Form (line|card|room|sheet), Asker {kind: model|engine|task|surface, name}, Head, Reason,
Subject {kind, id, callId, name}, Options[{Key, Label, Body, Consequence, Safe, Widening, Blocks, Dimensions}],
Input {Kind: text|blanks|checklist|pairs|dial, Blanks[], Dial, Prompt, Secret},
Pick {key, reason, confidence}, Stakes (reversible|costly|irreversible), Policy, Blocking {turn, tasks[]},
Batch, Later, Scope[], Attach[Block], Asked, Deadline, Withdrawn}`. Answer goes to
`POST /answer` with `key`/`picked`/`change`/`blanks`/`dial`/`scope`/`decidedBy`.

### 6.2 Decision tray (where questions live)
- One tray docked directly above the composer. It holds every waiting question.
- One question: the tray shows it whole.
- Several: a tab row at the top of the tray, one tab per question (short head,
  asker icon), oldest first; deeper clarifications first. Questions with the
  same `Batch` form one set: tabs + a final "Review" tab that sends all held
  answers at once. A set of only permissions collapses into one card:
  "3 actions need your OK · Allow all · One by one · Deny all".
- Non-blocking questions (`Blocking.turn` false: landings, fuel, designs) sit in
  the tray but never block the composer. Blocking ones say so: "The reply is
  waiting on this".
- Later: "Later" folds the question into a chip in the tray header ("2 waiting").
- In the conversation flow, at the point the question was asked, a quiet
  receipt line: while waiting "Waiting on you: Allow `rm -rf build`?" (click →
  focuses the tray tab); after: "Allowed once · you · 14:02". Withdrawn: "No
  longer needed — the turn moved on". A task proposal that went away (it started
  on its own, or the work stopped waiting) leaves no receipt: the task notice
  and the panel name what became of it. Each question has at most one receipt.
- Option labels are the engine's words: a path, file name or lone word keeps its
  case (`src/c.txt`, `notes.md`); only a plain phrase ("not now") starts with a capital.
- The tab strip shows the "needs you" mark on the tab while anything waits.

### 6.3 Question card anatomy
Head (medium) · Reason (muted) · Evidence (Attach blocks: text, table, diff,
image, diagram) · Answers · Words field · Footer (countdown, Later, You decide).

### 6.4 Answer forms
| Form | Control |
|---|---|
| permission | Allow once (primary) · Always… (opens scope/pattern choice; widening, never on irreversible) · Deny (safe) + "say why" field |
| choice / judgement | option buttons with Body/Consequence; the suggested pick marked "Suggested" with its reason; Safe option is the quiet one |
| checklist | checkboxes (2–8) + Submit |
| blanks | labelled fields by `Kind` (text, path → file picker, number, choice → select, time); defaults prefilled |
| pairs | per row a segmented control: A · either · B |
| dial | slider with min/max labels and the default marked |
| free text / clarification | text area; `Secret` → password field, never echoed |
| landing ("your call" from a task) | Accept · Not right · Tell it… (needs words) · Recheck · Let codeaf decide |
| task proposal | Start it · No; optional model choice; countdown |
| compare | when options carry `Dimensions`: a small comparison table |

### 6.5 Clocks
- `Deadline` set (task proposals auto-start, recommend-then-auto asks): a muted
  countdown "Starts in 12s" + "Hold" (engine `HoldQuestion`; needs a bridge endpoint).
  Typing in the card holds it automatically.
- Irreversible questions never have clocks.

---

## 7. Composer

- Conversation composer: textarea, Attach (files and pictures; paste and drag),
  attachment chips above the text, model label, Send/Stop/Steer, Queue in the menu.
- Queued messages: listed above the composer in muted rows with a remove action.
- Decision tray sits above everything else in the dock.
- Task composer: §4.4.

---

## 8. Live status vocabulary (one still mark + words)

| State | Where | Words |
|---|---|---|
| preparing a call | step row | "Preparing…" |
| running a call | step row | live caption, elapsed |
| thinking | work block | "Thinking" |
| writing the answer | work block | "Writing…" |
| waiting on you | tray, tab, step | "Waiting on you" |
| retrying | footer | "Retrying" |
| compacting | divider | "Summarizing earlier messages" |
| stopped / failed | footer | "Stopped" / "Failed · Retry" |
| task running / queued / your call / done / incomplete / paused | tree, notices | as §4.1 |

No spinners that spin forever; the running mark is still, with elapsed time as the motion.

---

## 9. Engine and bridge work this design needs

| # | Need | Engine seam |
|---|---|---|
| E1 | Task note / amend / pause / resume / stop endpoints | `PlanNote`, `PlanAmend`, `PlanPause`, `PlanResume`, `PlanCancel` on `remote.Agent` |
| E2 | Workspace file read + stat endpoints | `conn.FetchFile`, `StatPaths`; filedoor headers + inline allowlist |
| E3 | Send pictures and files; structured attachments on user entries | `SubmitImage`, `SubmitFiles`; add `Attachments` to `DisplayEntry` |
| E4 | Question hold, receipts and withdrawals | `HoldQuestion`; recent decisions + withdrawn reasons in the snapshot; let `decidedBy:"asker"` through |
| E5 | Map every live event kind the UI uses | Caption, Steer*, ToolForming/Announced/Finished, Retrying, Notice, Compacting, TaskPhase, QuestionWithdrawn/Answered |
| E6 | Task page completeness | trajectory end `reason`/`result`; worker's last words (`PeekReport`); `Changed` files |
| E7 | Native open/reveal/open-URL | narrow Tauri commands, workspace-confined |
| E8 | Favicons for engine-contacted domains | bridge proxy, cached |
