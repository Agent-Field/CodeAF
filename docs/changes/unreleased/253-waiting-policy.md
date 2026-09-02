---
kind: changed
title: nobody waits long — one hazard controller on every call, and a wait that says why
pr: 253
surface: [chat, engine, docs]
invalidates:
  - "`lane.Choice` carried `Deadline` and `Alt`, so a routing answer and a waiting answer shared one nil and a ledger with no opinion produced no clock at all. Both fields are gone: `lane.PlanFor(choice, lane.Pace, lane.Role, now)` builds a `control.Plan` for every token-generating call, whether or not anything was chosen, and the plan is what carries the ceiling, the floor and the alternatives."
  - "`lane.PlanFor` took a `lane.Belief`. It takes a `lane.Pace` — the two survival distributions a wait is judged against — built either by `lane.PaceOf(belief)` or by `lane.PaceFor(id, now)`, which reads the four-level hierarchy where there is one and the flat belief otherwise."
  - "Waiting was decided by absolutes: `deadlineCeiling` 8s, `lumpGap` 15s, `commitTokens` 64 visible tokens, one hedge per request, and `choose.go`'s own `hedgeTime`/`hedgeFloor`/`hedgeCeiling`. Every one of them is deleted. What decides now is `internal/lane/control`: act when the expected remaining wait exceeds what acting costs, and at the role's ceiling whatever the arithmetic says. `lane.VisiblePatience` 10s times the role's `Patience` is the only absolute left."
  - "`internal/provider` kept its own copies of the plan arithmetic while the lanes were built in parallel — `lanePlan`, `laneAlternatives`, `expectedIn`, `laneSurvival`, `predictiveSpreadFloor`, `waitHysteresis`, its own `purse` and its own `headLane`. All seven are gone; the names in `internal/lane` are the only ones. `lane.SpreadFloor`, `lane.Hysteresis`, `lane.HeadOf`, `lane.Spending` and `lane.DeadPathFloor` are exported for that reason."
  - "A wait that reached the ceiling with an affordable alternative was turned from a report into a hedge by `hedge.go`, downstream of the verdict. That ruling is the controller's now, so a row that says `report` never put a request on the wire — and an unattended role, whose λ is zero by construction, is still rescued at its own ceiling rather than merely told about."
  - "A strict lane pin sent `Only` and carried no candidate set, so the offer a stalled pin raises had nowhere to point and the wait was reported instead. A strict pin now carries the frontier — computed exactly as for any other call, sent to nobody — which is what makes `coreweave is slow · switch to auto? (y)` answerable."
  - "The transport's stall bounds were flat: 90s to the first delta, 45s mid-stream, 150s buffered. They pre-empted the 30s and 60s ceilings of `leaf.unattended`, `memory`, `auxiliary`, `standing`, `judge` and `design`, so for most of this build's calls the first thing to act on a stall was the one act that throws the whole attempt away. They are scaled by the role's `Patience` now and floored at twice its ceiling; `docs/ARCHITECTURE.md` Decision 10 rows 10–12 say so, and a law walks every role at every rate."
  - "`lane.Chain.Survival` took the flat `lane.SpreadFloor` as its floor and every caller passed it, so one figure stood under every lane. It takes that lane's OWN published dispersion — `lane.Hierarchy.Draw(id)`, the distance between the sheet's p50 and p90 for the pair — and `lane.SpreadFloor` is now only the prior for a pair nothing has been published about. A ledger that answers `lane.Hierarchy` has one more method to answer."
  - "A sheet row was folded into `b[model]` and `e[model, lane]` at each level's Kalman gain, so a pair whose only evidence was one published row was believed BETWEEN the world's pace and the published one — a lane the sheet put at 430 ms read back as about 1.2 s. A row is an offset, so `e[model, lane]` takes the whole difference from its parents and the prediction lands on the published number; `b[model]` is no longer moved by a row at all. The variances still shrink by each level's own gain: a half-hour aggregate says where a lane sits, not how sure to be."
  - "`lane.SetController` returned nothing, so a caller that swapped it put back nil and left the process with no waiting policy. It returns the previous factory, the way `provider.OnPhase` does."
  - "The store was one file merged last-writer-wins. It is `lanes.json` plus an append-only `lanes.log`: `Store.Load` is the compacted half and the journal is replayed over it, so two processes sharing a ledger no longer discard each other's afternoon. **The per-`Note` save is gone; the journal compaction still takes the exclusive lock on the send path** — `ledger.keep` calls `compact` on the first record of a process and every `journalLimit`th (512) after, and `internal/provider`'s stream loop calls `lane.NoteThought` mid-answer, on the first visible word after a run of thought (`client.go`, \"THE FIRST WORD OF ANSWER IS WHAT ENDS A THOUGHT\"), so one request in 512 and the first of every process can block on a file lock with a person watching. Issue #264 remains open for the off-path writer and the non-blocking lock; `keep`'s own comment said \"never on a send path\" and has been corrected to say what the code does."
  - "`internal/session` spelled `PhaseAsking` and `PhaseAllSlow` as its own string constants. They are `provider.PhaseAsking` and `provider.PhaseAllSlow` — one closed vocabulary — and `session.SetOfferAnswerer` is wired to `provider.AnswerOffer` at agent construction, so the `y` key reaches the request that asked."
