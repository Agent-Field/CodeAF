# Architecture and role-routing audit

**Date:** 2026-09-19
**Auditor:** `t-architecture-roles` / `cw0918-architecture-roles` (Cursor cursor-grok-4.6-high, Spark)
**Inspected SHA:** `c25d772088abfada66f056effd2ad91422ab03e9` (`feat/cw0918-architecture-roles`, Wave 4 pin)
**Production edits:** none. This document and PlanDB tasks only.
**GitHub:** none.

This is an independent source audit of modularity and high/low model-role use on the collaborative-workspace feature. It does **not** write `releases/folders-entry/ready.json`. It overlays `COMPLETE-UX-AUDIT.md` (live on `feat/cw0918-rx-contracts` SHA `2d781e64310ae7f2dedc06aa373ff1ce1687a7a4`) and `CONTRACTS.md` with role-routing facts those documents did not freeze.

## Method

Read `AGENTS.md` / `CLAUDE.md`, `LATEST-WORKER-POLICY.md`, `ENGINEERING.md`, `CONTRACTS.md` Roles/embed, and `docs/changes/unreleased/` (1211 RoleEmbed, 1213 organize-bind). Mapped packages with `go list`. Verified the five source concerns by reading call sites, not by assuming comments. Ran small boundary/complexity/role tests on Spark. Did **not** run `internal/tui3` or full `internal/session` suites. Did **not** print credentials; model ids below are allowlisted source constants (`Default*`, `KeyTier*`, `roles.*` / `tiers.*` keys, curated embed slug).

**Harness vs product (do not confuse):** this audit worker is Cursor Grok. New implementation workers use codeaf from `origin/santos/dev` with `--model z-ai/glm-5.3-flash --one-model`. That flag **withholds** the roles ladder. A GLM preview cannot observe the bugs below. Product defaults (flag off) still resolve pin → tier → session floor.

## 1. Layer map

Composition root is `cmd/codeaf` (adapters). Domain packages do not import each other except `wsapi → workspace` and `session → workspace` (job/Ref types). No import cycle among the named packages (`go list` 2026-09-19).

```
internal/workspace     collections.db v1→v6, membership, jobs, grants
        ▲
internal/wsapi         typed service; Discoverer/Collaborator/Executor injected
        ▲ (interfaces only; cmd binds)
cmd/codeaf adapters    folders / discovery / collab / exec / organize_bind
        ▲
internal/session       agent, tools, RoleOrganize, RoleCollabConsult
internal/tui3          DTOs + Folders/Collab/Exec interfaces; no workspace import
```

| Package | Owns | Imports of the set | Must not |
|---|---|---|---|
| `workspace` | SQLite graph, schema, jobs, provenance | none of session/tui3/wsapi/provider | agent, screen, memory |
| `wsapi` | snapshots, Instruct/Search/Apply, collab/exec doors | `workspace` only | session, tui3, provider, run, wsdiscover, wscollab, wsexec (`boundary_test.go`) |
| `wsdiscover` | `discovery.db` passages/FTS/vectors/cursors | `home` | session; embedder injected |
| `embed` | RoleEmbed pin + POST /embeddings adapter | catalog, config, provider, roles | workspace/wsapi |
| organization | **not a package** — `session.Organize` + `cmd/codeaf/organize_bind.go` + `workspace` jobs | — | — |
| `wscollab` | envelope, outbox, Contribute | none of session/workspace (`complexity_test.go`) | session; host binds Seam |
| `wsexec` | grants, launch-or-join, both roads | `env` | session/run/plandb; host maps |
| `session` | agent, `callRoleChecked`, organize/consult | `workspace`, `roles` | wscollab, wsexec, wsapi, embed, tui3 |
| `tui3` | paint | `session`, `roles` | workspace, wsapi, wscollab, wsexec, wsdiscover |
| `cmd/codeaf` | bind all of the above | all of them | — |

**Adapters (cycle-safe seams):**

