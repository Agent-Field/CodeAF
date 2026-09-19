# Complete UX audit — collaborative workspace (J01–J49, F01–F24)

**Date:** 2026-09-19
**Auditor:** `t-ux-complete-audit` / `cw0918-ux-audit` (Cursor cursor-grok-4.6-high, Spark, read-only of production)
**Status:** independent source+receipt audit. **Not** product acceptance. Do **not** write `releases/folders-entry/ready.json`.

This document is the required journey contract overlay on `USER-JOURNEYS.md` (J01–J43), `FOLDER-FIRST-JOURNEY-AUDIT.md` (F01–F24), `FOLDERS-COLUMN-STEERING.md` (J44–J49), and `REACTIVE-ORGANIZATION-STEERING.md`. It does not replace or weaken those journeys.

## Method and SHAs

Inspected trees (no production edits, no heavy suites):

| Tree | SHA | What it is |
|---|---|---|
| `/home/santosh/src/cw0918-fe-validate2` | `06643b17c713771d4cc57c2598f2e0a3206c8ee6` | Latest Folders-entry **candidate**. Live J36–J43 **failed**. Primary UI/source truth for this audit. |
| `/home/santosh/src/cw0918-fe-live-tui3` | same `06643b17` | Remediation lane running (`t-fe-live-tui3`); `place_folders.go` identical. |
| `/home/santosh/src/cw0918-ux-complete-audit` | `c25d772088abfada66f056effd2ad91422ab03e9` | Wave 4 pin / this worktree. **No** dedicated Folders tab (`placeBarPlaces=4`, `alt+5` = standing). |
| Feature `feat/collaborative-workspace-0918` | `c25d7720` | Same Wave 4 pin. Folders-entry has **not** been merged here. |

Additional evidence SHAs (receipts, not re-run):

| SHA | Claim in receipts | Honest use here |
|---|---|---|
| `4b3b407a` | Wave 1 live J01–J08 | Some panes live; J02 historically CLI; J07 scale incomplete |
| `e606ec55` | Wave 2 live9 J09–J18 | J18 10k **absent** on that run |
| `18e971de` | Wave 3 live5 J19–J26 | J24/J25 overclaimed |
| `b28f36c1` | Wave 4 live4 J27–J35 | J27 used `propose_task`; J31 unit-only; J35 no conflict pane |
| `1fd8b2cd` | Folders-entry integrate + J24 live | J24 parent-conflict **live-pass** |
| `9b0d04d5` | J25 archive Bind skip | **unit/production-path**; live archive deferred |
| `c6491527` | J18 reprove6 recall_a4=1.00 | Search metrics, **not** J07 1000-chat browse |
| `96621c0d` | J38/J41/J43 unit review ok | Live on `06643b17` still failed J38/J41 |
| `eff8d39c` | J42 restart review ok | Live J42 pass on `06643b17` |

**Verdict labels used below**

| Label | Meaning |
|---|---|
| **proven** | Live tmux on a named SHA, isolated `CODEAF_HOME`, pane/transcript shows the **required person-visible path** |
| **unproven** | Software exists; proof is unit, CLI, DB seed, slash-typed harness, or NL-to-tool with remembered IDs |
| **missing** | No discoverable UI, or required behavior absent in `06643b17` source |

CLI, `codeaf collections`, SQLite rows, and slash commands are **not** visible UI proof. A done PlanDB task is not acceptance. `verified:true` on `releases/wave-{1,2,3,4}/ready.json` is treated as a claim to inspect, not a fact.

---

## 1. Screen inventory (`06643b17`)

Global places (`internal/tui3/pages.go`): `placeOrder` = home, tasks, spend, settings, **folders**, standing, memory, search. `placeBarPlaces = 5`. Bar words: `home   tasks   spend   settings   folders`. `alt+5` is Folders; standing/memory/search stay `alt+6`–`alt+8`.