---

The reported defect was a turn that waited three minutes with no clock of any
kind on it, and the cause was one shape repeated five times: an absolute where a
belief belongs, and a belief where an invariant belongs. A cold ledger switched
the deadline off; a reasoning delta counted as a first token and left the only
branch that had one; a pinned lane that went quiet said nothing at all.

`docs/design/waiting/DESIGN.md` is the argument and it is unchanged by the
build. Its "Laws currently red" table now lists fourteen laws and no red ones.

Two deviations from the design landed deliberately and are follow-ups rather
than defects. **A v1 `lanes.json` is folded in as pseudo-observations** rather
than read straight into the pair level of a fresh hierarchy, as §C describes:
the beliefs survive and nothing is claimed that was not measured, but the
migrated variances are the fold's rather than the file's. And **§E's "ask once
whether the pin should borrow in future" is not built** — accepting an offer
rescues this answer and writes no `lane.<slot>.borrow` row, so a person who
wants a borrowable pin still sets it themselves in the picker.

**And the design's own acceptance, §K, is measured and two of its four criteria
still fail.** `bench/lanelab/REPORT.md` has the runs — five rows, three seeds,
4,500 trials against the shipped chooser, ledger and controller. The invariant
the work was ordered for holds: every one of 225 staged silences on a cold store
was acted on at the ceiling to the centisecond, and 98% of legitimate thinking
phases were left alone. What fails is cost, and this PR halves it.

**The floor under the predictive spread is a lane's own variability, and one
figure for every lane was not it.** `lane.SpreadFloor` is 1.0 nat; the lane §K's
rows are proved against publishes a p50 of 430 ms and a p90 of 900 ms, which is
σ = 0.577. Floored at one nat its `W(0.7 s)` is 0.876 s where its own spread
gives 0.305 s, and acting costs about 0.71 s — so a perfectly healthy request
wanted a second one at the first instant an act was legal, on every request that
had not started by then. The sheet publishes each lane's dispersion and this
build already keeps it per pair, because it is the same number a sighting is
weighed against as observation noise. So THAT is the floor wherever there is
one, and the constant is the prior for where there is not: a measured thing
outranks a prior, which is the law the rest of the package keeps everywhere.

**And the two silences are two quantities.** The clock that guards the answer
reads the silence a person is waiting through and the clock that guards the wire
reads the time since the endpoint last wrote anything at all — so a lane that
has written three words and is now reasoning is no longer read as a stall.

**And a refused path is told apart from a slow one at the report level**
(`HedgeReport.PathFault`), so the two are already separate quantities where a
caller reads them. The surface word for a refused lane is not built here — it
belongs beside `PhaseAsking`, and it is issue #266.

