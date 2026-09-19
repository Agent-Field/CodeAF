# Collaborative workspace — four serial usable slices

**Status:** approved for execution. Four GitHub issues are created from this plan. Implementation proceeds issue by issue on Spark.

| | |
|---|---|
| Product checkout | `/home/santosh/src/codeaf-collaborative-workspace-0918` |
| Branch | `feat/collaborative-workspace-0918` |
| Inspected baseline | `santos/dev` `7cda67c9a066b9c805e4327054a814e0c52c0ef9` |
| Design commit (pre-issue-1) | `610a32ba4cdf04053e61e0e14703304842e4b844` |
| Design | `docs/design/collaborative-workspace/` |
| Journeys | [`USER-JOURNEYS.md`](USER-JOURNEYS.md) J01–J35 |
| Engineering | [`ENGINEERING.md`](ENGINEERING.md) |
| Owner try | [`TRY.md`](TRY.md) |
| Control dir | `/home/santosh/src/codeaf-workspace-0918-control` |
| Owner branch override | stay on this branch; do not merge or push to `dev`, `santos/dev`, `staging`, or `main`; do not force-push |

Required product intent is P1–P12 and A1–A22 in the PRD. **Default** means a recommended implementation choice this plan settles so the four issues can be built. Defaults do not reopen required behavior. Supervising review and the owner's collaboration clarification **replace** earlier draft defaults that contradicted them.

GitHub issues (filled after `gh issue create`):

| # | Title | Issue | Journeys |
|---|---|---|---|
| 1 | Folders you can see: Root, shared membership, and new chats that stay themselves | _pending_ | J01–J08 |
| 2 | Semantic discovery, automatic filing, and scoped folder instructions | _pending_ | J09–J18 |
| 3 | Inspectable collaboration: ordinary chats coordinate, with optional shared discussion | _pending_ | J19–J26 |
| 4 | Safe execution: launch-or-join, authority, and unattended recovery | _pending_ | J27–J35 |

---

## 1. What is already true in this tree

Inspected against HEAD on this branch (worker-harness waves are already in history; they are not future work).

**Logical collections already exist, with no TUI.**

- `internal/workspace` is an independent SQLite store at `home.Join("v3", "collections.db")` (`application_id` `0x4146434c`, schema v1).
- Tables: `collections(seq,id,name)` and `memberships(seq,collection_id,kind,ref_id,session_id,target_collection)` with unique active `(collection_id,kind,ref_id,session_id)`.
- Shared membership, transactional cycle check, idempotent add/remove, bounded 10s lock wait, refuse foreign/future/damaged DBs, never reset to empty. `Open` is read-only; schema is created on first write.
- Boundary law: `workspace` must not import `session`, `tui*`, `store`, `standing`, `provider`, `remote`, `enginehost`.
- CLI: `cmd/codeaf/collections.go`. Manual: `internal/manual/chat/collections.md` still denies UI, automatic organization, inherited instructions, and inter-chat communication.

**“Folder” on the live surface means a filesystem directory.**

- `/folder` `/place` `/dir` choose filesystem context (`folderpick.go`). `/workspace` anchors a project. Home’s `projects` panel and `homefolders.go` / `homeband_folders.go` are filesystem places a conversation is “also about”.
- Home `o` is “open folder” (filesystem). Home `t` starts a chat in that **project**. Home `a` archives. `ctrl+t` is new-chat tab. `+` opens a start page; the conversation is minted on the **first message**.
- Seven places only: home, tasks, standing, memory, spend, search, settings (`pages.go`). Adding an eighth place would renumber `alt+` chords and fight the home mission-control ruling.

**Transcripts, search, mailbox, assignment, tick, hosts.**

- Journals: versioned JSONL, single-writer (`internal/session/sessionfile.go`). Opaque `chat:` refs already exist.
- `search_conversations` is lexical FTS5 (`internal/store/thread_search.go`) and is **absent unless learned memory is on**. `v3Memory` returns nil when `config.MemoryEnabledAt` is false; `v3SearchSeam` shares that store. A18 cannot be met by leaving search gated on memory.
- Mailbox is **local to one session** (main + task rooms). Comment in `mailbox.go` states cross-session routing is absent.
- Assignment revisions require person-origin (`assignment.go`). Agent restatements cannot mint person authority.
- Both execution roads are live: `CODEAF_TASK_BELT` unset → session task tree; `CODEAF_TASK_BELT=bash` → run/plandb. Issue 4 must not assume one global plan DB.
- Standing tick: in-window pass plus OS `codeaf tick`. Engine hosts idle-retire; they are not perpetual folder intelligence.
- Roles: `internal/roles` is an open registry with `callRoleChecked` accounting. No embedding client exists in `internal/provider`.
- No `@` mention picker exists.
- PERF: home keys/motion run no command and walk no directory; `app.View` reads memos, never the disk; no model call on render.
- Known-red file is absent (ratchet burned to zero). Do not recreate it to hide reds.
- Live keys are present on this Spark. `tmux` and `make` are installed. Values were not printed.