| Screen | Door | File | Logical Folders? |
|---|---|---|---|
| Home | `alt+1`, `/home` | `place_home.go` | Enter-from: heading `folders` opens the place (`home.go`) |
| Tasks | `alt+2` | `place_tasks.go` | Global work identities; not folder-scoped overview |
| Spend | `alt+3` | `place_spend.go` | Ledger; emptiness law |
| Settings | `alt+4` | `place_settings.go` | Memory/organize settings |
| **Folders place** | `alt+5`, `/folders`, Home heading Enter | `place_folders.go` | **Yes** — sequential list, not columns |
| Standing | `alt+6` | `place_standing.go` | Off-bar |
| Memory | `alt+7` | `place_memory.go` | Off-bar |
| Search | `alt+8` | `place_search.go` | Global search; not “add these chats to Billing” |
| Conversation / tabs | leave place, Enter on a chat | `app.go`, `chattabs.go` | Full chat |
| Start / new conversation | `New chat`, `n`, `/folders new` | `chatstart.go` | Pending membership via `pendingFolder` |
| Home `folders` panel | Home grid | `homepanel_folders.go` | Sequential drill + instructions + exec lines |
| Verb strip | `→` | `verbstrip.go`, `placekeys.go` | Overlay; `←`/`esc` close |
| Slash palette | `/` | `commands.go` | `/folders*` vs `/folder` |
| **Filesystem** folder picker | `/folder` `/place` `/dir` | `folderpick.go`, `folderpane.go` | Miller columns — **physical dirs only** |
| Folder name editor | `New folder` / `r` | `place_folders.go` | Create/rename box |

**Column browser (J44–J48 / F05):** **missing** on the logical graph. `place_folders.go` is 80-col sequential drill-in (`esc`/`back`). Finder-style columns exist only for filesystem `/folder`. PRD mockup (`PRD-TDD.md` §6) and `FOLDERS-COLUMN-STEERING.md` are not implemented. Right-hand details pane for the selected logical node is **missing** on the Folders place (home drill paints instructions + exec lines; the place does not).

Wave 4 pin `c25d7720` has no `place_folders.go` at all.

---

## 2. Controls and actions (logical Folders)

### 2.1 Visible rows on the Folders place

| Label (exact) | Activate | Source | Result |
|---|---|---|---|
| heading `folders` | — | `folderPlaceLines` | Optional index clause (`discovery delayed` / `degraded` / counts) |
| `New folder` | Enter / strip `c` | `beginFolderPlaceCreate` | Name editor; `CreateFolder(name)` **always parentless (Root)** |
| `New chat` | Enter / strip `n` | `startFolderPlaceChat` | Start page; files on first send if `pendingFolder` set |
| `Organize existing chats` | Enter / strip `o` | `folderPlaceOrganize` | Enqueues `organize_existing` |
| `cancel` | Enter / strip `x` when job live | `folderPlaceCancel` | Cancels pending/leased survey |
| folder name · `N chat(s)` | Enter | `enterFolderPlaceFolder` | Drill in (or complete move/nest) |
| chat title | Enter | `openFolderPlaceChat` | Opens full conversation |
| `back` · folder name | Enter | `leaveFolderPlace` | Pop trail |
| empty whisper `logical groups of chats · /folders create Billing` | — | `folderPlacePaintWhisper` | Root + no parentless folders; **never** “no folders yet” |
| nil whisper `folders are not wired here` | — | same | `Options.Folders` nil |
| organize status `queued` / `running` / `delayed` / `done` / `cancel` | — | `folderPlacePaintStatus` | Person words; never store `pending`/`leased`/`checked` |

### 2.2 Verb strip (`→`) on Folders place

Base always: `c` New folder, `n` New chat, `o` Organize existing chats. Live job adds `x` cancel.

Merged from `folderVerbs` / `folderCollabVerbs` / `folderExecVerbs` **only if the letter is free** (`mergeFolderPlaceVerbs`):

