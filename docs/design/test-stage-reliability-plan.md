# codeaf Test-stage reliability plan

**Updated:** 2026-10-23, after independent feedback and source verification.  
**Status:** Plan A implemented and focused source checks passed; isolated native build is the final release gate. Plans B and C are follow-on designs, not claims about this build.  
**Audited revisions:** engine `e2e5a6f08`; desktop `fefc2982`.  
**Review input:** `CodeAF-Test-Stage-Reliability-Plan-2026-10-23-feedback.md` on the Desktop. The review is evidence to check, not implementation authority.

## 1. Decision and scope

Land the incident fix first, using the existing stage runner, shell-job registry, job park, receipts and store. The engine must wait for essential work without buying model turns to poll it. A checkpoint must never become a terminal result just because the model goes idle.

The original proposal combined three different changes. They are now separate:

| Plan | Scope | Release meaning |
| --- | --- | --- |
| **A — incident containment** | Finality; stage-owned work and parking; fixture isolation; qualifying pass reuse; safe receipt reads; durable round extensions/history; cleanup fence; sleep reporting; conservative delivery; minimum running-check presentation | Prevent the demonstrated premature close and repeat-verification failures. This iteration implements and tests this plan. |
| **B — explicit verification protocol** | Requirements and coverage; managed runs; deterministic planning/adapters; evidence identity and dispositions; cumulative recovery allowances | Replace heuristic observer-based acceptance. A requires no new planner. |
| **C — durable distributed ownership** | Transactional delivery receipts, usage IDs, acknowledged control signals, lease fencing, process identity/restart reconciliation, spawn uncertainty | Required before claiming unattended exactly-once accounting or crash-safe execution ownership. A is only a bridge here. |

Do not represent A as solving B/C. No live job is automatically retried or migrated by installing this build. The running app stays untouched; the output is a separate bundle.

## 2. Corrected incident account

The engine source confirms three root causes: `parseStageResult` drops finality; `chatExecutor.Run` accepts a saved report at idle; `Agent.StillGoing` ignores shell jobs. The engine's own missing-report reminder can prompt an unfinished report while checks run.

The saved #1839 records also contain a successful `go test -count=1 ./internal/session/` run on checkout fingerprint `4f45bb4c2e39`, unchanged across later conversations. Evidence was scoped to a conversation, so later rounds repeated that work. A passing session suite does not prove that the separate repository-wide suite finished.

The initial ceiling test failed at $1.07 against $1. The earlier plan's attribution to the person's production ledger was wrong: package TestMain resets HOME and clears CODEAF_HOME, and `home.Dir` quarantines inherited state under test. Shared temporary package state is consistent with the failure; the exact earlier writer is not independently established. Ambient isolation remains hardening, not the demonstrated cause.

There are six Test conversation directories, not four. Saved messages show a user-granted extension and an app restart. Source proves that the extended limit was a local variable recalculated from recipe Max after restart. The exact historical mechanism that removed some round lines is unproven; report history replacement is a verified risk and will be repaired without rewriting old journals.

Nested negative-test fixtures and baseline-worktree probes contaminated the observer ledger. A probe whose `cd` failed never ran its check; replayed journal rows can repeat that observation. Later unfinished suites were killed by stage cleanup. An unavailable exit must not be described as a failed assertion or as proof of an infrastructure timeout.

The inspected local Test model meter books spend directly into the served store. Remote delta-report replay/discard is a separate reliability risk, not a demonstrated cause of duplicate charges in this incident. Manager/supervision turns are a material part of the incident's spend and belong in later instrumentation.

## 3. Terms and decisions

| Term | Current representation | Plan A meaning |
| --- | --- | --- |
| Stage | recipe Stage + stream Phase | Same pipeline step, with its existing completion rule. |
| Round / attempt | Phase.Round; usually one conversation | Remains a conversation round. A does **not** silently redefine Max as repair cycles. |
| Checkpoint | previously indistinguishable from a report | Runtime-only `StageResult.Checkpoint`; never folded as a finished round. |
| Owned operation | job registry owed flag was incomplete | Stage-only immutable ownership of essential finite shell work, independent of steer/quiet delivery. |
| Qualifying pass | observed terminal exit + snapshots | Exact requirement/context and unchanged checkout; reused with original provenance, not a claim of wider coverage. |
| Verification cycle / control epoch | new concepts | Reserved for B/C; not silently mapped to today's Round. |

