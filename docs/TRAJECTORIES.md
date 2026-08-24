# Every trajectory the system can take

*An audit, w41. The question it answers is the owner's: "conceptually, every
trajectory the system can take — is it exactly how we want?" Every row below was
read off the code, not inferred from a design note; `file:line` anchors were
taken against `chat-v3-task` at `cd2370d7`. This tree moves under several
sessions at once — re-grep an anchor rather than trusting the number if it does
not land.*

*Updated w42, 2026-08-24, by the lane that fixed C1, C2, C3, M1, M2, M4, m3, m14
and M6(b). Verdict rows carry the date where they moved; the finding paragraphs
under **FIXED** headings are left as they were written, because they are the
evidence the fix was made against. Line numbers in this document are from w41 and
have moved.*

## The world this is measured against

Four laws, decided across waves 35–40, are what "as-intended" means here.

1. **Division is the default road for wide work, everywhere.** One worker
   starts; it splits itself under two gates — the evidence gate
   (`internal/splitgate`) and the free-hands gate (`TaskGraph.freeHands`) — and
   the parts are picked up as hands free. The parent stays and folds.
2. **The planner DAG is the explicit exception.** `run_adaptive`, `/task
   adaptive`, the typed `orchestrate …` cue: a person asked for a planned graph,
   or the structure must be settled up front. Width alone is never the reason.
   Narrow work is byte-identical to the pre-swarm world.
3. **Consent.** Nothing runs from a model decision without the person's yes
   where a yes is required. Headless never auto-approves a question. A
   subharness `ask()` with nobody there is never a yes.
4. **One ledger.** Every road's spend folds into the session ledger; every
   worker is cancellable through the existing routes; standing orders reach
   every born worker; every live worker appears in `WorkingNow` — never faked,
   never missed. Prompt and tool descriptions are one source of truth with the
   code's actual routing.

## The answer, in three sentences

**Every door routes correctly.** No belt verb, prompt sentence, judge, surface,
setting or manual page still steers wide work to the planner; the reword landed
cleanly and a test pins both the new sentences and the absence of the old ones.

**Two roads did not actually run, and both do now** (w42, 2026-08-24).
`divide_work` had never been on a production belt (**C1**), and a subharness
falling back to the long way escaped the tool ceiling it was approved under
(**C2**). Both are fixed and pinned by tests through the real doors.

**The rest is a short list of honest gaps** — a set of sentences that promise
more than their code does, and the restart, standing-firing and planner gaps
below. The wide-work door that admitted unarmed (**M1**), the refusal that named
the wrong cause (**C3**) and the ledger leak on the stop path (**M2**) were
closed in the same lane.

---

## A. Typed — a person typed something

| trajectory | door | road taken | armed? | verdict | evidence |
| --- | --- | --- | --- | --- | --- |
| `/task <brief>` bare | `internal/tui3/taskcommand.go:54` → sizing call `:96-101` → settle `internal/tui3/app.go:2371-2401` | **one worker, always** — judge yes and judge no both reach `startTaskDoor(…, "single", …)` | **yes**, by the sizing judge | as-intended | `rememberDivisible` `internal/session/task_person.go:216-218`; `StartTask` puts the *same* trimmed string on the spec as `request` `task_person.go:114`; `armDivision` matches it `internal/session/task_divide.go:198`. Byte-identical by construction: one `brief` variable feeds both `JudgeDecomposable` (`taskcommand.go:99`) and `StartTask` (`:134`). Pinned by `task_divide_test.go:144` |
| …its dim line | `internal/tui3/taskcommand.go:162` | a fact, not a card | — | as-intended | `the work looks wide · one worker starts, and it can split as it goes`. A judge NO says nothing at all (`app.go:2382`) |
| `/task solo <brief>` | `taskcommand.go:73-74`, `:83-84` | one worker, sizing call never made | enumeration only | as-intended | `armDivision`'s own bullet names this case (`task_divide.go:182-194`); the command row promises no more (`internal/tui3/commands.go:200`) |
| `/task adaptive <brief>` | `taskcommand.go:130-132` → `StartPlannerRun` `task_person.go:123` | **planner DAG** | n/a | as-intended — the explicit exception | there is no `/orchestrate`, `/adaptive`, `/plan` or `/swarm` command; the full table is `internal/tui3/commands.go:59-271` |
| preset `sized` (default) | `internal/config/settings.go:1446`, resolver `:2772` | as `/task` bare | judge | as-intended | `settings.go:534` |
| preset `adaptive` | read at `internal/tui3/app.go:2387`, **only after `msg.parallel`** | planner DAG | n/a | as-intended — the person's own opt-in, stated once | a judge NO still starts one worker (`app.go:2382`, checked first); the row's hint calls itself an override (`settings.go:1449-1455`) |
| preset `single` | `taskcommand.go:93-94` | one worker, no sizing call | enumeration only | as-intended | `settings.go:526` |
| legacy preset `ask` | removed with its card (`settings.go:519-522`) | reads as `sized` | judge | as-intended | a read-time fallback, not a migration: `TaskStartAt` returns `DefaultTaskStart` for anything unrecognised (`settings.go:2773-2781`), and `Choices: TaskStartModes` stops the sheet writing it back |
| `/subharness` list + card | `internal/tui3/app.go:4420` → `internal/tui3/subharness.go:861` | a subharness run as a task node | n/a — the node runs `exec.Runner`, never a belt | as-intended | **two deliberate enters**: a list row only opens the card (`subharness.go:1069-1072`); the card's run row calls `runSubharness` (`:1128-1137`). A mouse press moves the cursor and never acts (`:1279-1283`) |
| `/standing <words>` | `internal/tui3/app.go:4393-4409` → `internal/tui3/standmark.go:219` | a marked send — never work | n/a | as-intended | `SubmitStanding` prefixes an instruction to *propose* rather than carry out (`internal/session/standing_mark.go:62-97`); `standing_contract.go:14` — "NOTHING STANDS UNTIL THE ANSWER IS YES, AND NOBODY PRESENT MEANS NO" |
| ctrl+enter (composer) | `internal/tui3/input.go:543-550` → `standmark.go:179` | same marked send | n/a | as-intended | a draft starting with `/` falls through to the ordinary command road (`standmark.go:190-195`) |
| ctrl+enter (home) | `internal/tui3/home.go:2002-2010` → `homeexchange.go:805` | an errand conversation | n/a | as-intended | not a task and not a planner |
| the margin's `+` doors | `internal/tui3/margin.go:76-82`, press `:313-317` → `:340-352` | **types into the draft; starts nothing** | n/a | as-intended | the file's own law at `margin.go:39-42`: "THE `+` ROW TYPES, IT DOES NOT ARM." |
| home's work doors | `home.go:2398-2404` | a new conversation, or an `ask here` errand | n/a | as-intended | neither admits a task |
| `orchestrate …` typed | `internal/session/orchestrate.go:502-504`, routed `loop.go:209` → `:568` | **planner DAG, immediately, no card** | n/a | as-intended | the typed sentence *is* the consent: "IT IS A TABLE LOOKUP AND NEVER A JUDGEMENT" (`orchestrate.go:492-497`), anchored to the head of the message, with three gates in front — a runner, a watcher (`:572-574`), and only what a person typed (`:577-580`) |

## B. Model-decided — the model reached for a verb

