# Issue 1: Folders you can see: Root, shared membership, and new chats that stay themselves

**Branch:** `feat/collaborative-workspace-0918` (from `santos/dev` `7cda67c9a066b9c805e4327054a814e0c52c0ef9`; the owner explicitly requested branch-off-`santos/dev`; do not switch).
**Design:** [`docs/design/collaborative-workspace/PRD-TDD.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/PRD-TDD.md)
**Plan:** [`serial-plan.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/serial-plan.md) · [`ENGINEERING.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/ENGINEERING.md)
**Journeys:** [`USER-JOURNEYS.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/USER-JOURNEYS.md) **J01–J08**
**Depends on:** nothing. Later issues depend on this TUI and `wsapi`.

Required: P1, P2, P3, P11 (overviews), P12, A1, A2, A18 (CLI/schema), A19. Defaults: virtual Root, home-panel UX, `/folders` as the slash name, pending membership on first message, **incremental v2 schema with only this slice's tables**.

This issue does **not** pre-create empty guidance, grant, delivery, or execution tables. Later issues bump `user_version` with their own migrations.

---

## User-visible outcome

A person can group chats in **logical folders** from the TUI without touching filesystem `/folder`.

- Home grows a `folders` panel (emptiness law: heading + one dim line when there are none).
- `/folders` focuses that panel. It is **not** an alias of `/folder`.
- From a folder: **new chat here** opens the existing start page; the first message creates a **new** conversation identity and files it in that folder. Esc creates nothing (J01).
- The same chat can sit in Billing and Security. One identity, one history, “also in Security”, counts and spend not doubled (J03).
- Add / move / remove a placement; **why here** shows who put it there and why (J04, J05).
- Nested/shared folders: Receipts under Billing and Security; rename visible through both paths; cycles refused (J02).
- Unfiled chats and parentless collections appear under a virtual **Root** that is not the filesystem home.
- Restarting the TUI shows the same graph. `codeaf collections` still works. `/folder` still means a directory (J08).
- Composer text and selected object survive organization changes from another window (J06).
- 80-column sequential browse; large fixture remains readable (J07 subset in live, full hundreds fixture in automated tests).

This is a usable organizing surface, not a schema-only checkpoint. Automatic semantic filing is issue 2. Inter-chat communication is issue 3.

---

## Engineering scope and module ownership

| Module | This issue |
|---|---|
| `internal/workspace` | Schema v1→v2 on the **write** path. Collection purpose/lifecycle/revision/timestamps. Membership events (origin, reason, evidence refs, actor, time) while **active** memberships stay unique. `root_state`. Keep cycle check + insert in one writer txn. Keep the import boundary test. Soft history (events) must not break idempotent add/remove. Move is add-destination + remove-source in one txn. |
| `internal/wsapi` | **New.** Typed service: `CreateFolder`, `RenameFolder`, `AddPlacement`, `RemovePlacement`, `MovePlacement`, `FolderSnapshot`, `RootSnapshot`, `PlacementsOf`, `WhyHere`. `Action` structs with expected revisions, origin, reason, idempotency key. No model calls. No import of `tui3`. Interfaces for a conversation inventory (read session world) injected from `cmd/codeaf`. New functions cyclomatic complexity ≤ 15. |
| `cmd/codeaf` | Open collections through `wsapi`. Wire inventory from existing session world. CLI: additive `--reason`; default output of old verbs unchanged. |
| `internal/tui3` | Home `folders` panel + folder detail card/sheet. Stable selection by object ID + navigation path. Composer text retained. If a selected placement disappears, keep the open object and explain. No disk/model on draw or on a mere cursor move. Glyphs via `tokens`. Register `/folders` in `commands.go`; `checkCommands` forbids aliasing it to `/folder`. Folder verbs ride the existing verb strip (`→` then letters). Do not import `internal/workspace` from draw paths — go through `wsapi`. |
| `internal/session` | Thin `folders` tool on the belt: list / file / unfile / move for the current chat, going through `wsapi`. Absent if collections cannot open (capability absent, not broken). |
| `internal/manual/chat/` | Replace denials that collection UI does not exist. Keep denials of automatic organization, inherited instructions, and inter-chat communication (those are issues 2–3). Quote actual command and key names. |
| `internal/session/prompts/system.md` | Mention `folders` only if the tool is actually on the belt. |
| Tests | `workspace` migration/concurrency/durability; `wsapi` action validation; tui3 panel/card/80-col/selection; session tool; namelaw/iconlaw/manual gates. A new committed test/harness for dual placement + selection stability. |
| Owner try | Update `TRY.md` with actual keys and a synthetic Spark fixture. Do not overwrite the owner's global binary or `~/.codeaf`. |