**Seam details that pin the wiring**

- Conversation id is 16 hex, minted in `cmd/codeaf/chatv3_layout.go` `v3MintSession` at `$CODEAF_HOME/v3/projects/<workspace-with-slashes-as-dashes>/<id>/transcript.jsonl`.
- Filesystem referred places (`session/places.go`) are capped at 16 and live on `meta.json`. That cap is not logical-folder membership.
- Local mailbox already has `deliveryID` + `durableDelivery` settled against the journal (`recorded`). Issue 3’s router must compose those, not mint a second id scheme.
- Cycle-safe wire pattern already used by the belt: `session.RunEngine` + `RegisterRunEngine` in `internal/run/enginewire.go` `init()`.
- Standing cadence is `standing.Interval` (5 minutes) plus OS `codeaf tick`.
- Slash table `commands.go` `checkCommands`: `/folders` cannot be an alias of `/folder`.
- Live TUI isolation follows `internal/e2e/tmux_test.go`. `make demo-home` launches with `HOME=` and strips `CODEAF_HOME` — do not copy that for acceptance.
- Person-facing copy must not say `auditor`.

---

## 2. Defaults that replace impractical sketches

The PRD is the product contract. These defaults replace earlier sketches that would fight this codebase, and they incorporate supervising review plus the owner's collaboration clarification.

### 2.1 Logical folders are a home panel, not an eighth place

**Default:** logical folders are a **home panel** named `folders` plus slash `/folders` (not `/folder`). Empty panel keeps its heading and one dim line naming what arrives there — never “no folders yet”. Selecting a folder opens a **detail card/sheet** (wide: card beside the list; ≤80 cols: sequential view with `esc` back). Icons go through `tokens.GlyphFolder` / `palette.glyph`, never literals.

Filesystem `/folder` is untouched. Logical membership never changes cwd, repo, or `/attach`.

### 2.2 Do not steal home’s existing keys

Home already spends `a` (archive), `t` (new chat in the filesystem project), `o` (open filesystem folder). Bare letters always type on home.

**Default:** folder actions ride the existing **verb strip** (`→` on a folders-panel row, then letters while the strip is drawn). Planned mnemonics:

| Key on the strip | Action |
|---|---|
| `n` | new chat in this logical folder (start page; membership committed on first message) |
| `f` | add the open/selected chat to this folder |
| `m` | move this placement (add destination + remove this edge; other placements kept) |
| `w` | why here (reason, source, actor, correct) |
| `x` | remove this placement |

If a collision appears in implementation, rename the chord, not the product action. Record the final names in the manual and TRY.md.

### 2.3 Collapse seven cognitive “roles” into two model jobs plus the main chat

**Default model-call map:**

| Job | When | Call path | Durable result |
|---|---|---|---|
| Main chat | Person sends a message | Existing session turn | Journal append |
| Title | First exchange | Existing `RoleTitle` | Conversation title |
| Query expand + organize | After a substantive user message, via a durable job | New `RoleOrganize` through `callRoleChecked` | Typed `ActionPlan` validated by `wsapi` |
| Embed | During discovery ingest and hybrid search | New pin `RoleEmbed`. OpenAI-compatible `/embeddings` | Vectors in `discovery.db` with model/version/dimension |
| Speak as a representative | Wave 3, when coordinating | Bounded per-participant invocation, same router | Durable delivery + journal append |
| Launch-or-join | Wave 4 tool | No extra model to “decide launch”; equivalence may use one `RoleOrganize` | Claim + execution binding |
| Folder overview | Opening a folder after a revision change | Software roll-up first; one cheap summary only if needed | Cached projection keyed by revisions |

No model call from `View`, home motion, or folder-tree paint. Tick/host processes jobs.

If embeddings are unavailable, hybrid search degrades to query expansion + lexical retrieval + organizer judgment, **labelled as delayed/degraded**. A keyword-only membership rule is refused.