| File | Maps |
|---|---|
| `cmd/codeaf/folders_adapter.go` | `*wsapi.Service` → `tui3.Folders` + `session.Folders` |
| `cmd/codeaf/discovery_adapter.go` | `*wsdiscover.Store` + `embed.Embedder` → `wsapi.Discoverer` |
| `cmd/codeaf/collab_adapter.go` | `wscollab.Router` → session/tui3/wsapi; `RegisterCollabRouter` |
| `cmd/codeaf/exec_adapter.go` | `wsexec.Adapter` → session/tui3/wsapi; process-wide `v3ExecRuntime` |
| `cmd/codeaf/organize_bind.go` | tick job → ingest → SearchEvidence → `Agent.Organize` → Validate/Apply |

**Verdict on modularity:** the split is real and test-enforced. It is simple enough: storage, service, index, three host-bound capabilities, one composition root. Scalability gaps are inside doors (full-world ingest per job, brute-force vectors), not missing packages.

## 2. Current AI call table (workspace feature)

Resolution ladder (`internal/roles/roles.go` `Resolve` / `Ladder`, lines 611–701): **pin `roles.<role>` → `tiers.<tier>` → session floor**. Effort suffix on a tier value (`model:low|medium|high`) is split in `ResolveCall` and applied as `provider.WithEffortRung` with `effort.RoleErrand` (`auxiliary.go` 259–278). Empty effort sends nothing. Session floor is **not** split.

Errand door: `Agent.callRole` → `callRoleChecked` → `completeWithNamedModel` → `provider.WithCallTag` via `callPurpose(role)` (`clientdoor.go`). Spend journals the **answered** model (`auxiliary.go` 293–301). Patience is per **tier** (`roles.PatienceFor`), not per role. One fall-through rung (`roleFallThroughs = 1`, `auxiliary.go:65`).

| Site | Semantic role | Registered tier | Effort | Model priority | Fallback / error | Cost tag | Production path |
|---|---|---|---|---|---|---|---|
| Main chat turn | person's question | n/a (not an errand) | conversation / ctrl+t | `Config.Model` | provider chain unless `--one-model` | `turn` | live |
| `Agent.Organize` | file chat from evidence | `RoleOrganize` **low** (`organize.go:16`) | errand; tier suffix if any | pin → **low** (or high pin when `High`) → floor | unusable JSON hops one rung; then error | `organize` | tick / standing (`v3Organizer`) |
| Organize `High=true` | restructuring / instruction conflict | still registered **low**; temporary pin to high-tier model (`organize.go:111–138`) | same | intended high | same | `organize` | **dead** — bind never sets `High` |
| `consultCollabParticipant` | invited participant (planner/critic are **labels**) | `RoleCollabConsult` **low** (`collab_consult.go:21`) | errand | pin → low → session | one hop | `collab-consult` | `coordinate` invite (`tools_coordinate.go:348`) |
| `embed.Client.Embed` | vectors | **pin only**, not registered (`roles.go:90–96`) | n/a | pin → catalog embeddings → `openai/text-embedding-3-small` | degraded lexical; no dummy vectors | `embed` on wire; **Account is nil** in production | discovery ingest + SearchEmbed |
| `RoleAuditor` | finished-work judge | high | — | — | — | — | **not** on organize/consult (tests pin this) |

`--one-model` (`cmd/codeaf/chatv3.go:1390–1428`, `auxiliary.go:112–114`): `RolesSource` is nil, fallbacks emptied, every text errand floor is `a.model`. Media pins (embed/vision/image/speech/video) stay capability-qualified. Tests: `cmd/codeaf/chatv3_onemodel_test.go`.

### Product defaults (allowlisted keys only; no credentials)

Conversation default: `config.DefaultModel` = `~deepseek/deepseek-v4-flash-latest` (`config.go:33`).

Untouched profile tier rows (`settings.go:1058–1074`), which is also the `all`/balanced crew:

| Key | Default |
|---|---|
| `models.tiers.reflex` | `google/gemini-2.5-flash` |
| `models.tiers.low` | `deepseek/deepseek-v4-flash-0731` |
| `models.tiers.worker` | `z-ai/glm-5.3-flash` |
| `models.tiers.high` | `anthropic/claude-fable-5.1` |
| `models.tiers.mastermind` | `anthropic/claude-opus-5` |
| `roles.embed` (curated last rung) | `openai/text-embedding-3-small` |

Open-family balanced (`crew.go:251–257`) puts high on `moonshotai/kimi-k3` and mastermind on `z-ai/glm-5.3`. **Worker GLM 5.3 Flash is the task seat, not the organizer seat.** Routine organize, without `--one-model`, is the **low** row (DeepSeek flash dated), not GLM flash.