Do **not**: change `/folder` semantics; treat `plandb.ParentID` as membership; call models from render; merge chats; invent an eighth tab-bar place; set `HOME`; create speculative later-phase tables.

### Schema and recovery (before coding)

- `PRAGMA user_version` 2 after a successful write-path migration. v1 files remain readable without migrating until a write.
- Refuse application ids / versions that are not ours (future > 2, foreign); never `DROP` into empty.
- Membership mutation + membership_event commit together. No network in that txn. Outbox tables for later issues are **not** created here.
- Journal is not in this DB. First-message order: mint transcript → `AddPlacement`. If placement fails, the chat still exists (unfiled / Root) and the UI says filing failed; retry is idempotent.
- Virtual Root edges are **not** stored. Do not fabricate memberships for unfiled chats.

---

## Invariants

- P1 / J01: first message in “new chat here” is a new 16-hex session id from `v3MintSession`; never silent merge. Esc creates nothing.
- P2 / J03: one object, many folders; membership is binary.
- P3 / J02: finite rooted DAG; cycles refused under concurrent opposite-edge inserts.
- P8 (partial): sharing one chat does not merge folder contents.
- P10 (partial): every new placement has origin (`person` \| `system_fallback` \| `organizer`) and reason; historical text is not an instruction (no guidance engine yet).
- P12 / J06: background/other-window membership changes do not jump the cursor or wipe the composer.
- Numeric task refs still require `--session`. Artifact refs still absolute paths, not content identity.
- Roll-ups count unique conversation IDs, not graph paths.

---

## Exact TUI journey (real model, tmux, isolated home)

**Not done until this is run on Spark against `bin/codeaf` at the issue commit.** Fake tests are not completion. Do not use `~/.codeaf`. Do not set `HOME`.

Cover **J01–J08**. Live chat is required for J01 and J03. J02 cycle concurrency, J07 hundreds fixture, and J08 schema fixtures may be deterministic tests against the same production interfaces, with TUI inspection where observable. Receipts name which evidence is live vs injected.

### Setup

```bash
# from /home/santosh/src/codeaf-collaborative-workspace-0918
git rev-parse HEAD
make build
SHA=$(git rev-parse --short HEAD)
CODEAF_HOME=$(mktemp -d /tmp/codeaf-cw-i1-XXXXXX)
CODEAF_PROFILE_DIR=$(mktemp -d /tmp/codeaf-cw-i1-profile-XXXXXX)
WS=$(mktemp -d /tmp/codeaf-cw-i1-ws-XXXXXX)
chmod 0700 "$CODEAF_HOME" "$CODEAF_PROFILE_DIR"
git -C "$WS" init
# Copy credentials the product way (InheritedDir + APIKeyAt / e2e newWorld),
# never print the key, never pass it as a process argument.
# Pin model.talk=deepseek/deepseek-v4-flash and icons=plain.
TMUX_SESSION="cw-i1-$$-$SHA"
# Use the e2e resize/readiness harness, not only new-session -x/-y.
```

Drive one pass at 80 columns.

### Journey (maps to J01–J08)

1. Land on home. **Pass (J01):** `folders` panel visible. If empty, heading plus one dim teaching line, not “no folders”.
2. Create logical folders **Billing**, **Receipts**, **Security**. Nest Receipts under Billing; also place Receipts under Security. **Pass (J02):** both paths show the same folder; Root is **not** a listed collection; `codeaf collections list` (with `CODEAF_HOME`) shows the three names.
3. Attempt a cycle (Receipts containing Billing). **Pass (J02):** refused; graph unchanged. Concurrent opposite-edge insert is a committed fault test.
4. Select Billing, **new chat here** (verb `n` or documented command). Start page opens; no new transcript yet. Esc: still no transcript (J01).
5. New chat here again. Send a real chat: `We email customers download links for receipts. Who should be able to open those links?` Wait for a real model reply.
6. **Pass (J01/J03):** new conversation id; membership is Billing only; filesystem project is still `$WS`; `/folder` still names a path.
7. Also file this chat in Security. **Pass (J03):** detail of either folder shows the same title; “also in …” on the other; one transcript path; opening from either placement shows the same history including the model reply.
8. Type more in the composer, then have a second `codeaf collections add` (or TUI add of an unrelated chat) happen. **Pass (J06):** cursor and composer text unchanged.
9. `w` why here on the Security placement. **Pass (J05):** origin person, a reason string, no confidence score.
10. Move the Billing placement to Receipts (`m`). **Pass (J04):** still in Security; Billing no longer has the direct edge; Receipts does; no cycle.
11. Remove the Security placement. **Pass (J04):** chat and history remain; only that edge is gone.
12. Detach tmux / quit codeaf. Reopen with the same `CODEAF_HOME`. **Pass (J08):** graph and transcript survive.
13. In the same chat, ask the model `list my folders` or `file this in Security`. **Pass:** `folders` tool called; durable membership matches the tool receipt. If already filed, idempotent no-op.
14. 80-column: sequential folder detail, `esc` back, no illegible triple column (J07).