| trajectory | door | road taken | armed? | consent | verdict |
| --- | --- | --- | --- | --- | --- |
| `propose_task` without `wide` | `task.go:279` → `:311` → admit `:412` | one node | enumeration only | countdown card (`task.go:570-593`); silence is yes | as-intended |
| `propose_task` **with `wide`** | schema `task.go:119`, spec `:492` | **one node — `proposeTask` has no planner branch at all** | **yes**, signal one (`task_divide.go:195`) | same countdown | as-intended in routing; the description over-promises — **M6** |
| `run_adaptive` (post-reword) | `tools_harness.go:286`, guard `:90` = `canOrchestrate` `:107` | planner DAG | n/a | none at launch; the fuel gate asks later | as-intended |
| `build_harness` tail | `tools_harness.go:192` → `harness_task.go:453` → admit `:463` | a design node; the page is written, then carded | **no — the kind guard refuses it** | save-or-discard card, and **no clock at all** (`harness_build.go:383-400`) | as-intended (M4 fixed w42, 2026-08-24) |
| `propose_subharness` | `tools_subharness.go:129`, guard `:122` | intake card → `startSubharnessRun` `subharness_contract.go:190` | n/a | mandatory card; **no clock that says yes** — the 15-minute window expires to *nothing ran* (`tools_subharness.go:388-392`) | as-intended |
| **route judge card** | `route_judge.go:146` → card `:304` → `launchRouteTask` `:395` → admit `:413` | one node from the judge's goal | **yes**, the judge's own `wide` | the card; a no returns before any admit (`:200-203`); a yes is deliberately not re-asked (`:383-388`) | as-intended (M1 fixed w42, 2026-08-24) |
| `stand` | `tools_standing.go:299`, guard `:295` | a standing item | n/a | card, mandatory, **no clock**: `Deadline` explicitly zeroed (`:963`), two select arms only (`:967-976`) | as-intended |
| `divide_work` itself | `task_divide.go:114`, guard `mayDivide` `:138` | parts on the same nesting road `propose_task` uses | — | none needed — a refusal is an ordinary tool result | as-intended (C1 fixed w42, 2026-08-24) |

`run_adaptive`'s description now says "THIS IS THE EXCEPTION AND NOT THE ROAD FOR
BROAD WORK … A goal that is merely WIDE is not one of them" (`tools_harness.go:71`)
and names `propose_task` with `wide` as the road; it is absent rather than
refusing when the runner is nil (`:90`). `propose_subharness`'s "nothing runs
because you proposed it" is enforced by `ResolveSubharness` (`:414-423`) being
the single writer of the answer channel, with exactly two callers of
`startSubharnessRun`: `/subharness`, and a confirmed card. `stand`'s "a session
nobody is watching cannot set one up at all" is true twice over — `askStanding`
refuses when unwatched (`:940-943`), and unwatched doors never fill
`Config.Standing` (`cmd/aforge/chatv3.go:155-160`, `standing_run.go:646`).

### Where wide work can be admitted, and whether it is armed

`TaskGraph.admit` (`task_run.go:678`) is genuinely the one arming door — it calls
`armDivision` at `:686` for every spec, whoever opened it. Six call sites:

| # | call site | admits | can carry wide work? | armed by | verdict |
| --- | --- | --- | --- | --- | --- |
| 1 | `task.go:412` `proposeTask` | the model's proposal | **yes** | `spec.wide`, else enumeration | as-intended |
| 2 | `task_person.go:112` `StartTask` | a person's `/task <brief>` | **yes** | the sizing judge's yes, else enumeration | as-intended |
| 3 | `task_divide.go:301` `divideWork` | one part of a division | bounded | inherited `request` matches the bank — **by accident** | drifted, inert (**m2**) |
| 4 | `route_judge.go:413` `launchRouteTask` | the judge's carded task | **yes** | `verdict.wide`, else enumeration | as-intended (M1 fixed w42) |
| 5 | `harness_task.go:463` `admitHarnessDesign` | a harness design | no — it is one page | **nothing: `armDivision` refuses any spec whose kind is not ordinary** | as-intended (M4 fixed w42) |
| 6 | `subharness_run.go:108` `admitSubharnessRun` | a saved program's run | no — the steps are fixed | nothing, by the same kind guard | as-intended (m3 fixed w42) |

All three doors that can carry genuinely wide work are armed, and the three that
cannot are refused by kind at the one arming door — `armDivision`
(`task_divide.go`) asks `spec.kind()` before it looks at any of the three
signals, and `restoreNode` asks `record.Kind` for the same reason on the restart
path.

## C. Autonomous — nobody typed; the machine started it

| trajectory | door | what it starts | road | armed? | consent | verdict |
| --- | --- | --- | --- | --- | --- | --- |
| standing `WhenHold` | `internal/standing/standing.go:143`, skipped `tick.go:166-168` | **nothing** | none | n/a | n/a | as-intended — it returns before the daily rail, the spend rail and the whole look/judge/fire walk |
| standing `WhenAt` / `WhenEvery` / `WhenFile` / `WhenIdle` / `WhenProbe` | `internal/standing/tick.go:250-331`, dispatched `:398-409` | `ActionSay` one line, or `ActionTask` **one headless conversational turn** (`standing_run.go:498-521`) | neither road — no `TaskNode` is ever born | **no, and it cannot be** | ratified at proposal time; a firing asks nobody | **drifted — M5** |
| holds reaching workers | `standing_world.go:118`, appended `task_run.go:826`, `:1035-1039` | — | — | — | — | as-intended for holds — one read per frontier pass, appended last to the brief, no per-node branch, and division parts join the conversation's own graph so `g.home` resolves the person's place. But **every kind** rides, not only holds — **M7** |
| resident leaf `request_split` | `internal/exec/tools.go:936`, armed `internal/exec/linear.go:600` | the leaf **ends** and its parts replace it | the resident's own growth road | by config, never per-node | nobody is asked; the money gate defers | as-intended — a deliberate product difference, documented on both sides (`task_divide.go:50-57` vs `linear.go:794-810`) |
| the shared gate | `internal/splitgate/splitgate.go` | — | — | — | — | as-intended — genuinely one implementation, and `cmd/aforge/cooperative.go:148-156` keeps the old names as forwarders over `splitgate.Floor` — except **M8** |
| `aforge chat --once` | `cmd/aforge/chatv3.go:146-172` | one headless turn | division road present but inert (C1) | `spec.wide`/enumeration | **`AskConsent=false` ⇒ the policy's "prompt" refuses** (`consent.go:282-284`); `Standing` nilled | as-intended |
| `aforge do` | `cmd/aforge/main.go:117` → `do.go:142` | one resident errand | resident: plan gate + leaf split | n/a | spend consent false unless `--yes-spend` (`do.go:308-317`); a question ends the run at exit 1 into `blocked_on` (`:704-713`) | as-intended on consent; the road is the resident's |
| `aforge exec` | `main.go:125` → `exec.go:30` | one linear pass | none | no | vacuous — no gate exists | as-intended; see **m5** |
| `aforge run <subharness>` | `main.go:123` → `subharness_run.go:54` | one typed program | none | **exemplary** — `headlessEnv.Ask` returns `Unanswered=true` and stops (`cmd/aforge/subharness_env.go:259-283`): "Nothing here ever returns an approval" (`:255-257`) | as-intended |
| checkpoint recovery | `task_store.go:838` → `:994` `restoreNode` | restored nodes on the frontier | division; never the planner | **re-derived, not persisted** | none required | drifted — **M3** |

Exit codes, cross-checked against `docs/HEADLESS.md:93`, `:306`, `:322-325`:
`do` 0/1/2 (`do.go:73`, `:82`); `exec` 0/2/3/4/5/6 (`exec.go:211-226`);
`run <subharness>` 0/1/2 (`subharness_run.go:35-37`); `tick` 0 clean, 0 on
`ErrHeld`, 1 otherwise (`cmd/aforge/tick.go:42-47`); any plain error 1.

### What a restart keeps

`taskRecord` (`task_store.go:141-257`) carries neither `divide` nor `wide` — the
file says so itself ("The road is not on the record", `:1034-1036`). Restored
nodes bypass `admit` and are inserted directly (`:972`), so `:1041` is the only
arming door on that path.

| arming cause | survives a restart? |
| --- | --- |
| `spec.wide` — the proposer's own judgement | **lost** — not on the record |
| the sizing judge's yes | **lost** — a one-entry in-memory bank (`task_divide.go:208-225`) |
| `splitgate.WorthIt(title+brief+acceptance)` | survives — all three strings are persisted |

Coordination survives: `Parent`/`Depth` are persisted (`:171-172`) and
`childrenOutstanding` is graph-derived (`task_run.go:2852-2862`), so a re-run
parent re-discovers outstanding parts.

## D. In-flight — work is running and something changes