### Harness GLM 5.3 Flash `--one-model`

`workers/glm-context-create/run.sh` (and policy): `codeaf chat --model z-ai/glm-5.3-flash --one-model`. Under that flag organize, consult, title, and the main turn are the **same** slug. Role-routing bugs are invisible there. Live product without the flag still has them.

## 3. Source concerns — verified

### 3.1 `pinOrganizeHigh` overwrites shared `Agent.config.RolesSource` — **confirmed**

```111:138:internal/session/organize.go
// pinOrganizeHigh makes a restructuring call resolve on the high-tier model.
// RoleOrganize stays registered low; without a pin the cheap title model would
// win the ladder even when the caller passed a high floor.
func (a *Agent) pinOrganizeHigh(high bool) func() {
	// ...
	a.mu.Lock()
	prev := a.config.RolesSource
	a.config.RolesSource = func(key string) (string, bool) {
		if key == roles.PinKey(roles.RoleOrganize) {
			return floor, true
		}
		if prev != nil {
			return prev(key)
		}
		return "", false
	}
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		a.config.RolesSource = prev
		a.mu.Unlock()
	}
}
```

Why it exists: `callRoleChecked` treats `sessionDefault` as **rung 3**. If `tiers.low` is set, that cheap model wins even when the caller passed the high-tier id as the floor (`roles.go:211–214`, `organize.go:54–57`). The pin is a workaround for a ladder that cannot say "this call is high."

Shared mutable state: the swap is `Agent.config.RolesSource`, held only during the pointer write, **not** during the RPC. Concurrent errands on the same `Agent` (title, consult, another organize) can resolve `roles.organize` to the high model, or restore a stale `prev`. Production today constructs a **throwaway** agent per job (`organize_bind.go:84–98`), so the live conversation is not polluted **yet**. Binding `agent.Organize` later would be a race. The correct seam is a call-scoped `roles.Source`, not mutating config.

### 3.2 Production never sets `OrganizeRequest.High` — **confirmed**

```65:70:cmd/codeaf/organize_bind.go
	plan, err := o.rolePlan(ctx, session.OrganizeRequest{
		ChatID: job.ChatID, SourceRev: job.SourceRev,
		Evidence:  formatEvidence(query, hits),
		Hierarchy: formatHierarchy(ctx, svc, job.ChatID),
		Degraded:  evidenceDegraded(hits),
	})
```

`High` is omitted (false). `v3Organizer()` passes `nil` organizeFn (`chatv3_standing.go:196`), so every tick/standing job hits this struct. Tests set `High: true` (`organize_test.go:71–82`) and prove the high-tier model answers — **only in tests**. CONTRACTS.md line 496 and `issue-2.md` require high for restructuring / instruction conflicts. `wsapi.EffectiveGuidance` has `Conflict`; bind never reads it. `create-folder` / multi-folder moves still ride low.

**Product effect:** hierarchy rebuilds and conflicting instructions are filed by `tiers.low` (DeepSeek flash dated on a default profile). The high/careful model is never asked.

### 3.3 collab-consult always low, including planner/critic — **confirmed**

```21:23:internal/session/collab_consult.go
	roles.Register(roles.RoleCollabConsult, roles.TierLow,
		"speaks as one invited participant from bounded source excerpts")
```

```49:49:internal/session/collab_consult.go
	response, _, err := a.callRoleChecked(ctx, roles.RoleCollabConsult, floor,
```

The invite `role` string is prompt text (`collab_consult.go:43`), not a tier. `tools_coordinate.go:338–348` records the roster then always calls this. CONTRACTS.md says `RoleCollabConsult` (not `RolePlanner`) so J19 does not reuse the adaptive-run brain — that part is right. ENGINEERING.md still wants a bounded invocation **with role**. A participant asked to plan or critique still sits on **low** (same bill as titles). `TestRoleCollabConsultIsRegisteredAndIsNotPlanner` **locks the low assignment in**.

**Product effect:** substantive Wave 3 consults are cheap-model prose. Distinct invocations exist; distinct **capability** does not.

### 3.4 `callRoleChecked` fallback can raise cost — **confirmed (by design, wrong for organize)**