| Label | Key | Source | Collision on Folders place |
|---|---|---|---|
| `new chat here` | `n` | `folders.go` | **dropped** (base `n` already taken) |
| `add current chat` | `f` | `folders.go` | ok |
| `nest this folder` | `e` | `folders.go` | ok |
| `rename this folder` | `r` | `folders.go` | place-only |
| `instruct this folder` | `i` | `folders.go` | ok |
| `move this placement` | `m` | `folders.go` | chat rows |
| `why here` | `w` | `folders.go` | chat rows |
| `remove this placement` | `x` | `folders.go` | **dropped while organize is live** (cancel takes `x`) |
| `mark this chat` / `unmark this chat` | `k` | `collabact.go` | ok |
| `coordinate these` | `c` | `collabact.go` | **dropped** (base `c` = New folder) |
| `pause coordination` | `p` | `execact.go` | ok |
| `stop work` | `s` | `execact.go` | chat with work |

**Right key:** places map `→` to the verb strip (`placekeys.go`). There is no column drill, so the steering Right-key conflict is **latent**: it will appear the moment columns land unless the contract picks one behavior per context.

### 2.3 Slash vs visible

Visible primary: `New folder`, `New chat`, `Organize existing chats`, membership verbs `f e r i m w x`, mark `k`, pause `p`, stop `s`.

Slash still exists and live harnesses still type it: `/folders create|add|rename|nest|instruct|new|organize`. `/folder` `/place` `/dir` stay filesystem (`TestBareFoldersOpensThePlaceAndFolderStaysFilesystem`).

**No visible row** for: Organize this chat; Add existing chats; Manage this folder; Coordinate selected (label in PRD; TUI says `coordinate these` and the chord is swallowed); Undo placement; Revoke grant; Open details pane.

### 2.4 Home folders panel (not the place)

Heading Enter → Folders place. Rows still sequential-drill (Wave 1 J02/J07). Paints `instructions` (`folderInstructLines`) and launch-state (`folderExecLines`). Verb strip is Wave 1 membership + collab/exec — **no** place-only New folder / Organize existing / rename-on-place.

---

## 3. States

| State | What the person sees | Source |
|---|---|---|
| Empty folder *list* | Whisper `logical groups of chats · /folders create Billing` | emptiness law |
| Empty list + unfiled chats | Unfiled rows still drawn (`folderPlaceRootStops`) | two truths |
| Nil / corrupt store | `folders are not wired here`; mutations refuse | not empty-success |
| Organize queued/running | status line + restable `cancel` | mapped from store |
| Organize budget/cap | `delayed` / `discovery delayed` after 8 unfiled | `organizeSurveyCap` |
| Organizer/embed down | `discovery delayed` or `degraded`; never `checked` | J17 |
| Loading | Snapshot on open/beat, never in View | no spinner; honest empty until memo |
| Offline / provider fail | delayed/deferred; foreground chat usable | J17 units; live mixed |
| Restart mid-organize | same `organize_existing` row resumes (`06643b17` J42 live-pass) | after `eff8d39c` |
| Narrow 80-col | sequential list + back; composer should survive | J43 official **fail**, home-retry pass |
| Wide | still sequential, not columns | J44 **missing** |
| Keyboard | ↑↓ cursor; Enter activate; Esc leave; `→` strip | no Left/Right column |
| Mouse | where existing TUI supports it | not Folders-specific |
| Naming | `type a name · enter create · esc cancel` | known `/…` line dispatches slash (J41 trap) |

**Startup default (F01 product choice):** not implemented as “first visit Root, thereafter last workspace view.” Launch still lands on Home / start conversation. Folders is a tab, not the first-run default.

---

## 4. F01–F24 — required folder-first flows

Status letters from `FOLDER-FIRST-JOURNEY-AUDIT.md` kept. Verdict is this audit’s.

