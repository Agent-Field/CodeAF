# Issue 2: Semantic discovery, automatic filing, and scoped folder instructions

**Branch:** `feat/collaborative-workspace-0918`
**Design:** [`PRD-TDD.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/PRD-TDD.md) · [`ENGINEERING.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/ENGINEERING.md)
**Depends on:** issue 1 committed on this branch (folders TUI, `wsapi`, collections v2).
**Journeys:** [`USER-JOURNEYS.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/USER-JOURNEYS.md) **J09–J18** (plus affected J01–J08).

Required: P4 (purpose + scoped guidance), P6, P7, P8, P10, A3 (find existing work), A4, A5, A6, A7, A8, A9 (guidance checkpoint), A13, A15 (discovery side), A18 (search with memory off), A22.
Defaults: `RoleOrganize` + **shipped `RoleEmbed` adapter**; brute-force vectors behind an interface; query-expansion fallback labelled degraded; `workspace.organize` profile switch; no keyword-only filer; no always-on model per folder.

---

## User-visible outcome

A person starts a **new** chat about emailed receipt links. It stays a new chat. codeaf **finds** an older access-control discussion without being given its title, **cites original passages**, and may **file** the new chat in Security as well as Billing — quietly, with a why-here reason. The person can remove that automatic placement and it **does not come back** from the same evidence (J09, J11).

They can **instruct this folder**. A descendant chat loads those instructions as standing guidance (not as retrieved maybe-relevant text). Guidance affects the **actual main turn and tool mutations**, not only the organizer (J13). Abandoned plans and the words that rejected them stay searchable (J10).

If learned memory is off, this still works (J15). If the embedder is down, the UI says discovery is delayed — it does not claim the workspace was checked, and it does not invent membership (J17).

Scale is measured in **passages/vectors/memory/latency**, not chat count alone (J18).

---

## Engineering scope and module ownership

| Module | This issue |
|---|---|
| `internal/workspace` | **Schema v2 → v3** on the write path. Tables this slice owns: `guidance`, `jobs`, `observations`, `placement_suppressions`, `proposed_actions`. No speculative Wave 3/4 tables. Job states: pending → leased → completed \| deferred \| failed \| cancelled. Lease + fencing token. Test v1-to-v3. |
| `internal/wsdiscover` | **New.** `home.Join("v3", "discovery.db")`. Ingest journals with persistent cursors (session id + source generation + ordinal + content hash — **not** a byte offset as identity). Passages + lexical FTS + vectors (model/version/dimension). Rebuildable. Rewind/delete invalidates derived rows (A22). Abandoned proposals remain unless the source was explicitly deleted. |
| `internal/wsapi` | `InstructFolder`, `EffectiveGuidance`, `SearchEvidence`, `ApplyActionPlan`, `SuppressPlacement`. Organizer output is a typed plan. Validator: IDs exist, same authority, DAG, freshness, suppressions. Model never writes SQL. |
| `internal/roles` + `internal/session/auxiliary.go` | Register `RoleOrganize` (tier: high when restructuring/conflicts, low for routine file). Register `RoleEmbed` as a **pin**. All calls through `callRoleChecked` / provider / spend. No private HTTP client. Do not route organize through `RoleAuditor`. |
| `internal/provider` / `internal/lane` | **Actual embeddings adapter** on the existing lane (OpenAI-compatible `/embeddings` if the router serves it). Inspect availability without printing keys. Configurable model ID. Expanded-query lexical fallback is degraded, not a replacement. |
| `cmd/codeaf` tick + engine host | After a substantive user message is **already** in the journal, enqueue `observe_and_organize` (coalesce by source revision). Process on the existing standing pass (`standing.Interval` = 5m, plus `codeaf tick`). Organize **outside** the DB txn, revalidate, apply. |
| `internal/session` | Split conversation history from optional memory: set `Config.ConversationHistory` even when `Memory` is nil. `search_conversations` and the search place work when memory is off (A18). Hybrid search **adds** embedding/expansion candidates beside existing BM25. Load `EffectiveGuidance` for the **main turn** and check changed/conflicting scope before affected mutations on available paths. Update the tool sentence “Search is lexical, not semantic” in the same change. |
| `internal/tui3` | Folder detail: instructions section; indexing progress (software, not a fake 100%). Why-here for organizer origin. Quiet filing. Preview still launches **no** AI. |
| Manual + system prompt | Delete denials of automatic organization and inherited instructions. State limits: no keyword-only file; suppressions; memory-off behavior; “delayed” vs “checked”. |
| Owner try | TRY.md: discovery / why / correct / instruct. |

### Where the model is called

```
person message
  → SOFTWARE persists journal
  → SOFTWARE loads EffectiveGuidance for current parents + Root (no ranking)
  → MAIN TALK MODEL answers, may call search_conversations (hybrid)
  → SOFTWARE checkpoints guidance before affected mutations
  → SOFTWARE enqueues job {type: organize, source_rev, chat_id}
tick/host
  → lease job
  → RoleEmbed of new passages (or degraded expansion if embedder down)
  → lexical + vector candidates (dedupe by source id)
  → RoleOrganize(evidence, hierarchy, suppressions, guidance consequences)
  → typed ActionPlan → wsapi.Validate + Apply
```

Similarity scores never become membership. `no-action` is a first-class result.

**Correction path:** user removes organizer placement → suppression keyed by collection + object + evidence hash. Identical evidence cannot re-add. New evidence may reconsider with a new reason (A8 / J11).

**Guidance checkpoint (A7/A9 / J13):** membership/guidance change updates navigation immediately; effective instructions are recomputed **before the next affected tool action or work commitment** on the main turn. A conflict pauses affected mutation only.

---

## Exact TUI journey (real model, tmux)

Isolated `CODEAF_HOME=$(mktemp -d …)` and private `CODEAF_PROFILE_DIR`. Same credential recipe as issue 1. Run-unique tmux. **Two chats**, real model. Cover J09–J18.

1. Create folders Billing and Security. Instruct Security with a standing line about authenticated receipt links (J13). **Pass:** instruction visible; versioned; source is the person.
2. In Security, new chat. First message records that customers must authenticate; reject mailing raw URLs; keep the rejection (J10). **Pass:** chat stays in Security; guidance present as folder guidance.
3. Quit and reopen. Instruction still there.
4. Billing, new chat. Message about emailed download links **without** the old title (J09). **Pass:** reply cites older Security passage; new chat id ≠ old chat id.
5. Wait for background organize (`codeaf tick` if needed). **Pass (J11):** why-here on a new Security placement says organizer + evidence. No approval card.
6. Remove the automatic Security placement. Restart. Tick with no new messages. **Pass (J11):** it does not return.
7. Third chat: espresso-machine receipts. **Pass (J12/A5):** no Security membership.
8. Search/ask about mailing raw receipt URLs. **Pass (J10/A6):** rejected plan and rationale reachable.
9. Short correction: `No, the other one`. **Pass (J10/A7):** next organize/main context includes it.
10. Repeat search with learned memory disabled. **Pass (J15/A18).**
11. Provider embeddings/organize forced to fail. **Pass (J17/A15):** pending/failed visible; no fake membership.
12. Recorded corpus / scale: passages, vectors, memory, latency (J18). Automated may carry the 10k-chat measurement; TUI inspects representative results.

80-col: folder instructions readable; no per-row model calls while scrolling.

---

## Required automated tests

- Fake-model organize: add / no-action / suppress / new-folder-after-equivalent-check.
- Hybrid search: lexical + **real embed adapter**; embed version mismatch does not compare vectors. Degraded expansion path labelled.
- Memory-off: history reader still constructed.
- Ingest cursor crash/replay (A13 / J15).
- Deletion/rewind (A22).
- Guidance composition + supersession; snapshot stale-revision refuse; **main-turn** checkpoint before mutation.
- Passage-scale measurement report (memory, latency, throughput).
- TUI: no organize on hover; why-here organizer copy.

`make test-focus` while editing; **commit**; `make test-touched` on Spark. Do not stack full session+tui3 suites.

---

## Acceptance slice

J09–J18, A3 (discovery half), A4–A8, A9 (guidance), A13, A15 (org), A18 (search), A22, P6–P8, P10.

Not done: cross-chat router, coordinate-selected, launch-or-join (J19–J35).