### Persist inspection (after TUI)

```bash
CODEAF_HOME=$CODEAF_HOME bin/codeaf collections show <billing-id>
CODEAF_HOME=$CODEAF_HOME bin/codeaf collections find conversation <chat-id>
sqlite3 $CODEAF_HOME/v3/collections.db 'PRAGMA user_version; PRAGMA application_id;'
# user_version=2, application_id=0x4146434c
# Confirm one conversation id, move-adjusted memberships, membership_events rows.
# Confirm a transcript.jsonl under v3/projects/… for that id.
# Confirm CODEAF_HOME is not ~/.codeaf
# Confirm no guidance/grant/delivery/execution tables exist yet.
```

Save `tmux capture-pane` to `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-1/` with the SHA.

**Fail:** any merge of chats; duplicate transcripts; `/folder` changing because of membership; Root appearing as a CLI collection; model call implied by merely painting home; using the person’s `~/.codeaf`; speculative later-phase tables; skipped live model because “no key” on a machine that has one.

---

## Failure / restart checks

- Kill the TUI after first-message journal append and before membership commit: chat exists, unfiled under Root, retry of file is safe (J01/J08).
- `collections` write while TUI holds the DB: busy wait ≤10s or the existing busy sentence; no corruption.
- Open a v1 `collections.db` fixture: list works without migrating; first write migrates to v2 and keeps ids (J08).
- Foreign/future DB: refuse, do not reset (J08).
- Unavailable referenced object: visible as unavailable, not silently dropped (J08).
- Narrow 80-col: navigator and detail sequential, `esc` back (J07).

---

## Required automated tests

- `internal/workspace`: v1→v2 migration; cycle still transactional; membership_events written; Root not a stored edge; list-without-write does not migrate; v1-to-latest (today: v2).
- `internal/wsapi`: add/move/remove/idempotency/expected-revision conflict; unique chat counts.
- `internal/tui3`: folders panel emptiness line; dual placement “also in”; selection stability; 80-col sequential; no store read in `View`.
- `internal/session`: `folders` tool present only when service is wired; refuses cycle/unknown ids in tool **result** text, not a panic.
- Manual/chat_test probes: “group chats in folders”, “is /folder a logical folder?”, “also in two folders”.
- `internal/tui3/manual_test.go`: `/folders` and any alias.
- Icon/name laws still green.

No full `go test ./internal/tui3` on a shared box while another agent holds the suite lock. Use `make test-focus` then, **after commit**, `make test-touched BASE=610a32ba4cdf04053e61e0e14703304842e4b844` on Spark.

---

## Documentation

- `internal/manual/chat/collections.md` (and a folders page if retrieval needs a `##` heading in asker’s words: “logical folders”, “group chats”).
- Remove “there is no collection slash command”. Keep “automatic organization is not implemented” until issue 2. Keep inter-chat communication denial until issue 3.
- Design progress notes: what was true (CLI-only collections, no UI) vs what is true now. Do not invent a PR number.
- `TRY.md`: create folders → new chat → same chat in two folders → reopen.

---

## Implementation checklist

1. Write schema/recovery note in `workspace` comments; implement v2 migration (no later-phase tables).
2. `wsapi` types + service + tests.
3. Wire in `cmd/codeaf`; CLI compatibility.
4. TUI panel + detail + verb-strip actions `n f m w x`.
5. Session `folders` tool + prompt/manual.
6. `make build`.
7. Commit. Affected tests on Spark. Live tmux journey + DB/journal inspection + receipt.
8. Update TRY.md. Push branch. Do not merge. Do not close this issue merely because code is written.

---

## Acceptance slice of the matrix

Done for this issue: J01–J08, A1, A2, A18 (CLI + v1 upgrade + folders without memory), A19 (narrow + stable selection + no render model), P1–P3, P12.

Explicitly **not** done: auto-file, semantic search, guidance, collaboration, launch-or-join (J09–J35).