| Flow | Entry → actions → result → exit | Source on `06643b17` | Verdict |
|---|---|---|---|
| **F01** First visit | Open app → Folders → Root. Empty list, unfiled visible, create/organize visible, no fs mirror | Tab exists; startup is Home not Root; J36 live-pass | **unproven** as first-visit default; **proven** tab empty-state |
| **F02** New chat at Root | New chat → send → return; Esc mints nothing | `startFolderPlaceChat`; J38 Esc-at-Root live-pass | **proven** Root path; inside-folder Esc surface **fail** (J38) |
| **F03** Contextual creation | Inside Billing: New folder Receipts + New chat both in Billing | `createLogicalFolder` → `CreateFolder(name)` only (`folders.go:730–746`). No parent. Nest is separate `e` | **missing** (confirmed gap) |
| **F04** Folder conversation | New chat in Receipts; ancestor instructions; cwd unchanged | `pendingFolder` + `i` instruct; `/folder` unchanged | **unproven** (wired; live used slash) |
| **F05** Column navigation | Root → Billing → Receipts → preview → open → back; narrow adapts | Sequential only. No details pane on place | **missing** (owned by `t-rx-*` J44–J48) |
| **F06** Bring existing work | Search/browse saved chats → select several → add to Billing | Only `f` / `/folders add` on **current** chat. Search place does not file. No multi-select | **missing** |
| **F07** Shared placement | Same chat via two folders; Also in; per-placement move/remove | `also in `; nest/shared units; live J41 rename **fail** | **unproven** (identity wired; browse/rename live fail) |
| **F08** Correct organization | Why → Undo/remove → restart/recheck | `w` why; `x` remove. **No Undo**. Restart re-add suppression is J11 | **unproven** (why/remove exist; Undo **missing**) |
| **F09** Organize onboarding | Organize existing → keep chatting; >8 checkpoint | Visible action; 5‑minute tick; **survey always walks Unfiled[0..]** and defers at 8 (`organize_bind.go:74–114`) | **unproven** / current survey **insufficient** (owned by `t-rx-runtime`) |
| **F10** Reactive organization | New message → coalesced background; no 5‑min wait; Organize this chat | After-message enqueue exists; **no event wakeup**; no “Organize this chat” | **missing** (owned by `t-rx-*`) |
| **F11** Guidance | Folder details → edit instructions → inherited sources | `i` + `/folders instruct`. Inspect on **home drill**, not Folders place. No inheritance browser | **unproven** (owned in details by `t-rx-ui` J47) |
| **F12** Start coordination from folder | Manage this folder → ordinary chat + visible scope | **No** person-facing “Manage this folder”. Tool `manage-folder` NL-only | **missing** |
| **F13** Coordinate selected | Select four → Coordinate selected → ordinary chat | `k` mark + `c` `coordinate these` — **`c` dropped** on Folders place. Live used NL+IDs | **unproven** (chord collision) |
| **F14** Find managers later | Folder details → coordinating chats | No details list of coordinators | **missing** (J47 details) |
| **F15** Delegate discussion | Ask one / fan-out / invite two; originals independent | NL `coordinate` deliver/invite. Painted request/reply/sent **not** in live panes | **unproven** |
| **F16** Change scope | Add/remove while coordinator runs | Store dynamic vs selected; `x` live (J29) | **partial / unproven** for dynamic growth |
| **F17** Resolve conflict | Two parents → one discussion → Root | `OpenConflictDiscussion` on `06643b17`; live-pass on `1fd8b2cd` | **proven** on `1fd8b2cd` (not Wave 3 SHA) |
| **F18** Task/work bridge | Implement → open work → return | NL `launch-or-join`; live4 **refused**, model used `propose_task` | **unproven** |
| **F19** Ongoing responsibility | Close views → event/schedule → reopen | Close ≠ pause (J25). Tick does **not** LaunchOrJoin | **unproven** |
| **F20** Stop controls | Pause vs stop vs revoke | `p` / `s` distinct. Live pause weak. **Revoke has no TUI** | **partial** |
| **F21** Folder overview | Busy folder: blocked/running/finished → source | Home exec lines only. No busy overview / details pane | **missing** (J47) |
| **F22** Return/recovery | Quit/restart/offline/budget/provider | J08/J42 restart organize **proven**; J31 crash **unit-only**; budget organize **not on DailyRail** | **unproven** as a whole |
| **F23** Scale/accessibility | Deep tree, keyboard/mouse/search, 80/wide | Sequential 80-col units; columns **missing**; J07 1000-chat **absent**; slash still required for several live paths | **unproven** / columns **missing** |
| **F24** Whole workflow | One session, no CLI/DB/IDs | Never run as specified | **missing** (final gate = `t-ux-validate`) |