| trajectory | mechanism | behaviour | verdict |
| --- | --- | --- | --- |
| parts inherit standing orders | `task_run.go:826` + `:1035-1039`; `standing_world.go:118-131` | orders arrive through the brief and only the brief; the part's own config carries no `Standing` door, so nothing is said twice | as-intended — `TestThePersonsOwnWordsAndTheStandingOrdersReachEveryPart` PASS (it calls `briefLocked` by hand, so the frontier→part seam is pinned at one remove) |
| parts inherit the person's words | `task_divide.go:282` `a.taskRequest()` | each part opens on the sentence the person typed | as-intended — same test |
| journals | `task_run.go:3435-3444`, `:3301` | one `tasks/<when>_<id>.jsonl` per part; the URI reaches the parent in the landing note (`:1702`, `:1736-1747`) and reaches a `tasks` row through `TranscriptURI` (`task_index.go:629`) with the live graph merged over the file (`:437-484`) | as-intended, and the path is now the family's rather than the owner's (`Place` gap fixed w43, 2026-08-24) — a part's journal is pinned by `TestAPieceOfATasksWorkKeepsToTheSameSessionFolder` |
| cancel-tree | `cancel.go:73-96`; `task_run.go:2126` + `:2235-2243` | stopping the parent cuts every unsettled kid, and each kid repeats it — a real cascade with no recursive walk. A part is individually stoppable from its own roster row (`internal/tui3/stop.go:277-304`) | as-intended **for the person**; a gap for the model — **M9** |
| spend fold | `task_run.go:3182-3204`; `loop.go:2101`, `:2127-2155` | on the ordinary path, exactly once: a part folds into the parent worker, which the conversation later folds whole into the session ledger; ordering is safe because a part folds in its own defer before `markNoted` and the tail loop waits on `reported()`. On a stopped ending the parent's books are already closed, so `Agent.spendLedger` skips the closed hop and folds the part straight into the conversation — the same total by a shorter route | as-intended (M2 fixed w42, 2026-08-24) |
| the parent stays and folds | tail loop `task_run.go:2788-2810`; park `:2187-2201` | waits on the **report** and never on the state (`:2780-2784`); hands its lane back at `:2799`, takes it back at `:2804`; exits only when nothing is owed, nothing is outstanding and no steered line is queued | as-intended — `TestAParentWaitingOnAPieceHandsBackItsLane` PASS; the queued-line clause is M12's fix, `TestSteeringAParkedParentWakesItAndArrivesInItsNextTurn` |
| deadlock at `parallel=1` | `freeHands` `task_divide.go` | cannot happen: with the asker in the only lane `freeHands` is 0 and the division is refused up front — deliberate, argued in `freeHands`'s own comment. The refusal (`divisionNoLane`) now says which of the two lane cases it is, and does not invite the worker back where the cap is exactly one | as-intended (C3 fixed w42, 2026-08-24) |
| every live worker on the surface | `work_tree.go:85-148`, `:161-170` | tasks + parts + adaptive runs + their nodes + background jobs; a root is drawn while anything under it is live and then the whole family is drawn; every row's id is `cancel.go`'s own spelling | as-intended — "NEVER FAKE LIVENESS" (`:76-78`) — with one surface drift, **m1** |
| …except a sub-harness run | `cancel.go:269-301` mints `harness:<n>`; `work_tree.go:86` lists tasks, runs and jobs only | cancellable, but not a row in the tree | minor — **m4** |
| deopt subharness → linear | trigger `subharness_run.go:192`; `internal/exec/deopt.go` `Deopt` → `ExecutorRunner.Run` `internal/exec/runner.go` | six producers feed one gate; a person's ✕ correctly is not one. The person is told in clean words — `needed a closer look — handled it the long way` (`deopt.go`) — **and the long way is only taken where the program's own ceiling already reaches a shell** (`DeoptHeld`); otherwise the run stops incomplete with `DeoptHeldWord` and the generalist is never reached | as-intended (C2 fixed w42, 2026-08-24); **M10** (pre-deopt spend lost) and **m6** (invisible while it runs) still open |
| a run's node fails | `internal/orchestrate/run.go:496` `land`, `:461` `metLocked` | **abandoned** — no re-plan, no rewire, no cascade; downstream nodes sit `Queued` and `synthesize` writes up Done and Failed only (`:524-536`). Its spend still folds (`orchestrate.go:951`; `run.go:500`) | drifted — **M11** |
| a run's fuel gate | `internal/orchestrate/fuel.go:36`, `:133-136`, `:142-145`; `run.go:413-417` | 80% says so once; 100% finishes what is in flight, starts nothing new, and asks topup/finish/stop | as-intended — matches `system.md:178-179` and the description at `tools_harness.go:71` |
| steering into a running task | `task_room.go:87-109`; `tools_tasks.go:430-438` | a person or the model can say a line into any node, parts included; the brief and acceptance stay frozen — the only writable field is `spec.model`, through `RetargetTask` (`:143-182`) | as-intended |
| steering a **parked** parent | `task_room.go`'s `SteerTask`; `enqueueSteeredLine` `agent.go`; park select `task_run.go`'s tail loop | the line RELEASES the wait, the runner re-enters the model with it, and the loop will not park on or close over a queued line (`steeringHeld`). The door answers whether the node was waiting, and both surfaces say so | as-intended (M12 fixed w43, 2026-08-24) |
| the governor's hold | `TaskGraph.machineBusy`, `task_divide.go` | the governor is no longer part of the division's gate: a busy machine holds the *parts* on the frontier (`waiting · machine busy`) and `armPoll` lifts them, exactly as it does for work admitted through `propose_task`. `divisionDone`'s receipt says so | as-intended (C3 fixed w42, 2026-08-24) |

### Pause and fuel, all nine mechanisms

Every one is announced and every one resumes, except where noted.

| mechanism | file:line | what pauses | resumes |
| --- | --- | --- | --- |
| admission governor (load/mem) | `task_pressure.go:132-148`, asked `task_run.go:820` | new starts of slot-taking nodes | automatically, `armPoll` every 5s (`task_run.go:935-953`) |
| lane cap (`task.parallel`) | `task_run.go:467-486`, `:858-859` | new starts past the cap; outranks the machine | automatically on any completion |
| provider pacing | `task_run.go:171-174`, `:1619-1625` | a call inside a running node | automatically |
| park | `task_run.go:2187-2201` | nothing — a lane is handed back | automatically |
| orchestrate fuel gate | `internal/orchestrate/fuel.go:126-155` | new launches; in-flight nodes finish | **only a person** — topup / finish / stop |
| 80% fuel warning | `fuel.go:36`, `:133-136` | nothing | n/a — but it reaches the person as a bare figure (**m7**) |
| session spend rail | `rail.go:40-51` | the next turn, before it records | only a person |
| process-exit pause | `task_store.go:1140-1169` | every running node | automatically, next session |
| consent hold | `consent.go:353-397` | one call, not the turn | the person answers, or an interrupt refuses it |

`internal/session/guardian.go` holds nothing — it is a low-tier model that can
turn a consent prompt into an allow and can never deny (`:8-21`, `:97-166`). The
governor lives in `task_pressure.go`.

## E. Prompt truth — `prompts/system.md` against the belt

The belt is assembled at `internal/session/tools.go:108`. Thirty-seven tool names
can appear on it; every one is either unconditional or gated by an absence-law
predicate, and the belt and the prompt page for division are built from the
**same** predicate (`tools.go:134` and `prompt.go:120` both call `mayDivide`),
which is exactly right.

**No belt verb steers wide work to a planner.** All three descriptions that touch
the question point breadth at `propose_task`: `tools_harness.go:71`
(`run_adaptive`), `task.go:83` (`propose_task` — "do not reach for a planner"),
`tools_harness.go:48` (`build_harness`).

**No source sentence and no manual page steers breadth to a planner.** A
repo-wide case-insensitive grep for breadth words near planner words returns no
non-test hit that reads the old way; `internal/orchestrate/prompt.md` describes
how a run that has already started behaves, which is out of scope. The compiled
corpus carries the capability: `adaptive-runs.md:22-39` is the section people
actually ask for, `:489-497` names wide work as a task outright, `tasks.md:165`
calls the `adaptive` preset an opt-in override. A grep for denials
(`cannot be divided`, `cannot split`, `impossible`) returns zero hits.
`TestTheBeltRoutesWideWorkToOneWorkerAndNotToAPlanner`
(`task_divide_test.go:544`) pins both the new sentences and the **absence** of the
two that produced the old reflex.