**And a sheet row is an offset.** It was shared out among the levels it may move
at each one's Kalman gain, and `μ` holds most of the variance and may not move,
so a pair whose only evidence was one published row was believed between the
world's pace and the published one: a lane the sheet put at 430 ms read back as
about 1.2 s. `e[model, lane]` takes the whole difference now and the prediction
lands where the sheet put it.

**§K's four bounds were then re-measured over a WARMED store, and three of them
fail there too.** The ruling was that a cost bound describes a steady state and
that the remaining failures were cold-start exploration the purse bounds, so
`bench/lanelab/gosim` gained a store axis: the cold arms enforce the ceiling,
the long think and the purse's own ceiling on the bill and REPORT the two cost
figures; a warmed arm — the sheet, plus sixty real answers of every pair folded
through the real `lane.Ledger.Note` door in a home of the run's own — enforces
all four. On the door the transport asks, the warmed arm measured
**time-to-action 100.00% of 225 acts at a maximum of 10.00 s (PASS), false
hedges 2.84% against 2% (FAIL), spend 5.56% against 3% (FAIL), long think 93.21%
of 162 phases against 95% (FAIL)** — every cost figure WORSE than the cold arm's
2.49% / 4.64% / 96.77%. Warming the store does not move these bounds into range;
it moves them the wrong way, because a cold store's low arm count is partly an
inability to hedge — the silent row raises 0 arms cold and 29 warmed, having
gained a frontier to point at. `REPORT.md` carries the four arms and the rows.
Nothing was tuned in response and `docs/design/waiting/DESIGN.md` §K is
unchanged: the ruling's premise is not what the measurement says, and the
decision is the owner's.

**A named limitation, and it is not a footnote — filed as issue #316. The wire
clocks warm; the permanent part is only that a legitimate long think cannot be
told from a stall by DURATION.** The first-token and gap clocks sharpen with
evidence as designed, because the sheet publishes a dispersion each estimate can
beat. For the duration clock there is no warm-up period. §B's abnormality test compares a wait against the quantile
of the survival its clock reads. A thinking phase has no published dispersion,
so `Chain.Survival` floors that survival's spread at `lane.SpreadFloor` and the
larger of the two is always the floor — **σ never falls below 1.0 nat however
many thoughts are folded in**, measured at 0, 1, 2, 5, 10, 20 and 60
observations. The quantile settles at about fifteen times the believed median:
**82.9 s against a 10 s ceiling**. So the duration clock can act before the
ceiling only for a model believed to think for under **0.663 s**, and **a person
using any model that deliberates for longer is in that one clock's ungated
regime on their first answer and on their ten-thousandth — only the role's
ceiling protects a long think.** Swept in the rig as well as read off the code: the long-think rate
at n = 0, 1, 2, 5, 10, 20, 60 reads 96.85 / 94.64 / 96.85 / 97.74 / 95.41 /
95.48 / 93.72 — no trend, the largest n the lowest reading.

The follow-up that would shrink it, named: **seed the think chain's spread from
the hierarchy's own prior** so the estimate can beat the floor the way the
first-token and gap clocks already do, or **give the duration clock a drift
quantile of its own** — a stopped thought judged against the gap between
reasoning deltas rather than against the whole phase. Both are mechanism changes
and neither is taken here; **issue #316** carries both, the runnable
replication, and the acceptance line — which includes recovering the 4.95 s p50
action on stalled thought this build trades to 10.00 s on 0.93% of requests.
`TestWhenTheThinkGateCloses` fails the build if the floor ever stops binding, so
the limitation cannot go stale unnoticed. The
long-think criterion is gated at full force on warmed arms for this reason and
reported on cold ones, where what it counts is the world's own think-tail rather
than anything a controller decides.

**HALF OF THAT LIMITATION IS NOW STALE — #316 closed the gate.** The think chain
measures its own dispersion from the thoughts it is shown, `σ` falls below the
floor with evidence, and the gate closes at 31 observations of one model at one
rung; `TestWhenTheThinkGateCloses` asserts that n rather than its absence. What
stands is the other half: the duration clock's payoff term prices leaving a
thought at a whole fresh thought, so it still cannot act before a ten-second
ceiling on a model with a five-second median, and the **4.95 s p50 named above
is not recovered by that close**. #316's second candidate — a think-phase drift
quantile — is what would recover it and it is still open.