---

## 5. J01–J49 — source and proof

### Wave 1 — J01–J08

| ID | Entry / actions / result | Source | Proof | Verdict |
|---|---|---|---|---|
| J01 | Land without folder; send; reopen; Esc blank start | Home + `pendingFolder` / start Esc | Wave 1 live panes | **proven** (home path); Folders-place Esc inside folder **fail** on `06643b17` |
| J02 | Create Billing/Receipts/Security; dual nest; rename; cycle | `CreateFolder`, `AddFolderPlacement`, `ErrCycle` | Units on HEAD; live historically CLI for second parent | **unproven** live nested-from-Root; **units proven** |
| J03 | Chat in Billing; add to Security; also in | `f` / AddPlacement | Wave 1 live | **proven** |
| J04 | Add old chat; move one edge; remove other | `m` / `x` | Wave 1 + Wave 4 `x` | **proven** for current-chat; **not** F06 browse-many |
| J05 | Why; manual edit; `/folder` unchanged | `w`; filesystem picker | Wave 2 live `w` | **proven** |
| J06 | Composer vs other-window placement | snapshot by id+path | units; J43 live mixed | **unproven** on Folders place (J43 official fail) |
| J07 | 1000 chats / 100 folders, 80+wide | sequential panel | live **100 folders**, not 1000 chats | **unproven** (scale: `t-fe-j18-scale` failed) |
| J08 | Quit/reopen; v1 list; damaged DB | store migration | Wave 1 live reopen | **proven** |

### Wave 2 — J09–J18

| ID | Verdict | Notes |
|---|---|---|
| J09–J10 | **proven** | live9 real model retrieval |
| J11 | **proven** | why + remove + no silent re-add (live9) |
| J12 | **unproven** | organizer creates scopes; junk-folder discipline not F09/F10 |
| J13–J14 | **unproven** | instruct wired; visible inheritance/details incomplete |
| J15 | **unproven** | memory-off search live; backfill cursor ≠ survey checkpoint |
| J16 | **unproven** | event/periodic review; 5‑min tick only |
| J17 | **unproven** | delayed/degraded strings exist; organize **not** on standing DailyRail |
| J18 | **overclaimed** then **metrics-pass** | live9 absent; reprove6 recall **measured**; J07 half still open; ready.json still “pass” on absent corpus |

### Wave 3 — J19–J26

| ID | UI entry | Verdict | Evidence honesty |
|---|---|---|---|
| J19 | NL invite planner/critic | **unproven** | Distinct invocations unit+DB; live pane is model table, not painted labels |
| J20 | `k`/`c` or NL+IDs | **unproven** | Live used remembered IDs; `c` swallowed on Folders place; `03-activity.txt` is home |
| J21 | NL manage-folder | **unproven** | CLI folders; no manager list in details |
| J22 | NL deliver/invite | **unproven** | Dual place via CLI find |
| J23 | NL + Bind/Resume | **unproven** | Offline send live; crash matrix unit |
| J24 | software `openConflictIfNeeded` | **proven** on `1fd8b2cd`; Wave 3 ready.json **overclaim** | |
| J25 | `p`; close view; `ctrl+e` put-away → archive on validate2 | pause/close **proven**; archive **unproven** live | |
| J26 | trusted origin stamp | **unproven** live; **units proven** | |

### Wave 4 — J27–J35