`roleFallThroughs = 1` (`auxiliary.go:65`). After a pin/tier miss, unusable JSON (`organizeAccept` / `collabConsultAccept`), or transport failure, the next rung is the **session model** (conversation default, often dearer than low). Comments (`auxiliary.go:42–61`) say this is so a title still arrives. For organize:

- Unusable JSON is billed, then a second call on the conversation model is billed, then parse may still fail.
- Under `--one-model` both rungs are the same slug: **two charges** for one errand.
- A high pin that fails hops to session, which may be a third vendor.

Journal tags the answered model (correct attribution). The extra hop is the cost bug. Organize should record no-action on unusable output rather than hop up the ladder.

### 3.5 `--one-model` masks role routing — **confirmed, working as specified**

Not a product defect. Measurement posture (`chatv3_onemodel_test.go:10–14`). Implementation workers and previews that pass `--one-model` **cannot** validate 3.1–3.4. `t-role-validate` must run **without** the flag (and still assert the flag settles every text slot).

## 4. Architecture checklist

### Cycles

None among workspace, wsapi, wsdiscover, embed, wscollab, wsexec, roles. Session does not import wscollab/wsexec/wsapi (`collab_complexity_test.go`). TUI does not import workspace/wsapi (`folders.go:12`, `collab.go`, `exec.go`). `go list` agrees.

### Render I/O

`tui3.View` (`view.go`) draws memos. Folders snapshots are on the home **beat**, not View (`CONTRACTS.md`, `framedisk_law_test.go`). Preview launches no AI. No model/disk on paint found in the folders path.

### Shared global mutable state

| State | Where | Risk |
|---|---|---|
| `chatCollab` | `session.RegisterCollabRouter` | process-wide inbound router; nil = absence |
| `v3CollabRouter` | `collab_adapter.go` | host singleton |
| `v3ExecAdapter` + `v3ExecRuntime.agents` | `exec_adapter.go:33–37` | process-wide agent map |
| `roles.registry` | init + mutex | last Register wins; organize/consult register from session init |
| `Agent.config.RolesSource` during `pinOrganizeHigh` | §3.1 | **bug** |

Absence law is otherwise held: nil discoverer/collab/exec leaves verbs off, no dummy success.

### Transactions, migrations, idempotency

- Schema `user_version` 1→6 (`workspace/schema.go:10–17`). Listing does not migrate. Foreign/future refused.
- Membership + provenance in one writer txn. `ApplyActionPlan` revalidates then batch-applies (`wsapi/apply.go:15–18`). Organize model call is **outside** the collections txn; `FinishJob` rechecks the fence (`organize_jobs.go:55–57`).
- Jobs coalesce on type+key (`jobs.go:14–16`). Ingest cursor replay is a no-op (`wsdiscover/ingest.go:20–23`). Embed runs **outside** the discovery writer txn (`ingest.go:21`).
- Organizer apply uses expected revisions; stale plan → conflict, not a silent rewrite.

### Queue bounds

| Bound | Value | File |
|---|---|---|
| Organize jobs per standing pass | 4 | `organize_jobs.go:22` |
| Organize lease attempts then fail | 8 | `organize_jobs.go:25` |
| SearchEvidence default limit | 20 | `wsapi/search.go:7` |
| Consult excerpts | `store.ConversationSearchDefault` = 8 | `conversation_read.go:16` |
| Errand fall-throughs | 1 | `auxiliary.go:65` |
| Collab live queue | session mailbox (unbounded channel not found; durable outbox is the record) | `wscollab/router.go` |
| **Ingest per organize job** | **all world journals** | `organize_bind.go:101–114` **unbounded** |
| **SearchSimilar** | load every matching vector, cosine in process | `wsdiscover/search.go:162–191` **O(passages)** |

### Cancellation

`ProcessOrganizeJobs` checks `ctx.Err()` per lease. Errands use `roles.PatienceFor` (low = 2m). `callRoleChecked` treats `context.Canceled` as not a failure row (`auxiliary.go:331–332`); deadline is a failure and is journaled. Lease expiry returns the row to pending; it is not "worker dead" (`jobs.go:77–79`).

### Test seams

Injected `Organizer`, `Embedder`, `Discoverer`, `Collaborator`, `Executor`, `session.Collab` / `Folders` / `Exec`. Fakes in `*_fake_test.go`. Production must not bind success-with-empty-vectors (`embed.go:36–37`, discovery adapter comments).