**A rescue is a price, not a floor, and the baseline arm is what says so.** An
earlier reading of this lab argued that §K's spend clause could not be met
because a rescue is a whole second request. That was an argument; the
measurement refutes it. `bench/lanelab/gosim` now runs an arm with **no waiting
policy at all** — the transport's own guard and a serial re-send when it fires —
and on a warmed store at a 5% fault rate it answers the same workload for
**$1.3930 / $1.4194** against the policy's **$1.3725 / $1.4812** on the two seed
sets, and takes **20.15 s / 20.13 s** at the median to get past a broken request
where the policy takes **10.76 s / 10.18 s** (30.95/30.49 against 20.18/20.22 at
the ninetieth). Overall per-request waits are indistinguishable at that fault
rate — 0.92/10.53 against 0.91/10.75. So what the premium buys is **latency on
the requests that go wrong**, at a bill that moves between 1.5% cheaper and 4.4%
dearer depending on the seed and the door, well inside the purse's own tenth.
The spend clause on the TOTAL is reported with that comparison beside it; what
is gated is the **avoidable** half — arms on healthy requests, and rescue arms
that lost to the lane they were rescuing — at **0.90%**, which is the measured
maximum across both seed sets and both doors (0.5973%) times a half again.

**And the warmed failure was a controller defect, which is now fixed and
measured.** The hazard inequality alone answered two questions — *does acting
pay* and *is the lane misbehaving* — and with a tight, correct belief it
rationally hedges healthy draws. An act before the ceiling now requires both the
payoff crossing AND an abnormality test: the wait is past the `1 − p` quantile
of the very survival that clock reads, first token against the first-token
survival, drift against the gap survival, the duration clock against the think
survival. **`p` is derived from §K rather than chosen** — under the null each
alarm opportunity exceeds its own quantile with probability `p`, a request
offers `k` of them, the union bound puts the per-request false-act rate at
`k · p`, and §K already fixes that at 2%, so `p = 0.02 / k` with `k` counted from
the request's own shape. `DESIGN.md` §B carries the derivation. The ceiling is
untouched and absolute, so time-to-action cannot regress, and a survival nobody
measured has no quantile — the gate stands open exactly where the payoff test is
already closed.

**Measured on two seed sets, 18,000 trials: three of the four bounds pass and
spend does not.** On warmed · shipped: time-to-action 100.00% of 225 acts at a
maximum of 10.00 s (PASS), false hedges **0.62%** familiar / **0.80%** held out
against 2% (PASS), long think **98.21%** / **97.77%** against 95% (PASS), spend
**4.45%** / **3.78%** against 3% (FAIL). The gate cut false hedges from 2.84% and
lifted long think from 93.21%, and the invariant did not move on any of the
eight arms. **The residue is no longer false hedging**: of 103 arms on that arm,
seven were on healthy requests and ninety-six were rescues of genuinely staged
stalls, so what spend is measuring on a workload that is half staged faults is
mostly the cost of the mechanism working. That is a question about the
acceptance criterion and it is the owner's; nothing was tuned, and §K's gate
shape is not written into the design while its load-bearing arm is red.

Measured, on the shipped door: false hedges fall from 5.3% to 2.9% of healthy
requests and the spend overhead from 5.5% to 4.8% of the bill, against
thresholds of 2% and 3%. Neither gate closes. `REPORT.md` carries the frontier —
every candidate against all four gates — and where the residue is: one row of
the five, and the remaining cause is `SheetWeight` itself. One row now centres a
belief on the published number and still says nothing about how sure to be of
it, so the spread a fresh pair is waited against is the four level priors summed
and the lane's own dispersion does not become the floor until the pair has been
measured. That is a number §C fixes as a prior and the ruling on it is the
owner's.