1. **Manual retains its existing meaning.** In-round bounded corrections remain possible. Manual prevents automatic decisions/recovery at the existing gates; it is not Pause. Switching Auto to Manual does not kill an already authorized check. Stop, Close, takeover, spending cap and repository disable retain their cancellation contract.
2. **Max still counts rounds in A.** Persist the effective round limit when a user grants additional work. Resume/restart does not refill or revoke that grant. Rerun/send-back grants the documented extra loop starting at its next round; a fresh run starts fresh, while preserving job spending according to the existing run contract. No general cumulative-cycle policy ships by accident.
3. **No success inferred from silence.** Checkpoints do not enter idle, reminder-exhausted or wall finalization as success. Inference cannot certify verification stages. A wall produces an honest incomplete/timeout result; larger typed non-content recovery accounting is B.
4. **Finite essential work blocks settling.** Promoted foreground calls remain owned even if steering pre-empts promotion. Explicit background jobs block only when recognized as checks; watches/dev servers do not. Ownership is not the mutable notification debt. Stage parking uses existing event-driven news, not provider polling. A person's message releases the park without revoking ownership.
5. **Completion is fenced.** Register ownership before process admission. A stage settles only with no owned work, model turn, task or queued completion wake. Once settled, self-wakes cannot start an unobserved turn. Stop holds the same-job control through executor cleanup; replacement cannot overlap it.
6. **Reuse is selective and conservative.** A failed sibling requirement does not throw away a qualifying pass. Reuse cannot erase a newer unresolved result, widen coverage, accept stale context, or call an unfinished broad suite passed. Preserve original source receipt identity/timestamps. Exact flag/environment identity remains conservative until B introduces explicit strength adapters.
7. **Observer ownership is explicit.** Strip exported observer functions below a recognized parent check. Bind new observations to the canonical owner worktree at tool admission; a nested fixture in a different worktree is diagnostic relative to that owner. An explicitly prepared check worktree gets its own declared boundary. Do not classify legacy records by temporary path names. Legacy unknown provenance stays unresolved/historical, not manufactured green.
8. **Incomplete persistence is not a failed test.** Write receipt components atomically. Ignore temporary files. A published invocation lacking a terminal component remains incomplete; corrupt committed data remains instrumentation failure. Supervisor liveness, not file absence alone, decides whether it is still running.
9. **Display facts, not model guesses.** Keep step state running and add a typed verification substate with check purpose/start time. Share it through the existing Event/Activity/Decision framework and engine state presentation. No new prose parsing or fourth timeline primitive.
10. **Sleep is deliberate retention, not network-partition authority.** Wire actual app sleep/wake into existing runner presence. Wake reports presence and refreshes the silence deadline; existing command walls remain in force. The existing unlimited asleep-retention policy is unchanged. This presence callback is not a wake admission barrier: strong revalidation, bounded retention and safe replacement behind an unreachable worker belong to C.
11. **Refused delivery does not advance the baseline.** Retain an immutable payload/key until acknowledged; do not cache retryable 5xx. Deterministic refusal retains diagnostics and parks through an explicit delivery problem, rather than replaying work forever. This is per-lease protection, not a transactional outbox across arbitrary crashes.

## 4. Plan A implementation and acceptance

| Work | Main source seams | Required checks |
| --- | --- | --- |
| Finality and decoder parity | session/tools_stage.go; factory/recipe.go; run/chatstage.go | checkpoint vs terminal, omitted final, coercible list/number parity, reject multiple lifecycle reports before dispatch; wall/reminder exhaustion cannot accept checkpoint |
| Owned work and stage park | session/jobs.go, promote.go, steer.go, task_job_park.go, unattendedrun.go; cmd/factory_stage.go | foreground promotion; early steer; explicit background check; quiet success wake; person can interrupt park; dev server/watch does not hold; no provider calls just to poll |
| Verification guard by rule | run/chatstage.go | renamed `until: green/proven` step is guarded; inferred success refused |
| Fixture isolation | verification/receipts.go, invocations.go | passing parent with intentionally failing children; genuinely failing parent; plain bash wrapper; compiled-test wrapper; explicit alternate-owner worktree; legacy unknown ownership failclosed |
| Cross-round reuse | factory_verification_checkpoint.go | independently qualifying pass survives failed sibling; same checkout only; current unresolved requirement not erased; source provenance retained |
| Receipt/read robustness | verification/receipts.go, snapshot.go; factory_stage_receipts.go | partial writes affect only that record; temporary names ignored; corrupt published status explicit; snapshot cancellation/process cleanup bounded; compaction call-ID dedup |
| Rounds/history and teardown | run/loop.go, report.go; Phase and report codecs | restart preserves granted limit; send-back grant exact; merge round history by attempt/chat identity; Stop→Resume cannot overlap cleanup |
| Sleep and delivery bridge | appwork.go, app_factory.go; lease; worker/client/serve | production sleep/wake path exercised; asleep hold valid; 5xx/lost reply/413/422 preserve baseline; byte-stable retry key; old strict server tolerates additive headers |
| Minimum desktop projection | Job callback → Phase → worker runtime header → api.Step → Rust/TS → shared step adapter | local/remote status equivalent; older fields default; waiting check remains blue running; current check start drives elapsed time; no argv/secrets or absolute paths in status |
| Documentation and build | manual, change entry, conversation contract where relevant | exact focused test counts and known limits; isolated latest engine+desktop bundle; signature and embedded version verified; no launch/interrupt |

