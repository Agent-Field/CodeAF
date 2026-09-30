# The spend rails — every money limit codeaf ships with

One page for every dollar figure in the build: what it bounds, what the person
sees when it fires, what zero means, and where the number lives so that raising
it is one edit.

**The law this page exists to state.** A limit nobody chose is a **backstop
against a runaway**, never a budget. A person who never opened settings has said
nothing about money, so the only defensible place to put their rail is where
nobody would defend the spend — not where an ordinary day's work reaches it. The
figures below used to sit at the second place: $20 a day, a $3 consent gate, a
15-cent standing firing, a $2.50 workflow. Every one of them fired on work that
was going exactly as intended, which turns a rail into a keystroke the person
owes, and a question asked every time is a question nobody reads.

**Zero means no limit.** On every rail where a ceiling can be lifted, `0` is the
person's own instruction and the reader honours it — including a `0` written
down, which survives a restart rather than reverting to the default in the
morning. The two exceptions are named below, and both are exceptions because
zero already meant something narrower and truer.

---

## The four rows a person turns

`/settings` → **spending**. These are the whole of what most people ever touch.

| Row | Key | Was | Now | What it does when it fires | `0` |
| --- | --- | --- | --- | --- | --- |
| **daily budget** | `daily_budget_usd` | $20 | **$500** | posts a blocking question — *"Daily budget reached -- $x spent of $y. Say the word and I'll continue"*. Nothing dies. | no limit |
| **ask before spending** | `plan_consent_usd` | $3 | **$100** | a planned job above this estimate quotes its step count and price and waits | never asks |
| **practice budget** | `practice_budget_usd` | $2 | **$50** | codeaf's own self-practice stops for the day | **practice off** — see below |
| **session ceiling** | `session.spendRailUSD` | $0 | **$0** (unchanged) | refuses the NEXT turn; the turn in flight always finishes; the refused message is never journaled | no ceiling |

Each row grows a **receipt** — the dim line beside its value — that says what a
bare `$0` cannot: `no limit`, `never asks`, `practice off`. The daily rail keeps
today's spend beside the word: `no limit · $4.25 today`.