### 2.4 Virtual Root is computed, not a fake membership row

**Default:** Root is not a `collections` row. `root_state` holds purpose/guidance/revision. Children of Root = collections with no collection-parent + conversations with no membership. A chat started at Root has root scope without an ordinary edge. Root is never a child. Old CLI `list`/`find` stay as they are.

### 2.5 Honest incremental schema

**Default:**

- Issue 1 migrates collections.db **v1 → v2** in the write path, additive, with **only** purpose/lifecycle/revision/timestamps, membership events, and `root_state`. Listing remains read-only and does not migrate. Refuse foreign/future/damaged; never reset.
- Issue 2 adds v3 tables it owns (guidance, jobs, observations, placement_suppressions). Issue 3 adds v4 (participants, deliveries). Issue 4 adds v5 (grants, execution_bindings). Each bump is an explicit transactional migration. Test v1-to-latest.
- Journals remain authoritative. `discovery.db` at `home.Join("v3", "discovery.db")` is derived and rebuildable (issue 2).
- A membership change, its audit event, and any outbox row this slice owns commit in **one collections transaction**. Model calls happen after, then revalidate expected revisions before apply. No AI/network inside a writer transaction.

### 2.6 Ordinary chats coordinate; shared discussion is optional

**Default (owner clarification):** any existing ordinary chat can coordinate other independent chats. There is no manager subclass and no mandatory new group chat.

Three patterns share **one** durable router, actor envelope, and receipts:

1. Direct authorized request/reply to one chat or folder representative.
2. Fan-out: one update delivered separately to several recipients.
3. Invite participants into the current discussion, or optionally create a separate discussion when a distinct history is wanted.

Planner/critic are configurable roles, not product entities. Each contributor gets a real bounded invocation. Reading historical text does not wake its chat and is not an instruction.

Conflict escalation: one discussion, both positions, hierarchical parent join with ancestor dedupe, then Root if needed. Two turns may be a per-level starting budget. Missing authority reaches the user. Unrelated work continues.

### 2.7 New chat here does not mint an empty transcript

**Default:** “new chat in Billing” opens the existing start page with a **pending collection id**. First message mints the conversation and then `wsapi` adds membership (journal first, membership second, reconcile if membership write fails). Esc on the start page creates nothing.

### 2.8 Typed service, not a god orchestrator

| Package | Owns | Must not |
|---|---|---|
| `internal/workspace` | SQLite, DAG, migrations, provenance rows | Import session/provider/tui/run; no model calls |
| `internal/wsapi` | Typed application service; `Action` validation; effective-scope resolver | Untyped `map[string]any` actions; SQL from the model; render-path calls |
| `internal/wsdiscover` | `discovery.db`, ingest cursors, hybrid search | Own membership truth |
| `internal/wscollab` | Durable envelopes, routing, receipts for all three patterns | Bypass single-writer journals; a second messaging subsystem |
| `internal/wsexec` | Launch-or-join adapter over **both** task roads | Use `plandb.ParentID` as folder membership |

Wire implementations in `cmd/codeaf`. TUI and session tools are thin adapters. New functions stay at cyclomatic complexity ≤ 15.

Feature switch: profile row `workspace.organize` (default on after v2). Off pauses automatic placements; manual folders, history, and work remain.

### 2.9 Automation policy

- Routine **adds** apply without an approval card; record why.
- Automatic **removals** only for organizer-origin or system-fallback placements. User placements stay until the user moves/removes them.
- Removing a chat removes that placement and future folder-scoped coordination over it. It does not delete history or cancel an authorized run.
- Selected-item coordination is a snapshot of marked object IDs. Whole-folder coordination follows current descendants.
- Compatible instructions compose. Incompatible override needs issuer authority + explicit supersession.
- Reading historical text does not promote it to an instruction.

### 2.10 Do not optimize for a manual folder demo

Issue 1 is a usable folder TUI because later AI has to land *somewhere*. Semantic discovery, collaboration, and safe execution are issues 2–4. All J01–J35 remain required.

---

## 3. Serial process (Spark, this checkout)

We are already in Spark-owned `~/src`. Operate inside the persistent fleet shell `codeaf-workspace-0918`. Do **not** `fleet run` (that rsyncs into `~/work` and strips `.git`). Do not edit `~/work`. Do not touch other checkouts.

For every issue:

1. Implement on `feat/collaborative-workspace-0918`.
2. Update `internal/manual/chat/` in the same change; delete obsolete denials; add probes if a new asker’s sentence would miss.
3. Update `internal/session/prompts/system.md` so it does not promise absent tools.
4. Document invalidated assumptions in the design progress notes. The pull-request changelog uses the actual PR number when opened; do not invent a PR number. Interim changelog checks must stay green without a guessed number.
5. `make build` → `bin/codeaf` only.
6. Focused tests (`make test-focus`) while editing.
7. **Commit the candidate.** Then `make test-touched BASE=610a32ba4cdf04053e61e0e14703304842e4b844` (or the recorded pre-issue source) plus applicable laws/packed manual. If a fix changes the tree, commit again and rerun. Receipt HEAD must equal tested source.
8. **Live tmux TUI journey** against an isolated `CODEAF_HOME` (recipe below). Fake-model tests are necessary but not completion. A missing key or skipped live test is a blocker.
9. Record journey IDs, SHA, fleet session, Cursor session, tmux names, commands, exit codes, log paths. Update TRY.md for the wave.
10. Review, push the branch. Do not merge. Do not close the GitHub issue merely because code is written. Only then start issue N+1.

**Isolated live-home recipe (all four issues):**

- Do not set `HOME`. Do not use `~/.codeaf`.
- `CODEAF_HOME=$(mktemp -d /tmp/codeaf-cw-iN-XXXXXX)` mode 0700. `CODEAF_PROFILE_DIR` private or cleared.
- Credentials: resolve with the product (`config.APIKeyAt` / `home.InheritedDir()` / e2e `liveKey`), write into the throwaway profile the way `internal/e2e` `newWorld`/`liveKey` does. Never print the key. Never place keys in command arguments. Pin `model.talk` to `deepseek/deepseek-v4-flash` and `icons` to `plain` for capture needles.
- Workspace: a throwaway **git repository**. Prefer matching `internal/e2e` env (`TERM=xterm-256color`, `CODEAF_HOME`, no `HOME` rewrite).
- Run-unique tmux session names (`cw-iN-<pid>-<sha>`). Reuse the existing terminal-resize/start readiness harness; mere `new-session -x/-y` is insufficient when tmux server policy overrides sizing. Drive one pass at 80 columns. Do **not** follow `make demo-home`.
- After the chat: inspect `$CODEAF_HOME/v3/collections.db`, journals, and (from issue 2) `discovery.db`. Save pane captures under the control dir `receipts/issue-N/`.
- Never clean other sessions. Never inspect arbitrary process argv.

---

## 4. The four issues (vertical)

| # | Title | User-visible outcome | Depends on |
|---|---|---|---|
| 1 | Folders you can see: Root, shared membership, and new chats that stay themselves | Home `folders` panel, `/folders`, new chat in a logical folder, add/move/remove, why-here, dual placement, restart | none |
| 2 | Semantic discovery, automatic filing, and scoped folder instructions | New chat finds old work without the old title; auto-places with reason; user correction sticks; folder instructions load in descendants | 1 |
| 3 | Inspectable collaboration: ordinary chats coordinate, with optional shared discussion | Existing chat coordinates selected chats via direct, fan-out, and optional joint discussion; inspect and intervene; offline then resume delivers once | 1–2 |
| 4 | Safe execution: launch-or-join, authority, and unattended recovery | Collaboration can launch or join work without duplicate implementation; pause vs stop; tick while TUI closed; both task roads | 1–3 |

Full bodies: `issue-1.md` … `issue-4.md`. Journeys: J01–J35 in `USER-JOURNEYS.md`.

---

## 5. Mapping P1–P12, A1–A22, and journeys

