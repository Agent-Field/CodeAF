# Issue 4: Safe execution: launch-or-join, authority, and unattended recovery

**Branch:** `feat/collaborative-workspace-0918`
**Design:** [`PRD-TDD.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/PRD-TDD.md) · [`ENGINEERING.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/ENGINEERING.md)
**Depends on:** issues 1–3 committed.
**Journeys:** [`USER-JOURNEYS.md`](https://github.com/Agent-Field/CodeAF/blob/feat/collaborative-workspace-0918/docs/design/collaborative-workspace/USER-JOURNEYS.md) **J27–J35**.

Required: P9, A3 (no duplicate implementation), A9 (conflicting writes wait), A10, A11 (both runtime roads), A14, A16 (remove placement does not cancel work), A20, A21.
Defaults: execution **adapter** over existing `StartTask` roads; durable launch intent + request key; pause coordination ≠ stop work; no new daemon; spend via existing daily rail + job-category reservation.

This is the last slice. Final gate: `make pr-ready BASE=7cda67c9a066b9c805e4327054a814e0c52c0ef9` on Spark plus live cases. Coordinators in issue 3 had read/discuss/organize; this issue adds **explicitly delegated execution**.

---

## User-visible outcome

An ordinary chat **and** a shared discussion can **do the work**, not just talk about it, without two coordinators silently implementing the same change (J27, J28).

- Launch-or-join: if equivalent work already exists, follow it; otherwise start **one** owned run/task.
- Two discussions of “Issue 42” are allowed; two unnoticed implementations are not.
- Pause coordination stops new decisions/launches. Stopping existing work is a **separate** explicit action (J33).
- Removing a chat from a folder does not delete history or cancel an authorized run (J29 / A16).
- Closing the TUI does not stop authorized work (J32). Unattended work respects profile permissions and the daily spend rail (A20).
- Agent cannot become the user to widen a grant or assignment (A11) on **either** `CODEAF_TASK_BELT` unset or `=bash` (J30).

J35 is the integrated journey on the final candidate: new chat → discovery/shared filing → folder guidance → planner/critic → five-feature coordination → one shared implementation → parent conflict → close/reopen → inspect and undo a bad placement.

---

## Engineering scope and module ownership

| Module | This issue |
|---|---|
| `internal/wsexec` | **New.** Adapter: launch-or-join, inspect, steer-with-authority, pause/stop, observe results. Maps to session `StartTask` **and** run/plandb when `CODEAF_TASK_BELT=bash`. Immutable **run-instance** id. **Never** put folder membership in `plandb.ParentID`. |
| `internal/workspace` / `wsapi` | **Schema v4 → v5.** `execution_bindings`, grants/responsibilities. Grant: goal, coordinating chat, selected-id or folder-dynamic scope, action classes, budget, issuer, revocation revision. Delegation cannot expand itself. Launch intent + request key **before** runtime admission. Test v1-to-v5. |
| `internal/session` | Extend assignment law **deliberately** for delegated revisions: authentic grant + original person request; still not model-supplied person origin. Apply on both roads. |
| `internal/run` + `plandb` | Idempotent admission by request key if missing. Register through existing `session.RunEngine` / `RegisterRunEngine`. |
| Tick / standing / enginehost | Unattended jobs reuse `codeaf tick` + in-window pass every `standing.Interval` (5m). Posture from the **home profile**, never the repo, never `--yolo`. |
| Spend | Same daily rail. Job-category reservation so parallel jobs cannot all spend the last dollar. |
| TUI | Discuss launch state on the discussion/folder preview (software-derived run state). Pause coordination vs stop work as two verbs. |
| Manual | Launch-or-join, pause vs stop, unattended, both task roads, what closing the terminal does. |
| Owner try | TRY.md: launch-or-join and pause vs stop. |

### Dual roads (A21 / J30)

- Unset `CODEAF_TASK_BELT`: session task tree.
- `CODEAF_TASK_BELT=bash`: run engine + PlanDB.

Live journeys must exercise **both** on Spark.

### Recovery (A14 / J31)

Crash after runtime accepted launch, before binding row: recovery **finds** the request-key execution and binds; it does not start a second external action. Lease expiry ≠ worker dead. At-least-once retries; no exactly-once promise for the outside world.

---

## Exact TUI journey (real model, tmux)

Two isolated mktemp homes or two env variants, same SHA. Workspace is a real git repo with a tiny file to change.

### Road A — default belt (CODEAF_TASK_BELT unset) — J27–J29, J33

1. From an ordinary chat and from a discussion, ask for a tiny README comment (J27). **Pass:** one task/run; inspectable from both.
2. Second coordination chat asks to do the same. **Pass (J28/A3/A10):** joins/follows; critique-only still allowed.
3. Pause coordination. **Pass (J33):** no new launch. Existing work still running unless explicitly stopped.
4. Explicit stop. **Pass:** work stops; history remains.
5. Remove the implementing chat from Billing. **Pass (J29/A16):** run not deleted.

### Road B — bash belt (J30 / A21)

Repeat launch-or-join with `CODEAF_TASK_BELT=bash` in a second `CODEAF_HOME`. **Pass:** work is a run/plandb instance with a persisted run-instance id.

### Unattended (J32 / A20)

Launch from a discussion, **quit TUI**, `bin/codeaf tick` under the same `CODEAF_HOME`. Reopen. **Pass:** work inspectable; spend on the existing rail. If the host cannot run unattended, the UI says so.

### Authority (J26 remainder / A11)

Representative tries to raise acceptance criteria as if it were the person, on **each** road. **Pass:** refused.

### Crash binding (J31 / A14)

Automated is primary: kill after runtime start, before binding insert; recover; one work unit.

### Budget (J33 / A15 remainder)

Tiny remaining daily rail. **Pass:** pending/deferred visible; no fabricated completed launch.

### Integrated (J35)

One synthetic workspace covering the complete experience. No hidden DB edits, copied personal state, or mock-only wiring.

**Fail:** two README implementations; ParentID used as folder; only one road tested; skip because “no key”; `HOME` overwritten.

---

## Required automated tests

- Fake runtime: launch-or-join, duplicate key, A14 crash.
- Grant cannot self-expand; A11 both roads (table-driven `CODEAF_TASK_BELT`).
- Pause vs stop; placement remove vs cancel.
- Spend reservation / exhaustion (A15).
- TUI verbs exist and manuals quote them.

**Final Spark acceptance (after commit):**

```bash
make pr-ready BASE=7cda67c9a066b9c805e4327054a814e0c52c0ef9
```

Record SHA, commands, exit codes, log paths. Do not add known-red lines.

---

## Acceptance slice

P9, A3 (no duplicate launch), A9 (write pause), A10, A11, A14, A16 (work lifetime), A20, A21, A15 remainder, J27–J35.

After this issue the matrix A1–A22 and journeys J01–J35 should be covered across 1–4. Personal model training, Hedge/FTRL, and multi-host graph sync remain out of scope.