| ID | Verdict | Honesty |
|---|---|---|
| J27 | **unproven** | `01-launch.txt`: `launch-or-join` refused; `propose_task` wrote the README |
| J28 | **unproven** | Join spoken; first road was not clean launch-or-join |
| J29 | **partial** | `x` **proven**; steer of person-grant **unproven** |
| J30 | **proven** (narrow) | both roads live |
| J31 | **missing** as live; unit **proven** | receipt admits unit-only; ready.json still pass |
| J32 | **unproven** | tick exit 0 ≠ unattended launch |
| J33 | **unproven** | `03-pause.txt` model: coordination isn’t active; revoke/budget not live |
| J34 | **missing** | emptiness needle, not busy-folder overview |
| J35 | **unproven / overclaimed** | one sentence; no parent-conflict pane |

### Folders-entry — J36–J43 (`06643b17` live)

| ID | Live | Notes |
|---|---|---|
| J36 | **proven** | heading, whisper, three actions, no generated folders |
| J37 | **proven** | `/folder` filesystem; not aliased |
| J38 | **live-fail** | Esc inside Billing stayed `Home new conversation` |
| J39 | **proven** | seeded unfiled at Root (seed is fixture, visibility is UI) |
| J40 | **proven** | visible organize → real job; other composer survived |
| J41 | **live-fail** | nested rename `no folder called Receipts` |
| J42 | **proven** | coalesce + restart same id (after j42-restart) |
| J43 | **unproven** | official drive **fail**; `/home` then M-5 retry **pass**. `t-fe-live-tui3` still running. Home send concatenates (`TestHomeTypingOpensItsOwnConversationEveryTime` red) |

### Columns / reactive — J44–J49

All **missing** in source. Planned on `t-rx-contracts` / `t-rx-ui` / `t-rx-proof`. No tmux wide+80col column screenshots exist that implement logical Folders (filesystem picker shots do not count).

| ID | Required | Source |
|---|---|---|
| J44 | Wide Root>folder>subfolder columns + pinned details | **missing** |
| J45 | Shared object two paths, one identity, Also in | identity **wired**; column two-path **missing** |
| J46 | Chat preview in details then full open/return | **missing** (Enter opens full chat; no preview pane) |
| J47 | Folder instructions/activity/attachments truthful + actionable | **missing** on place |
| J48 | Narrow/wide resize, deep-path windowing, keyboard/help | sequential 80-col only |
| J49 | Async graph updates preserve selection/path/composer; stale detail cannot overwrite | selection units exist; details staleness N/A (no details) |

---

## 6. Receipt honesty (done ≠ accepted)

| Artifact | Overclaim |
|---|---|
| `releases/wave-2/ready.json` | J18 `"pass"` citing `j18-corpus.txt` whose body is `kind: absent` / “not a pass” |
| `releases/wave-3/ready.json` | J24 `"pass"` on coordinate pane (no parent conflict). J25 `"pass"` with all participants `active`, no archive |
| `releases/wave-4/ready.json` | Copies those rows. J27 pass despite `launch-or-join` refusal. J31 pass despite unit-only. J35 pass without conflict |
| `receipts/issue-4-final-ux-audit.md` | Completed in **0s**; names J18 absent + J25 Bind residual, then “no new mandatory gap that blocks Wave 4 ready.json” |
| `TRY.md` Wave 2 | Admits 10k corpus absent then still calls J09–J18 passed |
| Manual `collections.md` | “New folder creates at Root, **or inside the selected folder**” — **false** on `06643b17` (`CreateFolder` has no parent) |
| `t-fe-validate2` | Honest **fail** (J38/J41/home send). Do not relabel |
| `t-final-ux-audit` | Done with unresolved mandatory gaps still listed |

Immutable Wave 1–4 binaries were not touched. Preview `93d688e2` remains a snapshot **without** reactive/column design.

---

## 7. Cost matrix (organization, embeddings, index, retry, idle)

Code: `06643b17`. **MEASURED** = receipt number. **ESTIMATE** = price/cap used as a gate. **UNMEASURED** = no enqueue→start→commit→visible timers in product.

