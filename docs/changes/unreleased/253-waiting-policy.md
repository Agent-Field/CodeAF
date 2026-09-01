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
  - "The store was one file merged last-writer-wins. It is `lanes.json` plus an append-only `lanes.log`: `Store.Load` is the compacted half and the journal is replayed over it, so two processes sharing a ledger no longer discard each other's afternoon."
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

**And a sheet row is an offset.** It was shared out among the levels it may move
at each one's Kalman gain, and `μ` holds most of the variance and may not move,
so a pair whose only evidence was one published row was believed between the
world's pace and the published one: a lane the sheet put at 430 ms read back as
about 1.2 s. `e[model, lane]` takes the whole difference now and the prediction
lands where the sheet put it.

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