Regression fixtures must drive production parse → tool dispatch → stage door → conversation follow → executor → fold, with controllable real child processes and fake providers/clocks. Short, deterministic release signals replace minute-long sleeps. Test the two #1839 shapes: a parent suite that passes with negative fixture children and a parent suite that actually fails.

## 5. Plans B and C: retained work and explicit design constraints

### B — requirements, execution and recovery

- Use default-branch recipe authority as the source for mandated checks. Chat-stage requirements are a separate, explicit manifest; no empty manifest success. Prefer deterministic repository adapters before a paid planner. Non-Go repositories retain declared recipe commands; remote gates remain remote, not silently replaced by local guesses.
- Choose a tree-hash/candidate-snapshot policy for commands that require committed inputs. Test is a pre-publication filter. Initial draft publication cannot require the CI that publication itself triggers; ready-for-review requires the current-head readiness policy.
- Record parent/child lineage, scope, toolchain and relevant declared inputs. File snapshots do not prove immunity from external edit-and-revert or from ignored inputs. Stronger flag/environment equivalence needs policy-specific adapters; timeout/count/concurrency are not universally cosmetic.
- Keep failed test identities; absence/skip is not a passing rerun. Flag changes to tests, scripts and gate definitions for Review. This is not automatic detection of semantically weakened assertions. Trusted repository gates may explicitly report flaky/base-failure dispositions; do not call those underlying tests passed. `make -k` retains failure status; it is not itself a masking example.
- A top-level failure remains unresolved until an equal-or-wider qualified pass or an explicit visible disposition. Nested fixture results remain diagnostic under their parent. Guarded scripts cannot silently rewrite their own acceptance policy in the candidate tree.
- Track managed writers, including task landings and restore/reset paths. Defer them during a protected run or invalidate affected evidence. No claim that snapshot hashes alone prevent races.
- Distinguish assertion failure, invocation failure, resource-policy kill, timeout, worker cancellation, interruption, instrumentation failure and uncertain execution. Linux CPU/process policy must not masquerade as a test failure. Declare per-check deadlines and heavy-run admission policy.
- Replace multiplicative reminder/inference/correction/recovery layers with one bounded policy. Inventory Follow PR counters, readiness limit extensions, manager questions, session wall, stage wall and manual grants. Specify cumulative time, model-call/spend caps, manual extension rules, and report-only versus code-repair allowances before enabling it.
- Plan A preserves existing Max semantics. B must explicitly migrate UI round labels, recovery actions and old jobs if cycles replace rounds. A human Resume must state whether it grants time or uses the remaining allowance.
- Store typed outcomes and accepted evidence counts. Do not keep the largest historically measured command as the accepted count. Spend instrumentation includes supervision, reporting, execution recovery and content repair.

### C — distributed safety and durability

- Immutable outbox must survive mirror cleanup and process restart. Exact serialized payload, headers, sequence/digest and baseline are persisted before send. Commit application and acknowledgement atomically with workflow/accounting changes.
- A deterministic 413/422 needs an explicit terminal rejection acknowledgement; usage has independent stable IDs and an acknowledgement door that survives lease retirement without restoring execution authority.
- Acknowledge Pause/Manual/Stop control signals rather than deleting them before successful delivery. An independent watchdog must not be blocked behind a hung HTTP report.
- Execution admission requires a live authority grant. Fencing alone does not stop an old process. Before replacement, reconcile physical ownership or stop it; unknown spawn/cleanup stays unknown. Do not claim exactly-once OS spawn.
- Record process identity and startup reconciliation. macOS process-group cleanup does not catch every `setsid` escape; Linux subreaper behavior differs. Include factory work in quit disclosure and reconcile engine teardown with desktop grace.
- Prefer versioned sidecar/receipt records for new durable facts; unknown whole-item schemas hide the job from old Stop controls, while unversioned fields can be dropped on old saves. Test downgrade round-trips and negotiate capabilities before enhanced writes.