| Trigger | Source | Spend | Budget / coalesce | Wake | Retry | Checkpoint | Class |
|---|---|---|---|---|---|---|---|
| After-message enqueue | `cmd/codeaf/chatv3_folders.go` `v3EnqueueOrganize` | Disk job row only | coalesce `chatID:sourceRev` | **tick only** | resume cancelled/deferred/failed same key | n/a | UNMEASURED |
| Organize existing chats | `wsapi.OrganizeExistingChats` | Disk enqueue; later model+embed | coalesce `organize_existing` | tick | same | **none** — always `Unfiled[0..]` | UNMEASURED |
| Tick / standing pass | `ProcessOrganizeJobs` / `v3OrganizePass` / `codeaf tick` | leases organizer | **4** jobs/pass (`organizePassLimit`); **8** attempts then failed; lease 120s | **`standing.Interval=5m`**. No enqueue wakeup | fence `FinishJob`; invalid → deferred `discovery delayed` | pass bound only | UNMEASURED |
| `doorOrganizer.surveyExisting` | `organize_bind.go:74–114` | RoleOrganize × up to 8 chats + ingest embeds | **`organizeSurveyCap=8`** then deferred | when leased | next lease **re-scans the same first 8 unfiled** if they stayed no-action | **stall risk (code-proven)** | UNMEASURED |
| Journal ingest | `wsdiscover.Store.Ingest` | RoleEmbed batch + FTS; vectors optional | cursor = session+gen+ordinal+content_hash skip | on organize fire | replay same cursor no-op | per-passage, **not** survey backlog | UNMEASURED |
| Query embed | `discovery_adapter.SearchEmbed` | 1 query vector + FTS/cosine | search rank budget | user search | n/a | n/a | **MEASURED** J18 p50/p95 952/1661 ms; embed **$0.00672** reused ingest (`receipts/fe-j18-reprove6/receipt.md`) |
| RoleOrganize | `session.Agent.Organize` TierLow | chat model | ladder; survey vs auto prompt | inside job | JSON plan; source rev check before apply | n/a | UNMEASURED per job |
| RoleEmbed Account | `v3Embedder` | provider `/embeddings` | — | — | — | — | usage **not** folded into session Account |
| Standing DailyRail | `v3StandingDailyRail` | caps tidy + standing firings | profile daily $; 0 = unlimited | 5m | skip when rail hit | — | Organize runner **does not consult DailyRail** — organize can spend after standing is blocked |
| Idle | five-minute tick | crash/restart fallback only | — | **not** event-driven | — | — | ESTIMATE (interval constant); latency UNMEASURED |
| Keystroke / navigation | View / cursor | **none** (law) | — | — | — | — | units: no store in View |

**Pricing provenance:** organize chat cost via `usage.Cost` / `catalog.PriceNow` (`ModelPrice` on standing posture). Embeddings: provider usage/header when present, **not** session ledger. J18 spend gate ≤$5 is an **ESTIMATE threshold**, not a product meter of organize jobs.

**Not in `06643b17` (steering requires):** event wakeup on enqueue; debounce/latest-rev supersede beyond coalesce keys; instrumented enqueue/start/commit/visible timings; durable survey offset; Organize this chat urgent path; compact “Added to Billing · Why · Undo”.

---

## 8. Product choices still unresolved

From `FOLDER-FIRST-JOURNEY-AUDIT.md`. Do not leave them implicit in a lane.

1. **Startup:** first visit Folders Root vs last workspace view. Proposed: first visit Root, thereafter restore. Home overview stays reachable.
2. **Logical vs physical project:** show real cwd/repo when starting work; never map folder name to disk. Reuse existing project selection.
3. **Starting management:** from folder, ordinary scoped chat + goal composer + visible scope; from existing chat, reuse it. No planner/critic template.
4. **Details priority:** activity/needs-you, attached chats/subfolders, coordinating chats, instructions; source-linked decisions only with history.
5. **Archive / delete / remove / stop:** keep placement removal ≠ archive chat/folder ≠ stop execution. Reachable labels; confirm destructive.
6. **Shared folder ordering:** stable per container; auto-add must not reshuffle the selected row.
7. **Right key:** verb-strip vs column drill — one behavior per context, documented in help.

---

## 9. Architecture for remaining work