**Two defects CLAUDE.md warns about are fixed.** `read` no longer claims to list
directories — `system.md:49` says "It reads FILES only; a directory is an error,
so list one with `ls`", and the code agrees (`os.ReadFile`,
`internal/exec/bare/tools.go:204`). `note`/`forget` are gone: there is no
`MemoryFile` field on `session.Config` any more, and neither word appears in
`system.md` as a tool name. The memory belt is exactly one verb, `remember`.

**What is still wrong:** the inventory omits fifteen tools that are genuinely on
the belt (**m8**), and `read`'s description promises media perception with no
guard (**m9**). The unconditional advertisement of `propose_task` and `tasks` was
**fixed in-lane**.

---

# Findings, ranked

*Findings marked **FIXED** carry the date and the lane. The paragraphs under them
are left as they were written — they are the evidence the fix was made against,
and rewriting them into the past tense would leave nothing to check the fix
against.*

## CRITICAL

### C1 — `divide_work` is not on any production belt. The division road is inert. — **FIXED, w42, 2026-08-24**

> `newTaskAgent` now carries `Divide: parent.Divide` onto every worker it
> builds, and `armDivision` refuses any spec whose `kind()` is not the ordinary
> one (which is M4, landed in the same change). Pinned through the real
> constructor by `TestTheProductionConstructorCarriesTheRoadOntoTheWorker`,
> `TestAHarnessDesignIsNeverArmedToDivide` and
> `TestTheRoadOffProducesWorkersWithoutTheVerb` — none of which writes a Config
> literal.

`Config.Divide` has exactly one production setter — `cmd/aforge/chatv3.go:640`,
`Divide: settings.Swarm` — and it sets it on the **conversation**. But
`Config.mayDivide()` (`task_divide.go:138-144`) also requires `mayFanOut()` =
`InTask && tasker != nil && taskDepth < limit` (`task.go:301`), which a
conversation never satisfies. Every task worker does satisfy it, and every task
worker is built by `Agent.newTaskAgent` (`task_run.go:3237`) — the sole
production constructor, called from `task_run.go:2263`, `task_audit.go:793` and
`harness_task.go:513` — whose `Config` literal at `task_run.go:3304-3383` copies
about thirty fields from the parent and **does not copy `Divide`**.

So `mayDivide()` is false for every agent in the running program: `divideTools()`
returns nil (`task_divide.go:114-117`) and `renderSystemAt` omits
`prompts/divide.md` (`prompt.go:120`). Meanwhile `armDivision` runs correctly at
admit via `g.home` (`task_run.go:686`), `node.dividing()` returns true, and the
capability is promised in five places: the roster line
(`internal/tui3/taskcommand.go:162`), the schema (`task.go:119`), the prompt
(`system.md:122-130`), and three manual pages (`adaptive-runs.md:22-30`,
`:493-497`; `tasks.md:1584-1620`).

Measured, not inferred. A probe built the worker through the production
constructor with the session's road on and the node armed:

```
child Config.Divide      = false
child mayFanOut          = true
child node.dividing      = true
child mayDivide          = false
divide_work on the belt  = false
divide page in prompt    = false
```

**Why no test caught it:** every division test hand-builds the worker's config
with a literal `Divide: true` — `task_divide_test.go:56-64`, `:470`, `:509` —
including the one written expressly to prevent this failure, whose comment reads
"a decision the worker's hands never hear about is the road open on paper only"
(`:504-506`). All eighteen pass. None calls `newTaskAgent`.

**The fix is two parts and must land together.** (a) `Divide: parent.Divide,` in
the `newTaskAgent` literal, beside `tasker`/`taskID`/`taskDepth`
(`task_run.go:3380-3382`). (b) A guard so it does not newly reach the wrong nodes
— see **M4**: either clear `spec.divide` for `spec.design != nil` / `spec.run !=
nil` inside `admit` (`task_run.go:686`), or gate `mayDivide` on the node's kind.
Plus a test that goes through `newTaskAgent` rather than around it. Not applied
here: turning a whole road on in production is a lane, not an audit.

### C2 — a deopted subharness escapes the tool ceiling and the consent gate it was approved under. — **FIXED, w42, 2026-08-24**

> **The linear generalist genuinely cannot be caged, so the honest half of the
> fix shape was taken.** `Toolbox`'s five tools are unconditional and sit in a
> fixed order by a stated cache law (`Definitions`), and `Arm` only ADDS optional
> families — there is no subtractive seam and building one would rewrite the
> block the law is about. So the decision moved to `exec.Deopt`, which now takes
> the PROGRAM'S own manifest: `DeoptHeld` says the long way may only be taken
> where the approved ceiling already reaches a shell (`bash` or `sh` — the shell
> subsumes write, edit and web, so a ceiling that reaches it reaches everything
> the fallback uses). Otherwise the run stops **incomplete** with
> `DeoptHeldWord`, in the person's own register, and the generalist is never
> reached. Both surfaces ask `DeoptLineFor` before they announce anything, so
> neither says "handled it the long way" over a run that is about to stop, and
> `Deopt` refuses again inside itself so neither can skip it. `ExecutorRunner.Run`
> now names its Env parameter `_` and says in prose why an executor has nothing
> to do with one. Pinned by `internal/exec`'s
> `TestACeilingThatDoesNotReachAShellHoldsTheLongWay`,
> `TestAHeldFallbackNeverReachesTheGeneralist`,
> `TestACeilingThatReachesAShellStillFallsBack`, and by
> `TestALongWayThatWouldReachPastTheCeilingIsNotTaken` on both surfaces.

`ExecutorRunner.Run(ctx, input, env Env)` (`internal/exec/runner.go:422-457`)
**takes the `Env` and never reads it** — the parameter is unused in the whole
body — and the generalist it hands the work to is belted with unconditional
`sh`/`job`/`write`/`edit`/`web` (`internal/exec/tools.go:762-798`).
`grep -rn "Approv" internal/exec/` returns nothing: there is no approval policy in
that package at all.

During the program, every call is checked against `Manifest.Whitelist` — a
ceiling, where empty means *no tools* (`subharness_env.go:391-404`) — and routed
through the approval gate (`:349-353`). After the deopt, neither exists. A
subharness a person approved with `Whitelist: []` becomes an unrestricted shell
agent in their workspace, silently, with no second question and nothing on any
surface saying the ceiling came off. `subharness_env.go:140-141` asserts the
opposite in a comment.

Fix shape: `ExecutorRunner.Run` must build its executor's belt from
`env`'s filtered toolbelt and its approval seam, or the deopt must re-ask. Either
way the ceiling a person approved has to survive the fallback.

### C3 — the division refusal names the wrong cause, and the machine hold is terminal where the same hold is temporary for `propose_task`. — **FIXED, w42, 2026-08-24**

> **The asymmetry was the finding, so the asymmetry is what went.** `freeHands`
> no longer asks the governor at all: it is the person's `task.parallel` cap and
> nothing else. A busy machine no longer refuses a division — the parts are
> admitted, the frontier holds them as `waiting · machine busy`, and `armPoll`
> lifts them by itself, which is byte-for-byte what work admitted through
> `propose_task` has always done with the same reading. `divisionDone`'s receipt
> says the parts are waiting and that there is nothing to come back for.
> `divisionNoHands` became `divisionNoLane`, which names the cap rather than "no
> free hand", drops the untrue worktree-cost claim (a worktree is made at START),
> and tells the worker to ask again ONLY where a lane can actually come free —
> at `task.parallel = 1` it says plainly that asking again will not change it,
> which also closes **m14**. Pinned by
> `TestADivisionNobodyIsFreeToPickUpIsDeferredRatherThanTaken`,
> `TestADivisionHeldByBusyLanesInvitesTheWorkerBack` and
> `TestABusyMachineHoldsTheDivisionsPartsRatherThanRefusingIt` — the last of
> which is the governor × division interaction **m18** listed as uncovered.
> `internal/manual/chat/tasks.md`'s division section carries the new words.

`freeHands` (`task_divide.go:404-423`) answers 0 when the governor holds
(`:410-413`), and `divideWork` then returns `divisionNoHands` (`:365-369`):
*"there is no free hand to take the N parts right now, and parts that can only
wait would cost a copy of the repository each … ask again later."* Three things
are wrong with that on a loaded machine:

- **"no free hand" is false.** With `task.parallel` unset, `limit` is 0 and
  `freeHands` would answer `taskFanLimit` (`:417-418`) — every lane is empty. The
  cause is machine load, and the engine owns the honest word three lines away:
  `waitingMachineBusy = "machine busy"` (`task_run.go:170`).