## 6. Review disposition and validation

Accepted after source checks: dropped finality/idle liveness; existing park reuse with steer fixes; qualifying pass reuse; temporary test-home correction; persisted extensions; incomplete receipt problem; actual missing sleep caller; refused-report baseline loss; stage-name guard hole; additive UI path needed.

Accepted with qualification: six historical Test chats are real, but the exact round-history erasure is not proven; sleep can expire/requeue on wake, not a measured guarantee that every 60-second Mac sleep kills work; current retry schedule has no jitter; original two-hour stage and session clocks are separate and reset per round; local Test spend is direct-metered.

Rejected as blanket fixes: simply extending the old all-or-nothing inheritance helper; classifying all external cwd as disposable; claiming `export -nf` covers compiled wrappers by itself; idempotency middleware as a crash-safe transaction; treating all Go flags as cosmetic; allowing old workers to keep writing indefinitely after lease loss; requiring initial-draft CI before publishing that draft.

Plan A validation is bounded isolated checks, exact test names/counts and a native build. Linux process/resource faults and native macOS sleep/escaped-descendant behavior need their respective lanes; a mocked presence test is not proof of physical sleep. Plans B/C release gates additionally require the fault matrix, old/new wire and downgrade tests, and disposable jobs across representative repositories. No production #1839 retry is part of validation.

## 7. Source anchors

Engine paths at the audited revision:

- `internal/session/tools_stage.go`, `internal/factory/recipe.go`, `internal/factory/run/chatstage.go`, `stagereport.go`: finality, guards, reminders and completion.
- `internal/session/task_job_park.go`, `task_child_run.go`, `jobs.go`, `promote.go`, `steer.go`, `unattendedrun.go`: park, owed/ownership, wakes and jobs.
- `cmd/codeaf/factory_verification_checkpoint.go`, `factory_stage_receipts.go`; `internal/factory/verification/*`: cross-round reuse, receipts, observer inheritance and snapshots.
- `internal/session/hermetic_test.go:67–79`, `internal/home/home.go:43–46`: test-home isolation, correcting the ledger diagnosis.
- `internal/factory/run/loop.go`, `report.go`: effective limit, stop/release and round-history fold.
- `cmd/codeaf/app_factory.go`, `appwork.go`; `internal/factory/lease/runners.go`; worker/client/serve: production sleep and delivery.
- `internal/factory/recipesource.go`, `recipefile.go`; `scripts/touched-verdict.py:209–238`, `Makefile:223–239`: trusted recipes, gate dispositions and committed-input constraints.
- Desktop `crates/core`, `apps/desktop/src/state/factoryJobModel.ts`, `docs/design/conversation-elements.md`: additive read model and shared chat presentation.

The original detailed proposal and feedback remain available for later B/C design. Local incident journals stay private; they are not copied into a repository or PR.


## 8. Implementation verification record

- Engine lifecycle fixtures exercise the real stage maker, checkpoint, promoted/background process, actual exit and final fold. Native call IDs prevent old killed receipts from appearing live. A new turn or refused assessment withdraws stale completion.
- Evidence fixtures cover passing parents with negative children, genuine parent failures, compiled wrappers, declared worktree boundaries, exact-input pass reuse with failed/unknown siblings, partial writes and compaction replay.
- Recovery checks cover Stop-to-replacement cleanup fencing, persistent extensions, word-based grants at the service boundary, cold-worker retry, pending send-back grants, merged history and bounded mechanical retry.
- Delivery/sleep checks cover lost replies after application, immutable retry payloads, newer unconfirmed snapshot retention, explicit 413/422 blocking, retryable response-cache behavior, native presence wiring and old strict server compatibility. Selected concurrency-sensitive paths pass under the race detector.
- Desktop: 59 focused projection/history tests pass; typecheck passes; the focused Rust bridge test passes. The macOS runner cannot set its RLIMIT_DATA ceiling; tests also ran with an explicit bounded Node heap.
- Source naming and whitespace checks pass. Broad Linux/native physical-sleep and arbitrary-crash acceptance have not been run and are not implied by these fixtures. No live job was used or resumed.

Specific retained limits: checkpoints are runtime-only; explicit coverage requirements remain B; delivery still has a service apply/cache crash gap and no automatic old-lease reconciliation UI; same-job cleanup fencing is in-process, not physical orphan reconciliation; presence reporting does not stop OS-resumed goroutines before the wake callback. Earlier same-round attempts stay visible in the UI, but their ambiguous legacy round-only chat link is withheld.