Simple modules, **disjoint file ownership**, parallel after `t-rx-contracts`, **one serial integrate**, **same-SHA live TUI**.

```
t-rx-contracts
   ├─ t-rx-runtime          workspace/jobs, wsapi organize, session organize, cmd organize_bind/tick wakeup
   ├─ t-rx-ui               tui3 place_folders + new column/details files (NOT folderadd.go, NOT collabview.go)
   ├─ t-rx-proof            e2e/manual/TRY for J44–J49 + F09/F10
   ├─ t-ux-add-old          NEW internal/tui3/folderadd.go (+ test) — F06 picker
   ├─ t-ux-collab-chrome    internal/tui3/collabview.go — painted request/reply/sent
   └─ t-ux-exec             wsexec / tick LaunchOrJoin / execact revoke — F18/F19/F20 revoke
        │
t-fe-live-tui3 (running) ──► t-fe-validate3
        │
        ▼
t-rx-integrate  (must ancestor live-tui3 + rx lanes)
        ▼
t-ux-integrate  (merge t-ux-add-old, t-ux-collab-chrome, t-ux-exec onto that SHA)
        ▼
t-ux-validate   (F01–F24 + J01–J49 live on that SHA; also waits t-fe-validate3 + t-fe-j18-scale)
        ▼
t-fe-ready      BLOCKED until t-ux-validate
```

**Reuse, do not recreate:** `t-rx-*`, `t-fe-live-tui3`, `t-fe-validate3`, `t-fe-j18-scale` (failed; still the J07/J18 scale gate), `t-fe-j24-conflict` (done), `t-fe-j25-archive` (done, live still due inside `t-ux-validate` J25 archive pane).

`t-rx-ui` **owns** F03 (New folder parent), F05/J44–J48 columns+details, F08 Why+Undo visible, F11/F14/F21 details, F12 Manage this folder from details, F13 chord-safe Coordinate selected (`c` vs New folder), Organize this chat visible, Right-key contract. Do **not** start a second Folders-place UI worker.

`t-rx-runtime` **owns** F09 survey checkpoint >8, F10 event wakeup, Organize this chat backend, cost timers, put organize spend on the same DailyRail as standing, greeting/no-action refuse.

---

## 10. New PlanDB tasks (created by this audit)

See control `workers/ux-complete-audit/handoff.md` for IDs after insert. Acceptance is always: isolated `CODEAF_HOME`, real TUI visible actions, pane evidence, named SHA. No GitHub. Spark only. Implementation workers: codeaf from `origin/santos/dev`, pinned SHA/checksum, `z-ai/glm-5.3-flash`.

| ID | Owns | Must not edit | Accept |
|---|---|---|---|
| `t-ux-add-old` | `internal/tui3/folderadd.go` | `place_folders.go` (except one restable hook named in contracts) | F06: search/browse, select several, add to Billing without IDs |
| `t-ux-collab-chrome` | `collabview.go` | organize_bind, column files | F15: management chat shows attributed request/reply/sent from live deliver/invite |
| `t-ux-exec` | wsexec, tick launch, `execact.go` revoke | organize jobs, place_folders | F18 launch-or-join used (not propose_task); F19 tick can continue authorized work; F20 revoke visible and distinct from pause/stop |
| `t-ux-integrate` | merge onto `feat/collaborative-workspace-0918` | no feature invention | one SHA, all lane commits, independent review |
| `t-ux-validate` | live F01–F24 + affected J01–J49 | production except harness | same SHA; CLI/DB seed ≠ pass; blocks `t-fe-ready` |

---

## 11. Bottom line

Folders-entry **wired a tab and three visible actions**. It did **not** deliver Finder columns, contextual create, add-old-chats, reactive organization, folder details, discoverable Manage/Coordinate, busy overview, Undo, or a measured cost story. Wave 1–4 `ready.json` overclaims J18/J24/J25/J27/J31/J35. `06643b17` live J36–J43 is **failed** (J38/J41/home send). `t-fe-ready` must wait for remediation **and** `t-ux-validate`, not for this audit being filed.