- **"would cost a copy of the repository each" is factually wrong for this
  cause.** A worktree is created in `workTaskNode` at START
  (`task_run.go:2249-2252`), never at admit, so a part held on machine load
  costs nothing.
- **"ask again later" is the worker's only path**, and it is strictly worse than
  what the frontier does with the identical condition: work admitted through
  `propose_task` queues, polls every five seconds (`task_run.go:935-953`),
  announces `waiting · machine busy`, and lifts itself. A division refuses
  silently and terminally, emits no event, and `internal/tui3` has no rendering
  for a division refusal at all — so no person is ever told.

The asymmetry is the finding: the same machine hold makes the *default road for
wide work* fail closed while the exception queues. `freeHands`'s claimed parity
with `runFrontier` (`task_divide.go:389-391`) is not real either — the frontier
asks the cap first and the machine second (`:858-862`, pinned by
`TestTheCapOutranksTheMachineOnTheWire`) while `freeHands` asks the machine
first, and the frontier exempts `!takesSlot()` nodes from both ceilings
(`:851-857`) while `freeHands` has no such branch. No test covers the
interaction: every `freeHands` test builds a graph whose governor is nil, and
every governor test drives `runFrontier` and never `divideWork`.

## MAJOR

### M1 — the route judge's card is the one model-decided wide-work door that admits unarmed. — **FIXED, w42, 2026-08-24**

