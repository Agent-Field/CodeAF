# Recovery — one controller for every failed call

*Written 2026-09-10 after a task hung for three minutes on `waiting · rate limited`
while nine identical requests went to one rate-limited machine. This is the fourth
wave on this family in two weeks (#793 one refusal door, #794 the hop verdict,
#786 the stream wall, #835 recovery owns exhaustion). Each closed the defect it
was opened for and each left the shape that produces the next one. This document
is about the shape.*

The waiting design (`docs/design/waiting/DESIGN.md`) answered one question with
one controller: **when** to act on a call that is slow. This document is its
sibling for the other question, which today is answered by eleven controllers:
**what** to do when a call has failed.

## 1. What the log says

Every model call this build makes writes a row to `~/.aforge/logs/calls.jsonl`.
Over the ten days 2026-08-31 → 2026-09-10 that is 16,370 finished attempts. The
census (`scripts/callcensus`, §8) says:

| fact | number |
| --- | --- |
| attempts that came back `200` | 87.7 % |
| attempts that came back `200` **and were a clean answer** | **76.1 %** |
| failures where the cause is the client cancelling itself (hedge losers, walls, deadlines) | **57 %** |
| failures where the cause is a provider `429` | 30 % |
| failures where the cause is the network on this laptop (DNS, no route, reset) | 7 % |
| failures where the cause is an account/routing `404` | 4 % |
| failures where the cause is a provider `5xx` (always inside an open `200` stream) | 2 % |
| retry chains that **never left the lane they started on** | **53 %** |
| deepest chain of identical requests to one machine | **17** (DeepInfra ×17, twice; Reka ×16) |
| retry chains that ended in a clean answer | 23 % |
| hedged calls that ended clean | 22 % |
| rows where the `lane` we chose and the machine that `served` disagree | **37 %** |
| rows where the `(via X)` in the error contradicts the `lane` field | 32 % of attributed errors |
| failed attempts that recorded their cost | **0 of 1,887** |
| `Retry-After` recorded | never |

Three readings matter more than the rest.

**First, most of our failures are ours.** 869 rows are `decode stream: context
deadline exceeded` with `ms` clustered at exactly 90,000 and 60,000, and 780 of
those had already received a first token — a live stream, guillotined. 413 of
them are one lane, OpenInference on `deepseek-v4-flash-0731`. Add the 715
`context canceled` rows that are hedge losers and the 111 walls, and the
provider is the minority cause of what we record as failure.

**Second, a retry today means "again", not "differently".** The Fireworks chains
that eventually answered did so on the one attempt that finally moved to
DeepInfra — after 7, 10, 13 and 14 identical sends. The ladder works; it engages
after the person has given up.

**Third, the ledger is keyed on a lie.** Every per-lane belief (`velocity`,
strikes, pacing, the sheet's uptime) is written against `lane`, and on a third
of the rows that is not the machine that answered. A lane can be struck for
another machine's 429.

The screenshot that opened this is all three at once, on a pre-#835 binary:
DeepInfra `404` (account policy — #835 now learns it once), Io Net `429` inside
an open stream after 21 s of silence, then Fireworks `429` × 9 with the same
bytes, because `retry.go` encodes the body before the attempt loop and
`velocity.go:805` says so out loud: *"the retries of the call that drew the 429
still wait it out, because their body is already written."* Meanwhile GMICloud
— which had served the three previous tool calls of the same task in six seconds
each — was not a candidate, because the fetched sheet marks it `Tools:false` and
`frontier.go:118` gates it out of every tool-carrying request for good.

## 2. What the code is

Eleven things decide what happens after a call fails or slows, on one request
path. Each owns a budget nobody else can see.

| # | controller | where | owns | budget |
| --- | --- | --- | --- | --- |
| 1 | AIMD limiter | `provider/limiter.go:83` | process-wide in-flight slots | 1–64 slots, halved on every 429, no phase |
| 2 | attempt loop | `provider/retry.go:91` | HTTP attempts, 429 pacing | 3 faults; 6 or 60 paced; 2 m or 10 m |
| 3 | connectivity | `provider/connectivity.go:84` | is the origin there | 2 m |
| 4 | stall watch | `provider/streamguard.go:752` | silence bounds | 90 s / 45 s / 150 s × role |
| 5 | stream wall | `provider/streamguard.go:414` | whole-reply duration | 5 m … 20 m |
| 6 | hazard controller | `lane/control/hazard.go:469` | *when* to act on slow | role ceiling 5–60 s |
| 7 | hedge race | `provider/hedge.go:279` | arms, cancel, voice, exhaustion | 4 arms + 1 ladder arm; purse 2 per 20 |
| 8 | refusal door | `provider/velocity.go:1001` | ledger writes | 3 strikes, 5 m cooldown, 24 h account |
| 9 | relaxation ladder + `fallbackChain` | `provider/endpoints.go:501` | request shape, then **model** | 7 rungs + 2 models |
| 10 | turn ladder | `session/loop.go:1215` | retry / hop / give up | 4 attempts × 3 models |
| 11 | errand ladder | `session/auxiliary.go:194` | role rungs | 1 try per rung, 2 rungs |

Each is well-written and tested in isolation. The problems are all between them.

1. **No shared budget.** Attempts (`retry.go`), attempts (`taxonomy`), arms,
   rungs, models and patience each bound a different axis. Their product —
   3 models × 4 attempts × 5 arms × 6 paced sends × 9 rungs — is nobody's number,
   and the time caps (2 m pacing, 20 m wall, 60 m task, 45 s–10 m role) compose
   without coordinating.
2. **Two model-hop mechanisms** draw from the same `FallbackModels`:
   `endpoints.go:560` inside the adapter and `loop.go:1427` in the session.
   Neither knows the other has already tried a model.
3. **A retry re-sends the same bytes to the same machine.** The body is encoded
   once, above the loop; no attempt can exclude the lane that just refused it.
4. **The walk is a prediction.** `canWalk` (`hedge.go:1307`, `client.go:582`:
   *"a PREDICTION that the walk will carry this refusal; the walk can still
   decline"*). #835 added `exhausted()` to catch the miss, so the prediction and
   its repair now both exist.
5. **A prose regex overrides the verdict.** `loop.go:1382`
   `!isRetryable(errMsg)` returns before the `taxonomy` switch computed forty
   lines earlier — the exact bug class `Evidence.Routing` had to be added to
   work around.
6. **A 404 is classified in three places** with three rules —
   `taxonomy/taxonomy.go:386`, `provider/refusalobject.go:64`,
   `session/taxonomy_boundary.go:407`.
7. **Two `Verdict` types**, `provider/verdict.go:15` (learning) and
   `taxonomy/taxonomy.go:262` (control), one name, in packages that call each
   other.
8. **Four waits are silent.** `sharedLimiter.acquire` (`retry.go:205`), the
   empty-200 ladder's `backoffWait` (`loop.go:755`, no `EventRetrying`),
   `abandonGrace` (`hedge.go:377`), and the connectivity probe before its first
   phase. The waiting design's law — *a wait that is real is reported* — has
   four exceptions.
9. **The refusal door is configuration-gated off.** `velocity.go:1002`,
   `:811`, `:832`: with routing off or no preferences carried, nothing paces,
   strikes or learns, while every other controller keeps acting.
10. **`Facts.Tools` is a one-way gate.** `frontier.go:118` removes a
    `Tools:false` lane from every tool request permanently, on a router flag
    this codebase has already measured to be wrong (`quirks.go:20`, MiniMax
    M2.7). The opposite error — a lane that claims tools and emits bad JSON — is
    caught by the quality ledger. The two directions have no symmetry.
11. **A cut that named no server re-asks the same lane.** `streamguard.go:563`
    `Rerouted=false` says it honestly; `taxonomy` answers by shortening the
    allowance (`BlindCutAttempts=2`), not by forcing a different lane.
12. **Failures are unpriced and misattributed.** No failed row carries cost or
    tokens; `lane` ≠ `served` on a third of rows; `cost_s` reaches 1e146 and the
    belief file has been refusing to compact on `NaN` for days
    (`the belief file could not be compacted: json: unsupported value: NaN`).

## 3. The rule

The owner asked whether we should *catch any error and retry irrespective of the
error*. Half of that is right and is the rule; the other half is what we do now
and is why chains reach seventeen.

> **Every failed call gets a next move, and the next move is never the last
> one.** A move is a different machine, a different shape, or a different
> model — in that order — chosen by one controller that owns the whole budget
> for the call, records every outcome against the machine that actually
> answered, and says out loud what it is doing while it does it. The same bytes
> go to the same machine a second time only when there is nowhere else to send
> them, and then only for as long as that machine itself asked.

Five clauses, each of which some controller above breaks today.

1. **One budget, one clock, one owner.** A call carries one `Plan`: a deadline
   in the person's time (the role's patience), a spend cap, and the set of
   moves already made. Every controller in §2 becomes a *move generator* under
   that plan or a *watcher* that feeds it. Nothing below the plan owns a retry
   count.
2. **Never repeat.** A move is a (machine, shape, model) triple and the plan
   refuses one it has already made. The body is encoded per move, with the
   refusing machines excluded and `Retry-After` honoured only when the pool is
   one machine wide.
3. **Facts, not predictions.** A handoff is a commitment: whoever takes a
   failure owns it until it is answered or the plan is spent. There is no
   `canWalk`; there is `walk`, which either returns an answer or returns the
   failure with its own moves appended.
4. **Every wait is narrated and every outcome is written down** — against the
   machine that served, with its cost, through the one door #793 built. No
   `select` on a timer in the request path without a `notePhase`; no
   configuration that turns the ledger off.
5. **Some errors are answers.** A malformed request from our own bytes, a
   context overflow, an account exclusion, a model the router withdrew: these
   are not retried, they are *changed* (shape, model, or the person is told)
   and the change is the move. This is why "irrespective of the error" is
   wrong: the verdict still matters, but its only job is to pick the move.

## 4. The shape

```
                    ┌──────────── Plan ─────────────┐
   request ───────► │ deadline · usd · moves made   │
                    └───────────────┬───────────────┘
                                    │
                         ┌──────────▼──────────┐
                         │      Dispatcher     │  one loop, in provider
                         │  move ← next(plan)  │
                         │  result ← send(move)│──► watchers: hazard (when to act),
                         │  verdict ← classify │       silence, wall, connectivity
                         │  ledger.write       │──► ONE door (#793), keyed on served
                         │  narrate            │──► ONE phase pipe (notePhase)
                         │  answered? return   │
                         └──────────┬──────────┘
                                    │ spent
                                    ▼
                    session: hop model (the ONLY model hop) or end the turn
```

**`Plan`** (`internal/lane/control`, beside the hazard's `Plan` — they are one
type once this lands): `Deadline`, `SpendUSD`, `Moves []Move`, `Role`.

**`Move`**: `{Model, Lane, Shape}` where `Shape` is today's relaxation rung set
(price ceiling, reasoning, max tokens, response format, images, tools). Plus a
`Wait time.Duration` for the one legal same-machine move.

**`next(plan, history)`** is a pure function, tested on a table, and its order
is the rule's order:

1. another machine in the serving set, best belief first (today's chooser);
2. the same machine after its own `Retry-After`, **only** if it is the last one;
3. the same machines with a relaxed shape (today's ladder, one rung);
4. `none` — the plan is spent; the session hops the model or ends.

A hedge is not a controller here: it is the dispatcher launching `next()` a
second time while the first move is still in flight, when the hazard says the
silence is worth acting on. The arms share the plan's moves, so two arms can
never demand the same machine, and the purse stays what the waiting design
says it is.

**`classify`** is `taxonomy.Classify` and nothing else. `provider/verdict.go`'s
type is renamed (it is a *learning* record, `lane.Reading` or similar) and the
three 404 rules collapse into `Evidence`: `Status`, `Upstream`, `Routing`,
`Account`, `Malformed`. `isRetryable(errMsg)` is deleted; a string never decides.

**`ledger.write`** is `refuseLane` plus a success row, called on **every**
outcome, keyed on `served` (from the stream's own `provider` field, or the
demanded machine when `Only` was one lane), never on `lane`. Cost and tokens
are recorded on failures too — the stream was paid for. The three configuration
gates go; a base that is not a router gets a ledger with one row, not no ledger.

**`narrate`** is `notePhase` on every wait, including the limiter's slot wait
and the empty-200 re-ask, and one `RetryNews` per move so the surface draws
`trying another machine · 2 of 5` from one composer (`tui3/failurerow.go`),
not from the status line in one place and the feed in another.

**Lane facts are priors, not gates.** `Tools`, `Uptime5m` and `Status` from the
sheet seed the belief and decay like every other belief in the hierarchy
(`docs/design/waiting/DESIGN.md` §C). A `Tools:false` lane is demoted, not
removed, and one probe in every N tool requests (N from the same purse the
hedge uses) is allowed to disprove it. GMICloud serving three tool calls in a
row while flagged `Tools:false` is the measured case.

**What the session keeps.** `loop.go` keeps exactly one decision: when the
dispatcher returns *spent*, hop to the next model in `FallbackModels` or end the
turn with the most actionable error. `endpoints.go`'s `fallbackChain` walk is
deleted — the adapter never changes the model. The task node's retarget seam
and the errand ladder call the same dispatcher with their own `Plan` (a task
node's patience is long; an errand's is its rung) and stop owning attempts.

## 5. What the person sees

The person's three questions, and the one answer each gets:

| question | today | after |
| --- | --- | --- |
| is it moving? | `waiting · rate limited` for minutes with no count | `trying another machine · 2 of 5`, the count real, the phase pipe the same one the waiting HUD uses |
| why did it stop? | a raw `API error (404): 0 endpoints…` or a turn that ended | never a raw router sentence; a turn ends only when the plan is spent across every model, and the failure row names the last move |
| how long? | unknowable; budgets multiply | the plan's deadline is the role's patience — talk 10 s to first action, 2 m to give up; the countdown is drawn from the plan, not from a backoff |

And one law from the census, so the biggest failure class shrinks rather than
being handled better: **a stream that is producing tokens is not cut for
elapsed time.** The wall bounds silence relative to the lane's own measured rate
(`gapFor` already does this); the 60 s/90 s duration deadlines that guillotined
780 live streams are removed, and the 20 m absolute ceiling (#786) is the only
duration bound.

## 6. What is deliberately not here

- **No new lane table, price list or provider order.** Beliefs stay where the
  waiting design put them; this changes who writes them and what key they use.
- **No change to what a 429 *means*.** It is still the provider saying "not
  yet"; what changes is that "not yet" from one machine sends the next move to
  another.
- **No diurnal or cross-process pacing.** Measured to matter less than the
  same-lane replay by an order of magnitude.
- **Not a rewrite of the hazard controller.** It stays the watcher that says
  *when*; this is the controller that says *what*.

## 7. Build plan

Five waves, each landing green on `dev` and each reducing a number in §1. The
first is measurement, because two of the twelve problems are that we cannot see.
Every wave runs its suites on the Spark, never on the laptop.

| wave | lands | number it moves |
| --- | --- | --- |
| **R0 see** | `scripts/callcensus` committed and run nightly on Spark against `~/.aforge/logs/calls.jsonl` (a `bench/` target); rows record `served` on every finish, cost and tokens on failures, `retry_after`, and an honest `deadline_ms`; `cost_s` NaN/Inf fixed at the source and the belief file compacts again | the table in §1 becomes a nightly metric |
| **R1 never repeat** | body encoded per move; refusing machine excluded on the next; `Retry-After` honoured only when alone; ledger keyed on `served`; `Tools`/`Uptime`/`Status` demoted to priors with a probe | same-lane chain share 53 % → < 5 %; max identical sends 17 → 2 |
| **R2 one classifier** | one `Evidence` for the three 404 rules; `isRetryable` regex deleted; `provider/verdict.go` renamed out of the way; `canWalk` → `walk` as a commitment, `exhausted()` deleted | the three-classifier bug class cannot recur; law test: one `Classify` call site per package |
| **R3 one budget** | `Plan` shared by hazard and dispatcher; `retry.go`'s loop, the hedge race and the relaxation ladder become `next()`; `fallbackChain` model hop removed from the adapter; task node and errand call the dispatcher | attempts × arms × rungs → one deadline; task never dies on the wire |
| **R4 every wait spoken, no self-cuts** | limiter, empty-200, abandon grace, connectivity on `notePhase`; the three configuration gates on the door removed; duration deadlines removed in favour of rate-relative silence; hedge losers logged as `exhaust`, not failure | self-inflicted share 57 % → < 15 %; zero silent waits (law test) |

Acceptance is end-to-end first, in `lanestub`, which #835 taught to stage a
paced pool, an account exclusion and a full pool. Each of the top ten error
signatures in the census becomes one staged scenario and one assertion on the
call log: *how many sends, to how many distinct machines, in how long, and what
the person read*. The screenshot scenario — policy 404, in-band 429 after 21 s,
then a 429 pool — must answer through the fourth machine in under fifteen
seconds with a `2 of N` line on screen and no `API error` string anywhere.

## 8. The census

`scripts/callcensus` reads `calls.jsonl` and prints §1's table. It groups by a
normalised error signature (model, lane, numbers and ids stripped), reconstructs
chains as (tag, node, model, rising attempt, ≤ 15 min gap), and reports lane
health as clean-200 rate and median latency per (model, served) for the last
three days. It is the instrument; without it every one of these waves is an
argument about anecdotes. The first run's full output is beside this file as
`census-20260910.md`.