**The practice carve-out is the one row where `0` is not "no limit".** It is a
*carve-out* and not a ceiling: zero switches self-practice off entirely
(`internal/resident`'s `WithPracticeLoop`), and there is deliberately no way to
spell "practice without a bound" — self-origin work runs while nobody is
watching, so it is the one pocket that always has a bottom.

**The crew's two limits live on `/crew`, not on this tab.** The **per task** row
here reads the per-task limit and opens nothing new: it is set with
`/crew cap task <dollars>` or on the panel's cap row (`per task $5 · crew daily cap none`).
It is the second rail where `0` is not "no limit" — a task always has a limit, so
`0` and `none` are refused and an emptied box is $5 again. The crew's **daily
cap** beside it is unset until somebody sets it with `/crew cap <dollars>`, and
`/crew cap off` takes it away.

## Every rail in the build

| # | Rail | Where the number lives | Was | Now | Unit | What it blocks | `0` = no limit |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | daily budget | `internal/config/config.go` `DefaultDailyBudgetUSD` | 20 | **500** | USD/day | asks; nothing dies | yes |
| 2 | plan consent | `internal/config/settings.go` `DefaultPlanConsentUSD` | 3 | **100** | USD estimate | plan waits for a yes | yes (never asks) |
| 3 | practice carve-out | `internal/config/config.go` `DefaultPracticeBudgetUSD` | 2 | **50** | USD/day | self-practice stops | **no — 0 is off** |
| 4 | session ceiling | `internal/config/settings.go` `DefaultSpendRailUSD` | 0 | **0** | USD/session | refuses the next turn | yes |
| 5 | lifted-tier cap | `internal/taxonomy/limits.go` `DefaultTierCapUSD` | 2 | **25** | USD per piece of work | stops buying a stronger model; the verdict comes back as work | yes |
| 6 | standing per-firing | `internal/standing/standing.go` `DefaultPerRunUSD` | 0.15 | **5** | USD per firing | interrupts that firing | **yes — newly so** |
| 7 | adaptive-run tank | `internal/session/orchestrate.go` `DefaultRunCapUSD` | 10 | **100** | USD per run | parks at a gate and asks for a top-up; running nodes finish | yes |
| 8 | workflow run bound | `internal/craft/types.go` `DefaultRunBudgetUSD` | 2.50 | **50** | USD per run | *"Cost check -- … Say the word and I'll keep going"* | no — 0 reads as the default |
| 9 | workflow ceiling | `internal/craft/types.go` `MaxRunBudgetUSD` | 10 | **500** | USD | the most a workflow file may grant itself | n/a |
| 10 | workflow wall | `internal/craft/types.go` `DefaultWallClock` / `MaxWallClock` | 30m / 2h | **6h / 24h** | time | stops opening new rounds; running leaves finish | no |
| 11 | discard consent gate | `internal/store/surgery.go` `SurgerySpendGateUSD` | 0.25 | **5** | USD already spent | asks before throwing running work away — raising it asks **less** | n/a |
| 12 | per-task limit | `internal/config/crew.go` `CrewTaskCapDefault` | none | **5** | USD per task | the call that would pass it is not made; the task stops on `this task reached its $5 limit · raise it in /crew`. `-yes-spend` does not lift it | **no — a task always has one** |
| 13 | crew daily cap | `internal/config/crew.go` `CrewCapAt` (`models.crew.cap`) | none | **none** | USD/day of crew spend | a task does not start, and a call that would cross it is not made; `codeaf do` refuses unless `-yes-spend` | yes (`off`) |
| 14 | checker ceiling | `internal/config/crewspend.go` `crewCheckCeilingTimes` / `crewCheckCeilingFloor` | none | **3× the checker's estimate, at least $0.05** | USD per check | the check stops and the task ends unchecked | n/a |

### Rails that already shipped unbounded, and stay that way

| Rail | Where | Default | Note |
| --- | --- | --- | --- |
| unattended session budget | `internal/session/principal.go` `Budget.USD` / `Budget.Wall` | **unset** | `--max-cost` / `--max-hours` (`CODEAF_MAX_COST`, `CODEAF_MAX_HOURS`), `--yolo` only. Unset is no ceiling: *"the $5.00 this was given is spent"* only ever fires on a figure somebody typed. |
| task subtree ceiling | `internal/store/task_budget.go` | **unset** | a `Set` flag separates "ungoverned" from "small", so nothing in the product installs one |
| errand cap | `internal/tui3` `ErrandOrders.CapUSD` | **0** | 0 is the launch's own rail, and therefore usually no cap at all |

### Not money, listed so nothing here is mistaken for a spend rail

`CODEAF_NODE_BUDGET` (60 nodes) and `--budget` on `codeaf run` / `codeaf exec`
(150 000 **tokens**) are counts. A task's own bounds are steps and time —
`taskDeadline` 60m renewable four times, `taskMaxSteps` 200, `taskNoProgress` 6
— and a task's dollar cap is the per-task limit set in `/crew` (`models.crew.task_cap`,
$5 by default), inside whatever rail the conversation that started it carries. `costHintUSD` (10¢) is the point
at which a first-run tip arms, not a ceiling.

## Where each number is set, in the order it wins

Environment pin → the row written into `~/.codeaf/config.json` → the built-in
default. A malformed *persisted* value falls back to the default rather than
stopping a launch; a malformed *environment* value is the operator's own
explicit instruction and still errors.

| Rail | Environment | Settings row |
| --- | --- | --- |
| daily budget | `CODEAF_DAILY_BUDGET` | yes |
| plan consent | `CODEAF_PLAN_CONSENT` | yes |
| practice carve-out | `CODEAF_PRACTICE_BUDGET` | yes |
| session ceiling | — | yes (also per-project) |
| lifted-tier cap | `CODEAF_RESPONSE_LIFT_CAP` | **no** — plumbing, turned when a provider misbehaves or a run is held to a price |
| standing per-firing | — | `per_run_usd` on the `stand` tool, per item |
| adaptive-run tank | — | the composer's third line, per run |
| unattended budget | `CODEAF_MAX_COST` / `CODEAF_MAX_HOURS` | no — flags |
| per-task limit | — | `/crew cap task` (`models.crew.task_cap`) |
| crew daily cap | — | `/crew cap` (`models.crew.cap`) |

## One source of truth

Every figure above appears **once**, in the constant named in the table, and is
interpolated everywhere it is shown: `codeaf --help`'s environment table, the
first-run ceiling screen, the `stand` tool's own JSON schema, the settings
rows. Three of them used to be written out separately —
`standing.DefaultPerRunUSD` was a literal `0.15` in the store, in the head's
proposal and in the belt tool, each with a comment asking the next person to
remember the other two — and `session.DefaultRunCapUSD` was spelled twice, in
the engine and in the composer. Changing a rail is now one edit, and this page.

## Still to come

The right answer is not a large default at all: it is **asking**. See
[issue #83](https://github.com/Agent-Field/codeaf/issues/83) — "Onboarding asks
for spend limits (daily, per plan, per task, standing) with 'no limit' as a
first-class option". The large defaults above are what keeps nobody
blocked in the meantime.