> The three edits below were applied as written, now that C1 makes them live:
> `routeVerdict.Wide`, the `wide` line in the judge's brief and its yes-example,
> and `wide: verdict.Wide` on the spec. Pinned by
> `TestTheJudgesWideVerdictArmsTheTaskItStarts` (over a goal `splitgate` counts
> as zero, so only the judge's own word can have armed it) and
> `TestARouteYesWithoutWidthArmsNothing`.

`launchRouteTask` (`route_judge.go:395-413`) builds a `taskSpec` with no `wide`,
no judge-bank entry (its `request` is the live user message, not the judged
text), and therefore only `splitgate.WorthIt` over a goal the judge wrote.

`splitgate.Items` (`splitgate.go:54`) counts only a digit run whose *immediately
adjacent* field begins with one of eighteen whitelisted nouns (`:35-37`), or a
spelled number six–twenty with such a noun inside forty bytes (`:42-47`).
Measured on real strings:

| text | items |
| --- | --- |
| `the adapters directory holds 11 files` | 11 ✓ |
| `twelve image files` | 12 ✓ |
| `eight endpoints` | 8 ✓ |
| `research the pricing tiers of every major cloud provider` | 0 ✗ |
| `sweep across many files and update the header in each` | 0 ✗ |
| `audit every trajectory the system can take` | 0 ✗ |
| `bring the eleven adapters up to the new interface` | 0 ✗ |

The judge is never asked for a count anywhere in `routeJudgeBrief`
(`route_judge.go:109-138`) — it is asked for a self-contained goal. So the card's
task is, in practice, never armed. And the manual already states the capability
as fact: `adaptive-runs.md:288-290` — "`task` is what it offers for wide work …
one worker starts and splits itself if the material is wider than one pair of
hands."

**The flagged follow-up, answered: yes.** The judge's wide/adaptive-shaped yes
should arm the task it cards. It is the same judgement `taskSpec.wide` exists to
carry (`task.go:235-250`); it is free; a wrong yes costs nothing because the
evidence gate still refuses (`task_divide.go:247`); and giving the judge an
honest place to put "wide" removes its reason to reach for `adaptive` to express
it — which matters, because `canRunShape` (`route_judge.go:217-225`) is a
feasibility check only and nothing in code inspects *why* the judge said
adaptive. Minimal change, three edits, no new machinery:

1. `route_judge.go:98` — add `Wide bool` with tag `json:"wide"` to `routeVerdict`.
2. `route_judge.go:123`/`:130` — add `"wide": true` to the yes-example and one
   line to the shape paragraph defining it.
3. `route_judge.go:409` — `wide: verdict.Wide,` on the `taskSpec`.

Deliberately not applied: it is dead behind C1 until C1 lands, and the two should
be judged as one change.

### M2 — a division part's whole spend is lost from the session ledger on any stopped ending. — **FIXED (first half), w42, 2026-08-24**

> `foldTaskUsage` now charges `Agent.spendLedger(node)` rather than the owner
> directly: an owner that has already closed its own books is skipped and the
> money goes straight to the graph's home, which is the session ledger. Same
> total, one hop shorter, and it covers the whole class — `propose_task`
> children stopped with their parent took the identical road. Pinned by
> `TestAStoppedPartsSpendStillReachesTheSessionLedger`. **The second half is
> still open:** the parent node's recorded cost includes its parts' while each
> part records its own, so a surface summing node rows double-counts.

`workTaskNode`'s defer closes the parent worker (`task_run.go:2269`) and folds it
(`:2276`); `stopChildren` does not run until `:2126`, *after* `work()` has
returned. So on any stop, threshold or deadline ending, every part stopped there
folds into an already-closed parent-worker agent whose `Usage()` nobody reads
again (`addAuxiliaryUsage` has no closed check, `loop.go:2127-2155`). The money
survives on the part's node row and index entry (`task_index.go:614`) — visible
and uncounted at the same time. This is an engine-wide ordering bug, not a
division-specific one: `propose_task` children have it too. It breaks the
one-ledger law on exactly the path a person takes when they are worried about
spend.

Second and smaller: the parent node's recorded cost *includes* its parts' while
each part records its own, so any surface summing node rows double-counts —
contrast orchestrate, where the root's figure is the tank and not the sum
(`taskcost_test.go:222-226`).

### M3 — a restart is lossy in three separate ways.

**(a) Arming is re-derived, and the comment understates the loss.**
`task_store.go:1037-1040` says a restart loses only the sizing judge's yes.
`spec.wide` is equally unrecoverable — it is not on `taskRecord` and
`restoreNode` never sets it (`:1002-1015`). That matters because
`task.go:243-249` argues `wide` rides on the spec *because* the bank is
unreliable, an argument that assumes a durability the record does not provide.

**(b) A restored parent can divide a second time.** `mayDivide` consults only
`spec.divide`; nothing records that `divide_work` already fired, and `divideWork`
(`:236-307`) has no idempotence check.

**(c) Parts that finished before the crash never reach the re-run parent.**
`rehydrate` marks restored non-queued nodes noted itself
(`task_store.go:974-976`, `:983-986`) and routes their reports to the
conversation's ambient lane (`:868`); `reported()` reads `n.noted`
(`task_run.go:2880-2884`), so the parent's tail loop cannot see them and folds an
incomplete set.

No test covers arming across a restart: `task_store_test.go`, `recovery_test.go`
and `replay_test.go` never mention `divide` or `wide`.

### M4 — a harness design can be armed for division. — **FIXED, w42, 2026-08-24**

> Gated by kind at the one arming door, as C1 required: `armDivision` returns
> false for any spec whose `kind()` is not the ordinary one, and `restoreNode`
> asks `record.Kind` — not the rebuilt spec, which for a design whose page never
> finished carries no `design` field at all. It closes **m3** in the same line.
> The related-and-smaller half is still open: a design thread still carries
> `propose_task`.

`admitHarnessDesign` (`harness_task.go:462-470`) passes the goal as `brief`, and
`admit` asks `armDivision` of it like any other spec. A goal saying "a harness
that checks the 8 endpoints" therefore sets `spec.divide = true` on a *page
writer*. The design thread satisfies `mayFanOut` (`tasker` set at
`task_run.go:3245`, depth 1), so the moment C1 is fixed it would be handed
`divide_work` — a verb that spawns workers in worktrees, for work that is two
model calls producing one JSON page (`harness_build.go:513`) with no worktree of
its own (`harness_task.go:512-513`). C1 and M4 must land together.

Related and smaller: a design thread already carries `propose_task`
(`task.go:293` + `task_run.go:3245`), so a page writer can admit ordinary tasks,
`wide` ones included. Bounded — it cannot reach a planner — but nothing in
`harness_task.go` argues for it.

### M5 — a standing firing cannot divide, and cannot fan out either.

`ActionTask` runs "a fresh headless session in the run folder, one turn on the
brief" (`standing_run.go:498-505`). `standingRunConfig` (`:631-663`) sets
`InTask=true` but leaves `tasker` nil, so `mayFanOut()` is false (`task.go:301`)
and the session has neither `propose_task`, nor `tasks`, nor `divide_work`;
`run_adaptive` needs `AskConsent` and gets none. `v3StandingPosture`
(`cmd/aforge/chatv3_standing.go:162-203`) never sets `Divide` either.

So the one place nobody is watching — overnight work, the case the ambient side
exists for — is the one place width is unaddressable: it runs strictly
sequentially inside one 60-step turn (`standing_run.go:92`). A gap against law 1,
not a bug in what is written.

### M6 — three one-source-of-truth breaks between a description and its code.

**(a) `wide` is silently inert when Swarm is off.** `armDivision` returns false at
`task_divide.go:192` before it ever looks at `spec.wide`, but the schema at
`task.go:119` promises "the worker may hand the parts out under itself" with no
hedge. Either the description hedges, or `AFORGE_SWARM=0` removes the property
the way every other conditional capability on this belt is removed.

**(b) The manual is wrong on two safety-relevant limits.** — **FIXED, w42,
2026-08-24.** `tasks.md`'s division section now states the capacity test as the
`task.parallel` cap (which is what it is, since C3 took the governor out of it)
and says outright that `task.max_load` / `task.min_free_mb` never refuse a split
— the parts wait and lift themselves. `saved-programs.md` gained *When the long
way is not taken — the ceiling holds*, which is now true of the code (C2).

**(c) `armDivision` ignores the escape hatch** — see M8.

### M7 — every kind of standing item rides into every worker's brief as a house rule.

`Store.Applicable` (`internal/standing/store.go:175-194`) filters on `Status` and
`AppliesTo` and never on `WhenKind`, and `renderStandingWorld`
(`standing_world.go:65-95`) writes whatever comes back under
`standingWorldIntro` — "They … are not suggestions. Work within them."
(`standing_world.go:33`). `Item.Prompt()` falls back to the person's raw words
for any kind (`standing.go:452-457`). So "remind me at 6 to check the deploy" is
presented to every task in the project as a rule to work within, until it
retires. Only a `hold` is a rule; the other five kinds are schedules.

### M8 — `AFORGE_SPLITGATE=0` has a reader that ignores it.

`divideWork` guards the evidence gate with `splitgate.Armed()`
(`task_divide.go:247`), but `armDivision`'s third signal calls
`splitgate.WorthIt` unguarded (`:201`). The stated law is one reader
(`splitgate.go:146-149`, `cmd/aforge/cooperative.go:169-172`), and
`internal/config/settings.go:782-784` and `tasks.md:1643-1645` both document the
switch as taking the width test away. With the gate off, work armed by neither
the judge nor `wide` still never gets the verb.

### M9 — the model has no route to stop work it did not start directly.

`Agent.Cancel` has no caller on the belt; the `tasks` tool's verbs are search /
read one / `say` / `resolve` (`tools_tasks.go:50-58`, `:123-153`) with no cancel
op. There is no `/stop` command, `esc` is explicitly never stop
(`internal/tui3/stop.go:54-58`), and `Interrupt` cannot reach a task because a
node runs on `context.Background()` by construction (`task_run.go:2086-2089`).
The model's only stop verb is `jobs kill`, and a part's job row is registered on
the **parent worker's** registry (`task_run.go:2098`), so the conversation's model
can kill the parent and never one part. For the person the routes are complete —
the stop card reaches `Agent.Cancel` with `task:N` / `run:N` for every roster row,
parts included. Law 4 holds for the person and not for the model; say which one
is meant, then close the other.

### M10 — a deopted run's pre-deopt spend never reaches the ledger.

`subharness_run.go:197` overwrites `result` wholesale, so the failed program's own
spend is discarded, and `runSpend` (`:273-278`) returns one of the two halves and
never the total. Contradicts `internal/manual/chat/saved-programs.md:131-134`.

Related: for most typed subharnesses the long way cannot run at all.
`internal/exec/runner.go:429-431` demands a `brief` field, while the intake card
emits only the manifest's declared fields (`subharness_intake.go:284-297`). Any
manifest without a `brief` deopts into `"linear" needs a brief` and lands
`TaskFailed` (`subharness_run.go:225-227`) — the opposite of the promise at
`internal/exec/deopt.go:10-16`. The passing test hand-writes `{"brief":…}`
(`subharness_doors_test.go:368-370`); the workaround is in the test, not the code.

### M11 — the orchestrate planner is never taught the failed-need rule.

A failed node is abandoned: `metLocked` (`internal/orchestrate/run.go:461`)
returns false forever for a dead need, so downstream nodes sit `Queued` in place.
Recovery exists only through the planner as cancel-and-re-add
(`amend.go:355` → `:449`) — and `orchestratePlannerLaw`
(`orchestrate.go:837-886`) contains **no clause about a failed need at all**, even
though `run.go:459-460` explicitly delegates that judgement to it. The one
recovery the design depends on is left for a model to infer. A node stranded that
way also keeps a live `queued` roster row after its run has closed
(`orchestrate.go:1787-1804`) and never reaches the project index (`:1635`).

### M12 — steering a parked parent has no effect, and can be dropped outright. — **FIXED, w43, 2026-08-24**

> **The line is now the news.** `SteerTask` marks it as the person's own
> (`userMessage.steered`) and hands it over through `enqueueSteeredLine`, which
> RELEASES the wait the runner is holding — the same release a report makes,
> without being a report. The tail loop reads `steeringHeld()` beside the owed
> count: it will not park on top of a queued line and will not break and close
> the child while one is still there, so the sentence always reaches a request.
> A worker that has already closed refuses out loud — *"task 3 has just
> finished, so there is nobody left to say it to"* — instead of swallowing the
> line. And the door answers WHETHER THE NODE WAS WAITING, so both surfaces say
> which wait the line landed in: the `tasks` tool with *"It was waiting on the
> pieces it handed out; your line wakes it"*, and the room with its own note,
> `it was waiting on its pieces — your line wakes it`. Pinned by
> `TestSteeringAParkedParentWakesItAndArrivesInItsNextTurn`,
> `TestALineSteeredAtAClosedWorkerIsRefusedRatherThanSwallowed` and, on the
> surface, `TestSteeringANodeWaitingOnItsPiecesSaysWhatTheLineJustDid` /
> `TestSteeringAWorkingNodeSaysNothingExtra`. `tasks.md` has a section of its
> own for it.

`SteerTask` accepts the line — a parked node's state is still `TaskRunning`
(`park` sets only `node.parked`, `task_run.go:2187-2201`) — and the tool answers
*"It arrives in its loop as the person's own words"* (`tools_tasks.go:437`). It
does not: `wakeLocked` declines because `config.InTask && !roomThread`
(`agent.go:1526-1529`), and the tail loop's park select wakes only on a child's
report (`task_run.go:2799-2803`). The correction sits queued for as long as the
slowest part runs, and if the last part reports and the loop breaks at
`:2793-2795` with a line still queued, the child is closed at `:2269` and the line
is dropped without ever reaching the transcript.

## MINOR

- **m1 — a parked parent reads two ways.** `WorkingNow` says waiting
  (`work_tree.go:161-170`); `TaskNotice.Waiting` has no parked value — `held` is
  written only while QUEUED (`task_run.go:1619-1625`) — and the task rail is built
  from notices, not from `WorkingNow` (`internal/tui3/task.go:2551-2568`), so it
  draws a parked parent as plainly running. Two accountings of one node, which
  `work_tree.go:12-17` forbids. `park` never announces.
- **m2 — division parts are marked `divide: true` by accident.** A part inherits
  `request` from its parent (`task_divide.go:282`), the judge banked exactly that
  sentence, and `judgedDivisible(spec.request)` (`:198`) matches. Inert because
  parts sit at depth 2. The invariant should not depend on `taskDepthLimit`
  staying at 2.
- **m3 — `admit` arms nodes that have no belt.** — **FIXED, w42, 2026-08-24**,
  by M4's kind guard: `armDivision` refuses a spec whose `kind()` is
  `subharness` before it reads `manifest.Purpose`, so the flag is never written.
- **m4 — a sub-harness run is cancellable but is not a row in `WorkingNow`.**
  `beginHarnessRun` (`cancel.go:269-292`) registers it for `Cancel("harness:<n>")`,
  but `work_tree.go:86` is tasks plus runs plus jobs. Mitigated: the run happens
  inside one turn, and `EventHarnessRun` draws a dim line
  (`internal/tui3/harness.go:148`).
- **m5 — two headless doors are looser than the third.** `aforge exec` builds its
  toolbox with no approval policy at all (`cmd/aforge/exec.go:91-100`), unlike the
  subharness door (`subharness_run.go:164`). And `aforge do`'s plan gate
  (`cmd/aforge/cooperative.go:158-175`) lets width alone keep a divided plan, at a
  door where nobody asked for a planned graph. Both are coherent inside the
  resident product; noted as cross-product drift.
- **m6 — the long way is invisible while it runs.** Because the `Env` is discarded
  (C2) there is no journal entry, no room event and no phase update
  (`subharness_run.go:255`): the person watches an unmonitored agent do shell work
  behind a frozen row.
- **m7 — the 80% fuel warning reaches the person as a bare figure**
  (`internal/tui3/roomorch.go:662-677`) while the planner gets a sentence
  (`orchestrate.go:793-796`); and a paused run tells the surfaces but never the
  model (`:395` vs `:463`), so the assistant can report a gated run as still
  working.
- **m8 — the inventory omits fifteen tools that are on the belt.** Present on a
  plain chat belt and named nowhere in `system.md:20-38`: `read_document`,
  `watch`, `track`, `commit`, `recall`, `web_search`, `web_fetch`,
  `generate_image`, `speak`, `generate_music`, `generate_video`, `view_image`,
  `divide_work`, `revise_design`, and the connect family. Under-promising rather
  than over-promising, so no absence law is broken — but the media family is a set
  of verbs the model will not plan around.
- **m9 — `read`'s description promises perception with no model behind it.**
  `senseRead` (`tools_sense.go:213-215`) appends `senseSentence` (`:68`) — "Never
  write a script to decode any of them" — with no guard, while `view_image` beside
  it is gated on `visionSeer() != ""` (`tools_view.go:91`). It degrades honestly at
  call time (`tools_sense.go:439`), which is why this is minor rather than the
  `note`/`forget` shape.
- **m10 — `task.start` is not on the self-service guard list**
  (`internal/config/selfservice.go:77-108`). `change_setting` can permanently move
  a person's default road to `adaptive` — the one preset that opens a planner and a
  fuel tank. Not silent (the call goes to the person like `edit`/`write`), but
  under an allow-all policy or `--yolo` a model can flip it. The file's own rule is
  "ERR TOWARD REFUSING" (`:27-29`).
- **m11 — the sizing judge's yes is a one-entry bank, and the typed door races
  it.** `StartTask` runs `shapeBrief` (up to 25s) before `admit`
  (`task_person.go:70`, `:112`); a second `/task` whose judge answers yes inside
  that window overwrites `divisibleAsk`, and the first task admits unarmed — after
  the surface has already promised it can split. `taskSpec.wide`'s own comment
  identifies the identical hazard for concurrent proposals and solves it by
  putting the judgement on the spec (`task.go:243-249`). The same change fixes
  **M3a**, because the spec is persisted.
- **m12 — the ninth standing order silently stops reaching workers.**
  `standingWorldMost = 8` (`standing_world.go:44`), clipped newest-first with a
  `…N more` line in the brief and nothing on any surface.
- **m13 — an interactive surface auto-approves its own human gates.**
  `cmd/aforge/chatv3_harness.go:150-154` wires the harness runner with no `Ask` —
  `// No Ask: a gate auto-approves here and the trail says so.` — so a saved
  harness's `human.gate` is waved through by
  `internal/subharness/exec_model.go:272` on the one surface with a person sitting
  at it, and `:648-652` propagates the nil `Ask` into nested calls. Honestly
  declared as a known absence (`chatv3_harness.go:43-48`), honestly recorded in the
  trail, and already named "the anti-pattern" by
  `docs/SUBHARNESS-PRD.md:266-269`. Recorded because law 3 says a yes is never
  assumed and the person **is** there.
- **m14 — `"ask again later"` is untrue at `task.parallel = 1`** — **FIXED, w42,
  2026-08-24**: `divisionNoLane` has two sentences, and the one-lane one ends
  "asking again will not change this".
- **m15 — a parked parent has no deadline.** `limits.deadline` is checked only on
  tool-end events (`task_run.go:2718-2726`) and a parked parent emits none, so a
  parent waiting on a part the governor never starts waits until a person stops it.
- **m16 — `run:<n>/<node>` is a mintable id `Cancel` cannot resolve**
  (`work_tree.go:229` vs `cancel.go:87`). Documented and unused by tui3; a trap for
  the next surface.
- **m17 — machinery vocabulary leaks through the deopt's `because` clause** — Go,
  goja and JSON error text is appended verbatim to the notice and the settle card
  (`subharness_run.go:194-195`). The constant obeys the vocabulary law; the
  sentence it builds does not. Same shape as `"allowed by the guardian"`
  (`guardian.go:162`).
- **m18 — test-coverage holes behind the above:** the governor × `freeHands`
  interaction, the stopped-parent fold ordering, steering a parked parent, arming
  across a restart, a settle with a stranded `Queued` node, a part's own journal,
  and the deopt's belt are all uncovered. **Three closed w42, 2026-08-24** — the
  governor × division interaction, the stopped-parent fold, and the deopt's
  ceiling (see C3, M2, C2). The other four stand.

---

# What `newTaskAgent` still does not carry down

C1 was one missing field in a literal that copies about thirty. So the whole
literal was diffed against `session.Config` (`session.go:716`). Thirty-eight
fields are not copied. Most are deliberate and the reasons are recorded here so
the next reader does not have to re-derive them; the rest are named as gaps and
were **not** fixed in this lane, because each is its own product decision rather
than a consequence of turning the division road on.

**Deliberate, and argued somewhere in the code:**

| field(s) | why a node does not get it |
| --- | --- |
| `Standing`, `standingItems` | orders reach a node through the brief and only the brief (`standing_world.go`); a per-node door would say everything twice |
| `Memory`, `MemoryImport` | a node is handed no store, which is exactly what keeps `remember` and `search_conversations` off its belt (`tools.go`) |
| `Harnesses`, `HarnessStore`, `RunHarness`, `HarnessCards`, `HarnessDesignWindow`, `Subharnesses`, `SubharnessMemory`, `SubharnessLastRun`, `SubharnessRecordRun`, `OrchestrateRunner` | the three big machines are deliberately not handed to a node — `tools.go:100-107` states it: a node can neither commission a procedure nor start a run of its own |
| `TaskParallel`, `TaskMaxLoad`, `TaskMinFreeMB` | graph-level. Read once when the graph is built (`task_run.go:625-626`) and shared through `tasker`, so a node reading its own copy would be a second answer to one question |
| `TaskDeadline`, `TaskRepairRounds`, `TaskAutoApproveSeconds` | read on the agent that OWNS the frontier's bookkeeping, never on the worker |
| `TaskModel`, `TaskModels` | absent means a part inherits its parent worker's LIVE model (`defaultTaskModel`), which is the right answer for a part |
| `System` | replaced wholesale for a node by `renderSystemAt` |
| `writeScope` | only ever set by orchestrate (`orchestrate.go:1063`); no conversation carries one |
| `Errand` | an errand is a shape a conversation has, not a node |
| `Guardian` | a node's policy is allow-all with `AskConsent=false`, so there is no consent prompt for a guardian to turn into an allow |
| `WorktreeRoot` | orchestrate-only (`orchestrate.go:1292`) |

**Named as gaps, not fixed here:**

- **`Routing`, `ModelFallbacks`, `NearestModels`** — **STALE, w43, 2026-08-24.
  The conclusion was drawn from the Config literal and is wrong about the
  running program.** All three are read in exactly one place — `New`, where they
  are handed to the provider client (`agent.go:43-60`) — and `newTaskAgent` does
  not build a client at all: it hands the node THE PARENT'S OWN, unwrapped
  (`client := unwrapCompleter(a.client)`, and `agent.go:230`'s own comment says
  so). So a worker asks through the person's routing strategy, falls back down
  the person's list and gets the catalog's nearest-model rescue — transitively,
  all the way down to a part, whose parent's client is the conversation's. The
  three are facts about the connection and there is one connection; copying them
  onto a node's Config would be three fields nothing reads. `memory_consolidate.go:242`
  and `standing_run.go:755` are not a contrast, they are the same rule seen from
  the other side: those two BUILD clients, so they must carry `Routing`.
  Measured through the real constructor and pinned by
  `TestTheProductionConstructorCarriesTheProviderRepairOntoEveryWorker`, which
  also fails if anybody gives a node a connection of its own.
- **`SupportsParameter`** — **FIXED, w43, 2026-08-24**: verified exactly as
  written and copied down, so the no-tools rescue works at every depth.
  `TestAPartIsBuiltWithTheSameNoToolsRescueItsParentHad` builds the part through
  the real constructor and checks that the catalog was asked about the PART'S
  model and that the swap is on its row.

  Used *inside* `newTaskAgent` to refuse a model that cannot use tools, and not
  copied down. So the rescue works exactly one level deep: a part, or a
  `propose_task` child of a node, is built by an owner whose `SupportsParameter`
  is nil and gets no check at all. C1 makes this reachable far more often than
  it was.
- **`TaskProgressCheck`** — **FIXED, w43, 2026-08-24, with the finding's last
  sentence corrected.** The field is the TEST SEAM (`session.go:930-932`);
  production leaves it nil and `taskProgress` asks the real read-only checker
  either way, so a part's leash was never unchecked — what stopped one level
  down was any test's ability to put a deterministic answer behind it. Copied,
  because a seam must reach as far as the machinery it stands in for
  (`TestAPartsLeashIsCheckedByTheSameHandTheConversationSet`). `TaskDeadline` is
  read on the same owner (`taskLimits`) and has the same shape; it is left
  alone, so a part's checkpoint is still reachable in a test only through the
  step and no-progress thresholds.

  Asked of the node's `owner` (`task_run.go:2645-2654`), and a part's owner is
  the parent worker, whose config has none. The no-progress check never runs for
  a part.
- **`Place`** — **FIXED, w43, 2026-08-24, and not by copying it.** A node with a
  Place would be a second session: `Place` is what turns on the meta file, the
  presence rows, the delta reader and the artifacts directory, all of which
  belong to the conversation and would be written twice. So the PATHS ask the
  family instead — `Agent.familyPlace(node)` answers the graph's `home`'s Place,
  and it is what `workTaskNode` (worktree, resume, git-root lock),
  `newTaskAgent` (journal) and `task_audit.go`'s accept/re-audit
  (`node.workingCopy`) now read. One family, one folder.
  `TestAPieceOfATasksWorkKeepsToTheSameSessionFolder` puts a part's worktree
  under `<session>/trees/<id>` and its transcript in `<session>/tasks/`, and
  fails on the old reading.

  `workTaskNode` reads `a.config.Place` for `prepareTaskTree` and
  `lockGitRoot`. A part's owner is the parent worker with an empty `Place`, so a
  part's worktree lands under the repository's default `tasks/<session>/<id>`
  rather than inside the session's own folder (Decision 26), and the git-root
  lock is keyed on a different place. Pre-existing on the nesting road — a
  `propose_task` child of a node already does this — but division makes it
  common.
- **`ArtifactsIndex`** — media a node generates is not recorded in the artifacts
  index the conversation's is.
- **`SpendRailUSD`, `ReplyGuardOff`, `ProfileDir`** — three person-set defaults a
  node simply does not have. The rail one matters most: `railBlockLocked` cannot
  stop a runaway family, only the conversation's next turn.

# What was fixed in the routes lane (w43, 2026-08-24)

Two of the constructor's named gaps and M12. Every claim was re-verified in the
tree before it was acted on; one had gone stale and is marked as such above
rather than quietly deleted.

1. **The constructor's carry set.** `SupportsParameter` and `TaskProgressCheck`
   are in the literal; `Place` is answered by `Agent.familyPlace` at the three
   readers that need it rather than copied onto the node; the provider-repair
   trio is recorded as travelling with the CLIENT, with a comment at
   `unwrapCompleter` saying so and a test that fails if a node is ever given a
   connection of its own. Everything is pinned through `newTaskAgent` — no
   worker Config is written by hand in `task_carry_test.go`.
2. **M12.** A steered line marks itself as the person's, releases the wait a
   parked runner is holding, and cannot be parked on or closed over; a worker
   that has closed refuses in words; both surfaces say which wait the line
   landed in.
3. Manual: `tasks.md`'s new *Steering a task that is waiting on its pieces*
   section, the division section's own sentence about it, and two probes.
4. This document's verdict rows and constructor bullets.

# What was fixed in the roads lane (w42, 2026-08-24)

The lane's own record. Every claim above was re-verified in the tree before it
was acted on; none had gone stale.

1. **C1** — `Divide: parent.Divide` in `newTaskAgent`'s literal, with M4's kind
   guard in `armDivision` and `restoreNode`. Three tests through the real
   constructor.
2. **C2** — `exec.Deopt` takes the program's manifest and refuses to hand a
   caged program to an uncageable generalist; `DeoptHeld`, `DeoptHeldWord`,
   `DeoptWordFor`/`DeoptLineFor` are the new vocabulary, asked by both surfaces
   before either announces anything.
3. **C3** — the governor left the division's gate; `divisionNoLane` replaced
   `divisionNoHands`; `divisionDone` says when the parts are waiting on the
   machine.
4. **M2** — `Agent.spendLedger` skips a closed hop so a stopped part's spend
   still folds.
5. **M1** — the route judge's `wide`, applied as written.
6. Manual: `tasks.md`'s division section and a new `saved-programs.md` section.
7. This document's verdict rows, and the constructor audit above.

# What was fixed in the audit lane (w41)

Four comment, prompt and manual corrections. No behaviour changed; every
structural finding above was left as a finding.

1. `internal/session/prompts/system.md` — `propose_task` and `tasks` now carry the
   same "(when your tool list carries it)" hedge every other conditional verb on
   the inventory carries. Both are gated by `mayProposeTask` (`task.go:293`) and
   are genuinely absent on a floor node and on every repair round. (The class of
   defect CLAUDE.md records `note`/`forget` having caused.)
2. `internal/session/task_divide.go` — `armDivision`'s third bullet used "eleven
   adapters" as its example of a brief that enumerates enough items.
   `splitgate.Items` counts that as **zero**: `adapter` is not one of its eighteen
   item-nouns. The example now says "adapter FILES", and a new paragraph states
   that the noun is load-bearing and that this signal arms far less work than its
   wording suggests — which is the fact M1 turns on.
3. `internal/standing/standing.go` — `ActionTask` claimed a firing runs "with a
   worktree, a landing and a cost row, exactly as propose_task's work runs".
   `standing_run.go:501` says outright "IT IS A SESSION AND NOT A WORKTREE". The
   comment now says what the code does.
4. `internal/manual/chat/adaptive-runs.md` — the route-judge section said a yes
   starts a run on the default **$2.00** tank. `orchestrateDefaultCap` is `10.00`
   (`orchestrate.go:77`) and the same page says $10.00 forty lines earlier
   (`:78`). Corrected.

# Verified as-intended, so it is not re-audited

- `TaskGraph.admit` is genuinely the one arming door: two production
  `&TaskNode{}` constructions (`task_run.go:687`, `task_store.go:995`), two
  writers of `spec.divide` (`task_run.go:686`, `task_store.go:1041`).
- The planner is unreachable from every autonomous path: `run_adaptive` requires
  `AskConsent` (`tools_harness.go:108`), and `routeOrchestrate` additionally
  refuses woken and authored turns (`orchestrate.go:577-580`).
- Headless never auto-approves a tool question: `consent.go:282-284` refuses when
  nobody is watching, `:276` refuses inside a node, and the memo is barred from
  swallowing the critical floor (`consent.go:246`, `internal/approval/floor.go:27-45`).
- `internal/splitgate` is one implementation asked in three places, with one
  reader of its environment switch — modulo M8.
- The parent stays, parks and folds; the cancel cascade is real; `WorkingNow`
  never fakes liveness; the fuel gate matches its prompt.
