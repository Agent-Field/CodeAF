---
kind: fixed
title: a stream that trickles is hedged or cut on a named reason, not left to the wall
pr: 636
surface: [chat, engine]
invalidates:
  - "The first review counted one streamed event as one token. Providers may batch many tokens in an event; the rate reading now divides the elapsed interval by its visible token count, so a healthy 20-token/s stream delivered ten tokens at a time keeps progress credit."
  - "`control.Plan.Ceiling` was believed to be the hard bound on time-to-action for every call, and `lane.VisiblePatience` to be the longest a person is asked to wait before something is done. Neither held for a stream that kept writing slowly: `hazard.Note` moved `progress` to now on every reading with a visible token, so one token every couple of seconds reset the ceiling's own clock and `silence >= Ceiling` was never reached. A visible token resets it now only while the stream is keeping up."
  - "The drift clock was believed to catch a lane running below its believed rate. It asks each gap in isolation against that gap's own quantile, and a run of gaps each three times the believed median — a lane at a third of its rate — is an ordinary draw every single time. The gaps are accumulated now: each contributes `(ln g − Gap.Mu) / Gap.Sigma`, the sum and its weight decay by `exp(−g / Ceiling)`, and `drift / √weight` is compared against the same threshold `falseActBudget` already derives. At one gap it is arithmetically the single-draw test, so nothing is caught later than it was."
  - "PR #622's `rescueOnStall` read as the rescue for a stalled stream generally. It hangs off a `control.Report` verdict and rescues a MISSING first token or a stream that stopped; for a stream whose rate collapsed after it began the controller raised no verdict at all, so nothing reached it. That case is the ceiling's now."
  - "`control.CeilingReason` was the only word a bound-raised act carried. `control.RateReason` (`rate collapsed`) is the word when the ceiling was reached because the stream stopped earning progress rather than because it fell silent, and it is on the call row."
  - "A hedge the race refused left no trace anywhere: `hedgeRace.hedge` returned silently when `claim` found nowhere untried and again when the allowance said no, so a call whose rescue was refused was indistinguishable from a call nothing had to be done about. `claim` answers the reason — `budget`, `no alt`, `no room` — the race keeps the first, and it is on the row as `refused`."
  - "The model-call row carried the controller's action and never its reason: `waitFacts` copied `action`, `wait` and `cost` off the act and dropped the one word that says which clock decided. `calllog.Record` has `Reason` beside `Action` now, and `aforge logs` prints both — `acted hedge · rate collapsed · no rescue: budget` — and prints nothing at all on the rows that have neither."
  - "`internal/manual/chat/lanes.md` said `slow` meant a machine answering and taking its time, and had nothing about a stream that never stopped and merely crawled. It now has a section in the words people ask it in, including the limit: a lane whose visible rate has never been measured cannot be judged this way and is bounded only by the ceiling on silence."
---

The reported run lost five minutes to two streams that trickled for two and a
half minutes each. Rungs one to four stayed silent because the stream was
writing; the ceiling stayed silent because each token reset it; and the
transport's overrun wall at five times the lane's longest finished reply was the
only thing left. That is one shape said twice — an absolute defeated by a
technicality, and evidence that is decisive in aggregate read one draw at a
time.

The accumulator gates the CREDIT and never an act. It raises no verdict and
enters no payoff arithmetic; all it decides is whether a visible token still
counts as progress toward the ceiling, and the ceiling — which is ungated by
both the payoff test and the abnormality test — is what acts. So the only
behaviour that moves is the one `Plan.Ceiling` already promised, and the two
existing false-act guards in `internal/lane/control`, 500 and 2,000 streams
against §K's 2% budget, are unchanged and green.

The decay horizon is the ceiling's own, which is what makes the claim current
rather than historical: a bad patch is forgotten on the same horizon as the
bound it can stop resetting, and a stream that recovers sheds it.

Two limits, so nobody reads more into this than it says. **It was not measured
against a live model** — the acceptance is scripted, in `internal/lane/control`
against a scripted belief and at the wire against `lanestub`, and the issue's
own end-to-end re-run is not repeated here. And **the `Hedge`-verdict half of
the refusal record is written but not proved**: a race-level `budget` refusal on
a `Hedge` verdict needs the allowance really consumed between the controller's
affordability read and the race's spend, so the test proves that word through
`rescueOnStall`, which the front door reaches.

Two other halves of #215 were closed elsewhere and are not touched here: the
split ledger identity by #300 and #353, and a missing first token by #622. The
issue's "hedge budget drained by sub-second rescues" half was closed by #253's
`lane.ActionFloor` and abnormality gate; it is named in the contract here so it
cannot regress unnoticed.