### Complexity ≤ 15 (new functions)

Package AST tests **pass** on this SHA: workspace, wsapi, wsdiscover, wscollab, wsexec, embed; session collab files (`collab.go`, `collab_consult.go`, `tools_coordinate.go`, `exec.go`). `cmd/codeaf/organize_bind.go` has **no** complexity gate. Pre-existing `callRoleChecked` is far over 15; do not grow it. New organize/consult work must stay in small helpers.

## 5. Other confirmed findings

### F1 — every organize job re-ingests the whole world

`doorOrganizer.Organize` always calls `ingestWorldJournals` (`organize_bind.go:54`, `101–114`): every `session.ReadWorld` journal, every ingestible line. Cursors skip unchanged passages, but **every tick still reads every transcript** and may embed new ones in one `Embed` batch (`ingest.go:80–102`). Idle/backfill cost grows with conversation count. `t-rx-runtime` already owns F09 checkpoint >8; this is the sibling ingest-scope bug.

### F2 — RoleEmbed Account is nil at the door

```23:32:cmd/codeaf/chatv3_embed.go
func v3Embedder(...) embed.Embedder {
	// ...
	return embed.New(client, model, nil).WithSecret(settings.APIKey)
}
```

Wire is tagged `roles.RoleEmbed` (`embed.go:121`). Session ledger `Account` is not connected. Embed spend can exist in the provider log and vanish from DailyRail. `t-rx-runtime` already names this; keep it on that lane, do not duplicate.

### F3 — throwaway `session.New` per organize job

`roleOrganize` (`organize_bind.go:84–98`) loads settings, builds posture, `session.New`, `Organize`, `Close`. Isolates §3.1 today; pays full agent construction per job. Bind a long-lived organizer client instead of a full agent, **after** the RolesSource pin is gone.

### F4 — brute-force vector search

`SearchSimilar` selects all non-deleted vectors for that embedding generation and scores cosine in Go (`search.go:173–191`). Correctness is fine; J18 10k is already a failed scale gate (`t-fe-j18-scale`). Do not start a second ANN lane here; note it as a known bound.

## 6. Tests run on Spark (this audit)

No one-suite lock. No full `tui3` / `session` / `check`.

| Command | Result |
|---|---|
| `go test -count=1 -timeout 3m -run 'TestNewFunctionsStayUnderTheCeiling\|TestWorkspaceFunctionsStayAtMostFifteenDecisions\|TestServiceDoesNotImport\|…' ./internal/{workspace,wsapi,wsdiscover,wscollab,wsexec,embed,roles}` | pass (10 tests / 7 packages) |
| discover/workspace/wscollab remaining boundary+complexity | pass (6 / 5 packages) |
| `go test -count=1 -timeout 8m -run 'TestOneModelSettles\|TestWithoutOneModel\|TestOneModelDoesNotRead\|TestV3Organizer\|TestDoor\|TestOrganize' ./cmd/codeaf` | pass (20) |
| `go test -count=1 -timeout 10m -run 'TestRoleOrganizeGoesThroughCallRoleChecked\|TestOrganizeUserMessageCarriesFolderHierarchy\|TestRoleCollabConsultIsRegisteredAndIsNotPlanner\|TestCollabFilesDoNotImportTheRouterPackage\|TestNewCollabFunctionsStayUnderTheCeiling' ./internal/session` | pass (5) |

These tests **document** current (wrong) production wiring: high organize works only when the test sets `High`; consult is asserted low; `--one-model` withholds the ladder.

## 7. Overlay on the live UX audit and rx contracts

Add to `COMPLETE-UX-AUDIT.md` (rx-contracts SHA `2d781e64`) as a dated overlay; do not weaken J/F rows.

**Critical (person-visible cost/quality, not a missing tab):**