| ID | Issue | Journeys | Notes |
|---|---|---|---|
| P1 | 1 | J01, J03, J20 | New identity on first message; never merge |
| P2 | 1 | J03, J04 | Shared ref, binary membership, “also in …” |
| P3 | 1 | J02, J08 | DAG + cycle txn; bidirectional listing |
| P4 | 1 surface, 2 guidance, 3 representative | J12–J14, J21, J24 | Folder is not a filesystem dir |
| P5 | 3 | J19–J22, J25 | Coordination is a chat role; several allowed |
| P6 | 2 | J09–J10, J15–J18 | Semantic retrieval; rejected plans remain |
| P7 | 2 | J11–J12, J16–J17 | Auto-file without approval; user can correct |
| P8 | 1 + 2 + 3 | J03–J04, J14, J22 | Shared object ≠ merged folders |
| P9 | 4 | J27–J33 | Launch-or-join; no silent duplicate implementation |
| P10 | 1–4 | J05, J10–J15, J22–J24, J26, J29–J31, J34 | Provenance; history ≠ instruction |
| P11 | 1 + 2 | J07, J18, J25, J34 | Overview + search; no pressure to reuse chats |
| P12 | 1 | J06–J07, J35 | Selection/composer stable across background org |
| A1 | 1 | J03 | Same chat in two folders; counts deduped |
| A2 | 1 | J02 | Concurrent opposite-edge inserts cannot cycle |
| A3 | 2 finds, 4 no duplicate launch | J09 + J28 | Split across intelligence and execution |
| A4 | 2 | J09, J18 | Different wording, same dependency |
| A5 | 2 | J12, J18 | Similar wording, unrelated; no bogus membership |
| A6 | 2 | J10 | Rejected proposal + rationale retrievable |
| A7 | 2 | J10, J13, J29 | Short correction reaches context before commit |
| A8 | 2 | J11 | User removal persists; stale evidence does not refile |
| A9 | 1 UI, 2 guidance, 4 pause writes | J06, J13, J29 | |
| A10 | 4 | J28 | Two coordinators, one work owner |
| A11 | 3 + 4 | J26, J29, J30 | Cannot claim to be the user on either road |
| A12 | 3 | J23 | Offline recipient, then once-only durable delivery |
| A13 | 2 | J15 | Crash after journal, before index |
| A14 | 4 | J31 | Crash after launch, before binding |
| A15 | 2 + 4 | J17, J33 | Budget/provider failure visible; no fake success |
| A16 | 3 scope + 4 removal | J20, J29 | Selected snapshot vs dynamic folder |
| A17 | 3 | J24 | One conflict discussion; Root cannot exceed user |
| A18 | 1 CLI/schema; 2 search without memory | J08, J15 | |
| A19 | 1 | J06–J07 | Hundreds fixture + 80-col + no render model calls |
| A20 | 4 | J32–J33 | Unattended launch under profile permissions |
| A21 | 4 | J30 | Session-task road and bash-belt/run road |
| A22 | 2 | J08, J15 | Rewind/deletion/unavailable vs absent |

Hardening that the PRD called “phase 6” is **in-issue**: migrations, restart, rollout switch, manuals, and live journeys are exit conditions of 1–4, not a fifth issue.

---

## 6. Unresolved implementation choices (honest)

These do not reopen P1–P12. They are decided at the named issue:

1. **Exact embedding model slug** that this Spark’s provider actually serves. Issue 2 inspects without printing keys, registers `RoleEmbed`, and records the chosen id. Not hardcoded in TUI copy.
2. **Vector lookup:** brute-force cosine in SQLite for the first release. Measure passages/vectors/memory/latency on the promised corpus before calling it scalable. ANN only if measurements demand it.
3. **Opening the conversation FTS store when memory is off** vs indexing journals only into `discovery.db`. Prefer: split `v3SearchSeam` from memory **and** feed `discovery.db` from journals. Issue 2 owns the split.
4. **Explicit Root placements** beyond the virtual-root default: not in this release.
5. **Multi-host synchronized organization graph:** not in this release; preserve remote identifiers only.
6. **Live discovery quality thresholds:** establish on the issue-2 fixture corpus before calling A4/A5 statistically done.
7. **Final `/folders` aliases** if users type `/collection`. Default: no `/folder` alias. Optional later alias `/collections` pointing at `/folders`.
8. **Whether `codeaf collections` grows purpose/why flags in issue 1** or stays CRUD while TUI/tools carry provenance. Default: CLI gains `--reason` / `--json` fields without changing existing output for old invocations.

---

## 7. Key risk

Issue 2 is the load-bearing slice: it must split search from optional memory, ship a real embedding adapter through existing provider accounting, and turn organizer output into **validated durable actions**. If issue 2 ships a lexical-only filer, issues 3–4 cannot satisfy P6/A4/A5. Issue 3’s risk is proving three communication patterns with real per-participant invocations, not a group-chat demo. Issue 4’s risk is dual execution roads plus host idle-retirement.

Owner-facing demos: after each wave, update [`TRY.md`](TRY.md) with actual keys and a synthetic fixture on Spark, never overwriting the owner's global binary or `~/.codeaf`.