1. **Organize always uses the low-tier model** in production. Restructuring and instruction conflicts never raise. Hierarchy quality and "why did it dump everything in X" journeys (J11 and reactive organize) are running the title model. Cost instrumentation on `t-rx-runtime` must **label the model**, not only enqueue/start/commit timers — otherwise a later High fix looks like a spend regression.
2. **Invite planner/critic is the same cheap call as a title.** F15 paint can be perfect while the contribution is low-tier. `t-ux-collab-chrome` owns paint only; quality is `t-role-collab-tier`.
3. **`--one-model` previews do not prove organize/consult routing.** Folders-entry live TUI that launched with the harness flag cannot claim role-routing acceptance.
4. **Whole-world ingest per job** will show up as idle embed cost after "Organize existing chats". Distinguish scheduler delay from **ingest+embed** vs RoleOrganize latency.
5. **Embed Account nil** — DailyRail may omit vectors while the provider bill includes them.

`t-rx-contracts` already froze columns/reactive **interfaces**. It did not freeze: when `High` is true; that consult labels do not select a tier; that `--one-model` is excluded from role proof; that ingest is per-chat not per-world. Those belong in the rx runtime/proof lanes and in `t-role-*` below — not a second contracts rewrite of Folders keys.

`t-fe-ready` stays blocked on `t-ux-validate` **and** on `t-role-validate` (this audit). Do not ready a SHA whose organize/consult still ignore high/low.

## 8. Remediation tasks (disjoint, codeaf GLM, no GitHub)

Implementation workers: Spark, codeaf from `origin/santos/dev` pinned SHA/checksum, `z-ai/glm-5.3-flash`. **Do not pass `--one-model` when proving role routing.** Feature branch `feat/collaborative-workspace-0918`. No issues/PR.

| ID | Kind | Owns | Must not edit | Depends on | Accept |
|---|---|---|---|---|---|
| `t-role-organize-high` | code | `internal/session/organize.go` (remove `pinOrganizeHigh`; call-scoped Source or a high floor that **wins** the ladder); `cmd/codeaf/organize_bind.go` set `High` from guidance `Conflict` and create-folder/restructure kinds; ingest **this job's chat** (plus invalidated ids), not `ReadWorld()` every time; unusable plan → no-action **without** hopping to the session model | `internal/tui3/*`, `collab_consult.go`, `auxiliary.go` (do not grow `callRoleChecked`) | `t-rx-runtime` (same `organize_bind.go`) | Unit: conflict fixture sets High and the completer model is `tiers.high`, not low. Production bind test fails if High stays false on Conflict. Concurrent title+organize on one Agent do not see a leftover pin. Ingest test does not open sibling journals. Complexity ≤15. Manual: RoleOrganize never RoleAuditor. |
| `t-role-collab-tier` | code | `internal/session/collab_consult.go`, invite path in `tools_coordinate.go` only; CONTRACTS.md one paragraph: participant labels `planner`/`critic`/`design` (case-insensitive) raise floor to **high** (not `RolePlanner`); other labels stay low; optional `roles.collab-consult` pin still wins | `collabview.go` (`t-ux-collab-chrome`), organize_bind, `auxiliary.go` | none (parallel with chrome) | Test: invite role `planner` rides `tiers.high`; `summarize` rides low; two speakers still two invocations (J19). Prompt still says the label. Not RolePlanner. |
| `t-role-validate` | test | proof only (unit + isolated `CODEAF_HOME` organize/consult **without** `--one-model`; keep a separate `--one-model` cell that all text slots match session model) | production feature code | `t-role-organize-high`, `t-role-collab-tier` | Named SHA. Model ids in journal/call log: routine organize = low; conflict organize = high; planner consult = high; embed tag `embed`. Harness GLM `--one-model` cell does **not** count as this proof. |

`t-rx-runtime` (running) keeps F09/F10/wakeup/DailyRail/**RoleEmbed Account**. Do not steal those files while it is running. Note F1/F2 on that task; `t-role-organize-high` waits then owns High + ingest scope + pin removal.

Edges: `t-rx-runtime` → `t-role-organize-high` → `t-role-validate` → `t-fe-ready`. `t-role-collab-tier` → `t-role-validate`. `t-ux-validate` remains the F01–F24 live gate (unchanged).

## 9. Bottom line

The package graph is modular, cycle-free, and UI-thin. Role routing for the workspace **is not**. CONTRACTS promised high organize on conflicts and a real per-participant consult; production always calls low, papers over the ladder with a shared `RolesSource` mutation that the door never enables, and implementation workers using `--one-model` cannot see any of it. Fix High + ingest after `t-rx-runtime`, raise consult floors for planner/critic, and prove it on a SHA **without** `--one-model` before `t-fe-ready`.
