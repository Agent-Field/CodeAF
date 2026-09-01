# Waiting — nobody waits long, and the build always knows why

*Date: 2026-09-01. Status: design → build. Lives at `docs/design/waiting/DESIGN.md`.
Written against `dev@97724633` and the audit of the three-minute turn that
prompted it. Replaces rows 1–6 of `docs/ARCHITECTURE.md` Decision 10's clock
table; the rest of that table is unchanged.*

## The one sentence

Every call this build makes to a model is watched by one controller that, at
every instant, knows how much longer the silence is expected to last and what
acting would cost — and acts the moment the first exceeds the second, or at a
ceiling it never crosses, whichever comes first.

## What is wrong today

Five things, all of them measured on `dev@97724633`:

- **A cold ledger switches the clock off.** `choose.go` returns an empty
  `Choice` when it knows fewer than two lanes; `hedge.go` refuses to build a
  race when the `Choice` names no alternative; `client.go` builds the watch
  nowhere else. So the case that most needs a deadline — a model nobody has
  measured — is the one case that gets none. A turn waited three minutes with
  no clock of any kind on it.
- **Routing and waiting are one value.** `Choice` answers *which lane* and
  carries `Deadline` and `Alt` as well, so "no opinion about where to send it"
  and "no opinion about when to give up on it" are the same nil.
- **A model picked after launch is never learned about.** The sheet beat's model
  list is fixed at session construction from two config slots. `SetModel` does
  not touch it, and the picker calls `SetModel`. Cold start is therefore the
  steady state for exactly the models people choose deliberately.
- **Thinking defeats the deadline.** A reasoning delta counts as a first token,
  so the first-token branch is left forever and the mid-stream test then needs
  a believed rate the cold ledger does not have. A stall sixty seconds into a
  run of thought is answered by a transport bound two and a half minutes away.
- **A pinned lane that stalls says nothing.** A strict pin sends `only`, carries
  no alternative, and the watch has nowhere to go — which is correct — but the
  surface never mentions it either, so the experience of a pin gone slow is an
  unexplained wait.

Underneath all five is one shape: **absolutes where a belief belongs, and a
belief where an invariant belongs.** A fixed 15-second gap, a fixed 64-token
commitment, one hedge per request, an 8-second ceiling that only exists when a
belief does. This design keeps exactly one absolute — how long a person waits —
and learns everything else.

---

## A. The invariant

**From zero history, for every token-generating call through the funnel, at
every instant the client knows (i) the expected remaining wait given the silence
so far and (ii) what acting would cost. It acts when waiting is expected to cost
more than acting.**

Two clauses bound it:

1. **A hard ceiling bounds time-to-action regardless of belief.** For a role a
   person is reading it is **10 seconds** (`lane.VisiblePatience`); every other
   role scales it by its own `Patience` multiplier. Past the ceiling something
   is done, whatever the arithmetic says, because a belief that recommends
   waiting past a person's patience is answering a question nobody asked.
2. **Unless every reachable lane is believed slow** — in which case acting buys
   nothing, so the act is to *say so*: `all lanes slow` on the HUD, with the
   wait still counting. Silence is never an option; a wait that is real is
   reported.

And one law that is the root cause, stated so it cannot come back:

> **Routing and waiting are two questions and must never share one nil.**
> `lane.Choice` answers which lane. `control.Plan` answers when to act. A call
> with no routing opinion still has a plan, still has a ceiling, and still
> reports. `Choice.Empty()` means "send what you would have sent"; it has never
> meant "wait forever" and now it cannot.

---

## B. One controller for all phases

### The three silences are one question

A request is silent for one of three reasons: nothing has arrived at all, the
endpoint is writing a run of thought nobody can read, or visible text was
arriving and stopped. Until now these were three rule sets in two files. They
are one question asked of three different distributions.

Let `T` be the time to the **next visible progress** and let `s` be how long we
have already waited without it. The controller needs one number:

```
W(s) = E[T − s | T > s]          the expected remaining wait
```

For a log-normal — which is what every measured lane is — **W rises with s**.
That is the property the whole design turns on and it is worth stating plainly:
a stream four seconds late is not four seconds from finishing; it is a draw from
the tail, and the cheapest thing to do with a draw from the tail is to ask
somebody else. `control.Survival.Remaining` is the closed form.

**And `s` is two quantities, not one.** The clock that guards the ANSWER reads
the silence a person is waiting through — the time since the last visible word —
and it is what the action floor and the role's ceiling ask about, because a run
of thought is not progress and nobody is asked to watch an empty line for
longer on the grounds that the endpoint is busy. The clock that guards the WIRE
reads the time since the endpoint last wrote anything at all, readable or not,
and it is what the stall clocks ask about: a lane that has written three words
and is now reasoning has not stopped, and buying it a second request would be
buying one for a lane that never stalled. A heartbeat is not writing and moves
neither.

### What acting costs

For the best alternative lane `a`:

```
A = E[TTFT_a] + V / rate_a + λ · Δ$ + m

  E[TTFT_a]   what a would take to say its first word, from cold, including the
              second request's own handshake
  V / rate_a  regenerating the VISIBLE tokens this stream has already delivered,
              at a's believed rate. Hidden tokens are not in V: a run of thought
              is money already spent and nothing a person would watch disappear.
  λ · Δ$      the second request's money, converted to seconds through the
              role's λ. With λ = 0 nobody is waiting, no money buys speed, and
              the controller never hedges — it only ever reports.
  m           hysteresis, so the controller does not flap at the crossing
```

**Act when `W(s) > A`.** That is the whole rule, and it is the same rule in
every phase; only the distribution behind `W` changes.

### The three distributions

| phase | `T` is | learned from | while it runs |
| --- | --- | --- | --- |
| **silent** — nothing at all has arrived | the serving lane's first token | every finished stream's TTFT | a heartbeat proves the path, never resets `s` |
| **thinking** — reasoning deltas, no visible word | *two* clocks: how long this model's whole thinking phase lasts, and the gap between two thinking deltas | thinking duration per `(model, effort rung)`; gaps from the lane's rate | the stream is ALIVE — no cut — and `s` keeps running |
| **writing** — visible text arriving | the gap between two visible tokens | the lane's rate posterior | each visible token resets `s` |

**A legitimately long think is not a stall and must not be hedged blindly.** A
model asked at the top rung may deliberate for a minute; hedging it fires a
second minute-long think and buys nothing. So the thinking phase is judged
against a *learned duration model* for that `(model, rung)`, not against the
lane's first-token belief — and the ceiling still applies, which is what keeps
the invariant true when the duration model is itself cold.

**A stalled think is a stall.** The second clock catches it: thinking deltas
arrive at the lane's believed rate like any other token, so a run of thought
that goes quiet for longer than that rate excuses is acted on exactly as a quiet
answer is.

**Heartbeats never reset the silence clock.** `: OPENROUTER PROCESSING` is proof
about the *path* and about nothing else. It keeps the dead-path claim from
firing and it keeps the lane's belief from being charged for somebody's wifi. A
clock it reset would be a clock a router could hold open forever by saying
nothing in a well-formed way.

### Which act

The controller returns one of six kinds. The order below is the ladder, and it
is the one already in `docs/ARCHITECTURE.md` — this design does not invent a
second one.

| act | when | what happens |
| --- | --- | --- |
| `None` | `W(s) ≤ A` and `s < ceiling` | keep waiting; this is the answer to almost every question |
| `Hedge` | `W(s) > A`, an alternative exists, the purse allows | another arm to `Act.Lane`; the ladder's rung 1 |
| `Ask` | the same, but a **person pinned** this lane | the offer, §E. A pin is asked, never overridden |
| `Report` | no alternative, or every alternative is believed no better | the HUD says the wait is real and why; nothing is sent |
| `Escalate` | every gate-passing lane has been tried and none answered | hands off to the EXISTING model ladder (`internal/provider/endpoints.go`, rung 4). The controller never changes a model itself |
| `Commit` | this arm has earned the answer | the same inequality read backwards: cancel the others |

**`Commit` is not a second rule.** Past the first tokens the question "should I
abandon this?" is the same inequality with `V` set to what this arm has already
written: leaving costs `E[TTFT_a] + V/rate_a`, and staying costs `W(s)`. When
staying is cheaper the arm keeps the answer. That is what retires `commitTokens
= 64` as an absolute — a number that was right for a 400-token reply and wrong
for a 4,000-token one — and it is why there is no separate commitment constant
anywhere in this design.

### Many arms, one budget

`Hedge` may fire more than once. What bounds it is money, not a counter:

- **Arms are bounded by the purse**, which is today's `lane.Budget` unchanged:
  at most a tenth of the last hour's spend, counted in requests rather than in
  minutes. `maxArms = 4` stays as the absolute cap on one question.
- **First visible progress takes the voice.** Whichever arm writes the first
  word a person can read is the arm they hear; the others are held, exactly as
  today's one-voice rule holds them.
- **`Commit` cancels the losers**, and cancelling stops the bill on most lanes
  (measured: about twenty of them, OpenAI, Anthropic and Fireworks named).
- **A hedge is a measurement** whichever way it lands. That law is unchanged and
  it is what makes the second request nearly free in information terms.

Dean & Barroso's figure is the sanity check on the whole mechanism: **one to two
per cent extra requests removes most of the p99.** If the arms are costing more
than that, the deadlines are firing too early and the proof in §K says so.

---

## C. Beliefs: global, hierarchical, time-adaptive

### Why a flat ledger cannot answer a cold start

A belief per `(model, lane)` is blind on the first request to every pair, and
the first request to a pair is the steady state. The measured world says why
that is unnecessary: on 2026-08-30, seventeen lanes of `deepseek/deepseek-v4-flash`
spanned **430 ms to 3030 ms** on first-token p50 (7×) and **6 to 75** tokens a
second (12×) — and those differences are *properties of the machines*, which
mostly persist when the same machines serve a different model. A ledger that
throws that away asks the same question from scratch for every pair.

### The sum

```
ln T(model, lane)  =  μ  +  a[lane]  +  b[model]  +  e[model, lane]
```

| term | is | moves on |
| --- | --- | --- |
| `μ` | the world's own pace | everything |
| `a[lane]` | this **provider**, across every model it serves | every sighting of that provider |
| `b[model]` | this **model**, across every provider | every sighting of that model |
| `e[model, lane]` | this deployment, right now | only its own sightings |

A prediction is the sum of the four means with the sum of the four variances. A
pair nobody has measured predicts from `μ + a + b` with an honestly wide spread
— which is exactly "we know roughly, and not precisely" — and cold start needs
no special case anywhere. `lane.Chain` is the type; `lane.Chain.Predict` is the
sum.

Three chains are kept per subject: `ln TTFT` in milliseconds, `ln rate` in
tokens a second, and `ln thinking duration` in seconds keyed on
`(model, effort rung)` — the last has no lane term, because a lane cannot make a
model think less, it can only make the same thought arrive faster, which the
rate chain already says.

Quality stays a **Beta** and stays a **gate, never a weight** (Decision 10's
law). It is hierarchical in the same shape — a provider that truncates on one
model usually truncates on another — but its parent is borrowed as a *prior*
rather than summed, because a success count is not additive in the log domain.

### The update, exactly

One observation `z` (already in the log domain) with observation noise `R`. The
prediction is the sum, so the innovation variance is the sum:

```
ŷ = Σ X_i                            i over the four levels
S = Σ P_i + R
ν = (z − ŷ) / √S                     the standardized residual

for each level i:
    K_i  = P_i / S                   this level's share of the surprise
    X_i ← X_i + K_i · (z − ŷ)
    P_i ← P_i · (1 − K_i)
```

This is a scalar Kalman update on a four-dimensional diagonal state with
`H = [1 1 1 1]`. It is eight lines of arithmetic and it has the property the
design needs: **a level that is already certain absorbs almost none of the
surprise, and a level that knows nothing absorbs almost all of it.** The first
sighting of a brand-new provider moves `a[lane]` a long way and `μ` barely at
all; the thousandth moves `e` and nothing else.

Ageing, per level, per unit of time with no observation:

```
P_i ← min( P_i · 2^(Δt / halfLife_i),  ceiling_i )
```

The estimate is never moved — we have no reason to think a lane got faster, only
a reason to be less sure — and the ceiling is what stops a belief becoming
*worse* than free. This is `Posterior.Predict`'s law, kept, and now stated four
times with four half-lives.

### The numbers, and which are priors

**Half-lives** — how fast each fact really turns over:

| level | half-life | why |
| --- | --- | --- |
| `e[model, lane]` | **10 min** | one deployment's queue. Unchanged from today's `HalfLife`, which was measured against real recovery. |
| `a[lane]` | **30 min** | a provider's fleet: a capacity event moves every model it serves, and it is over in the half hour the sheet aggregates on. |
| `b[model]` | **6 h** | a model's size and shape. It changes when a deployment is re-quantized, not within a session. |
| `μ` | **24 h** | the world's pace. |
| quality (all levels) | **60 min** | `QualityHalfLife`, unchanged: what a lane *is* holds for hours, and forgetting it at the speed of load makes the gate inert. |

All five are **priors to be learned**: the build ships with these and
`bench/lanelab` fits them against replayed sightings. They are the only numbers
in this design that are guesses, and they are guesses about how fast the world
changes rather than about how fast a lane is.

**Prior widths** at each level, in nats of log-spread, for a process that has
seen nothing:

| level | σ | why |
| --- | --- | --- |
| `μ` | 1.2 | first-token medians across the measured seventeen span ln(3030/430) = 1.95, so a spread that covers most of it without claiming to. |
| `a[lane]` | 0.9 | most of the observed spread is between providers. |
| `b[model]` | 0.6 | less than between providers, on the models this build talks to. |
| `e[model, lane]` | 0.5 | what is left after the two above. |

**Fixed, and not priors** — these are statements about people and about physics,
not about machines, and nothing learns them:

| constant | value | what it is |
| --- | --- | --- |
| `lane.VisiblePatience` | 10 s | how long a person watching an empty line is asked to wait. |
| `lane.ActionFloor` | 700 ms | below this a second request is racing the network, not the lane: it has its own handshake and prefill to pay. |
| `lane.ReadRate` | 18 tok/s | how fast a person reads; the ceiling on how much delivery speed is worth buying. |
| role `Patience` | ×0.5 … ×6 | §F. |

### The sheet is a prior, and it is also the only measurement of a tail

The endpoints sheet enters as a pseudo-observation at `R = k·σ₀²` with
`k = SheetWeight = 4`, into `e[model, lane]` — never into `μ` or `a[lane]`,
because a sheet is published per model and folding it into the world's pace
would let one refresh move every belief this process holds.

**And it enters as an OFFSET, which means the pair's own level takes the whole
of it.** A row is an absolute first token and a belief is a sum, so what is left
to learn from a row is this deployment's distance from what its parents already
say. Shared out as a Kalman gain it was not learnt at all: `μ` holds most of the
variance and may not move, so the level that may took about a sixth of the
difference and a lane the sheet published at 430 ms was believed at 1.2 s.
`e[model, lane]` carries the difference in full and `Chain.Predict` comes back
on the published number. The variances still shrink by each level's own gain,
because how much a row TEACHES is a different question from where it puts the
median: a half-hour aggregate at `SheetWeight` teaches little, so the belief is
centred on the sheet and honestly unsure of it. **`b[model]` is no longer moved
by a row** — an offset that landed there too would be re-aimed by the next row
of the same sheet, leaving every pair but the last centred somewhere nobody
published.

**Its spread is floored and it is never a certainty.** Today `derivedDeadline`
reads `TTFT.Quantile(1.2816)` raw, which is the variance of the *estimate* — it
shrinks to nothing with evidence, and a controller reading it believes a tail
impossible. `Chain.Survival(draw, unit)` is the one door that converts a chain
into a distribution to wait against, and the floor is not optional.

**The floor is the lane's own variability, and one figure for every lane is not
it.** A sheet row carries a p50 and a p90, and the distance between them *is*
that lane's measured dispersion — `ln(p90/p50) / 1.2816`, between about 0.15 and
1.0 nat across the seventeen measured. This build already keeps it per pair,
because it is the same number a sighting is weighed against as observation
noise; `Hierarchy.Draw` reads it back. Only a pair nobody has published anything
about falls back to the prior:

| constant | value | what it is |
| --- | --- | --- |
| `lane.SpreadFloor` | 1.0 nat | how variable one answer is taken to be where nothing has been published: the shape the measured sheet published on its **worst** lanes (p90 ≈ 3.5 × p50). |

**Held under every lane instead, it was measurably wrong**, and §K is where it
showed. A lane publishing 430 ms and 900 ms has σ = 0.577; floored at 1.0 its
`W(0.7 s)` is 0.876 s where its own spread gives 0.305 s, and acting costs about
0.71 s — so a perfectly healthy request wanted a second one at the first instant
an act was legal. `bench/lanelab/REPORT.md` measures both sides of it: the
per-lane floor halves the false-hedge rate and the spend overhead, and neither
reaches §K's threshold on the proof rows, which is a finding about the
inequality rather than about the floor.

### Change points

The world moves in steps as well as drifting: a provider adds capacity, a
deployment is re-quantized, a region fails over. Exponential forgetting handles
drift and is far too slow for a step.

**A one-sided CUSUM on the standardized residual `ν`, per pair:**

```
S⁺ ← max(0, S⁺ + ν − k)              slower than believed
S⁻ ← max(0, S⁻ − ν − k)              faster than believed
k = 0.5, h = 4.0                     nats of surprise
```

When either exceeds `h`, **reset `e[model, lane]` toward its parents** — mean to
zero, variance to the prior width — and clear both sums. Nothing else is reset:
`μ`, `a` and `b` are shared facts and a step in one deployment is not evidence
about them. The reset is recorded (`Hierarchy.Shifted`) so the call log can say
why a lane suddenly moved, and it is the only mechanism in this package that
changes an estimate rather than a variance.

`k = 0.5` and `h = 4.0` are the standard slack and alarm for detecting a one-σ
shift in about eight observations; both are priors to be fitted in the sim.

### Diurnal: argued for, and not built

**Recommendation: do not add it.** The argument for is real — router load has a
daily shape and a term for it would predict the 09:00 slowdown before it
happened. Three arguments against, and they win today:

1. **The data is not there.** A diurnal term needs weeks of observations per
   subject at the level it lives at, and the level it would live at is
   `a[lane]`, whose half-life is thirty minutes. A person's own ledger will
   rarely hold two clean cycles of any one provider.
2. **The existing mechanism already tracks it, with a lag.** A daily swing is
   slow compared to a 30-minute half-life plus a CUSUM that fires in eight
   observations. What a diurnal term buys is the *first* few requests of the
   swing, not the swing.
3. **It is the kind of term that looks right and is not checkable.** Nothing in
   the proof plan can distinguish a fitted diurnal component from an overfitted
   one on a single machine's traffic.

**The condition for adding it:** `bench/lanelab` reports residual autocorrelation
at 24 hours, on replayed real sightings, above what the drift model explains. If
that appears, the term is one more component in the sum — `c[lane, hour]` with a
long half-life — and nothing else in this design changes. The shape was chosen
to leave that door open.

### The file, and two processes

Today's store writes the whole belief set and merges last-writer-wins per lane.
**The hierarchy makes that actively wrong:** two processes each hold `μ`, `a`
and `b` for the *same* subjects and each has folded different evidence into
them. "Newer wins" then discards one process's afternoon because the other
happened to save second, and the shared levels are exactly the ones that make
cold start work.

**So the store becomes state plus an append-only observation journal.**

```
~/.aforge/v3/lanes.json     the compacted state: version, the chains, the Beta
                            counts, the facts, and the moment it was written
~/.aforge/v3/lanes.log      one line per observation since the last compaction
```

- **Append.** After an answer — never on the send path — one line of NDJSON is
  appended: `{id, ttft, gen, gap, tokens, prompt, cached, probe, at}` or an
  outcome. One `write(2)` under `O_APPEND`, about 150 bytes, with a **shared**
  advisory lock held across it.
- **Load.** Read the state, replay the whole journal into it in time order.
  Observations commute in this filter to first order and exactly in time order,
  so every process converges on the same belief. A missing journal is an empty
  one; an unparseable line is skipped and counted.
- **Compact.** On the beat, when the journal passes 512 records or 10 minutes:
  take the **exclusive** lock — which no appender can be inside — replay,
  write the state atomically through a temporary file and a rename, `ftruncate`
  the journal to zero, release. The rename is why the lock file is separate from
  both, exactly as today.
- **A lock that cannot be taken is not a reason to lose a belief.** The current
  fallback is kept: on a filesystem with no advisory locking the work still
  happens, unlocked, which is what a lone process has always done.
- **Migration.** A `lanes.json` holding today's flat array is read as the pair
  level of a fresh hierarchy, with `μ`, `a` and `b` unknown. Nothing is thrown
  away and nothing is claimed that was not measured.

Why not keep last-writer-wins: it cannot merge shared components. Why not a
journal alone with no compacted state: an unbounded replay on every process
start is a cold start that gets slower every day.

---

## D. Choice

The chooser is largely right and this design changes four things about it.

**The score is unchanged in shape.** After the capability gate — tools, context,
quantization, uptime, `status` — Thompson sampling over

```
score = T_perceived + tail + λ · price
T_perceived = max(TTFT, …) + visible/min(rate, ReadRate) + hidden/rate
tail        = the p90 term, because a person remembers the twelve-second wait
              and not the four-hundred-millisecond one
```

The reading-rate ceiling stays: delivery faster than 18 tokens a second buys a
person nothing, so a lane is not paid for it.

**λ is the role's** (§F), and the highest λ belongs to the role a person is
reading. Background roles are cheap and `λ = 0` is price-only, which is the
honest reading of "nobody is waiting".

**What changes:**

1. **`len(beliefs) < 2` stops being a refusal.** With the hierarchy, a model
   nobody has measured still has a prediction for every lane the *sheet* names
   and for every provider the *world* has seen. An order of one is still not a
   ranking — that law stands — but "one lane in the ledger" is no longer the
   same thing as "one lane known".
2. **`Choice` loses `Deadline` and `Alt`.** They belong to `control.Plan`, which
   is built for every call. `Choice` carries `Order`, `Only`, `Ignore`,
   `Frontier` and `Why` and nothing about time.
3. **A never-seen model triggers a one-shot asynchronous sheet refresh** and
   answers from the hierarchy meanwhile. The refresh is queued to the beat and
   **never on the send path** — that law is untouched and its structural test
   still holds it.
4. **`SetModel` informs the beat.** A model the session moves to is added to the
   beat's list and refreshed once, at once. Without this the first three changes
   are unreachable for exactly the models people pick deliberately.

`provider.order` still carries the top three with `allow_fallbacks` on, because
a slow answer beats no answer. Probe-on-typing stays optional and unchanged: at
about half a thousandth of a cent it is the cheapest sighting there is, and it
measures our own path rather than everybody's.

---

## E. A pinned lane is asked, never overridden

Same controller, same arithmetic, different act. Where an unpinned call would
`Hedge`, a pinned one raises `Ask`.

**What a person sees**, on the status line, in the segment the phase clock
already owns:

```
coreweave is slow · switch to auto? (y)
```

**The rules:**

- **It is raised once.** A second offer for one request would be nagging.
- **`y` fires the rescue at once** — the same hedge the controller would have
  fired, to the same lane the frontier already named. There is no second choice
  made at the worst possible moment.
- **Any visible token withdraws it.** The pin came good; the question is moot;
  the line goes back to what it was. A thinking delta does **not** withdraw it,
  because nothing has arrived that a person can read.
- **It also withdraws at `PhaseWindow`** (15 s), like every other phase, so a
  question about a request that is over never sits on the screen.
- **Once, it may set pinned-borrow.** After the offer is accepted the surface
  asks — once per profile, not per request — whether the pin should borrow when
  it stalls in future, and writes `lane.<slot>.borrow` if so. That is the
  existing `LanePin{Borrow: true}` row and no new setting.
- **The pin itself is untouched.** Accepting is for *this answer*. The next
  request goes to the pinned machine, because that is what a pin means.

**The message types.** The offer rides the phase channel that already exists —
`provider.PhaseNews` → `session.OnPhaseNews` → `tui3.PostPhaseNews` — because a
second channel for one sentence is a second thing to keep alive.

```go
// internal/provider/phase.go
const PhaseAsking Phase = "asking"   // a wait a person can end

// PhaseNews gains one field:
//   Ask string — an opaque token naming the open offer, empty when there is none.

// internal/provider/offer.go  (new)
func AnswerOffer(ask string, yes bool) bool   // true when an offer was still open
```

`PhaseNews.Detail` carries `"coreweave is slow"`, `Then` carries `"auto"`, and
`Waiting()` includes `PhaseAsking`. The offer registry is a small map with a
mutex, entries expiring at `PhaseWindow`, living beside the race that owns them.

**The keystroke.** `y` in `internal/tui3`, taken **only while an offer is live
for the model on screen and the composer is empty**. Somebody who is typing is
writing their next message, not answering a question, and a surface that stole a
`y` out of a sentence would be worse than the wait. The manual page for lanes
gains the offer and its key in the same change (the manual law).

**With no surface at all** — `aforge do`, a standing run, any headless session —
there is nobody to ask, and the invariant still holds. So:

> **A pinned lane with no reader borrows at the ceiling, once, and says so in
> the log.** `pinned lane coreweave was silent for 10s — borrowing auto for this
> answer`.

The reason is the whole point of the pin: a pin is a person's instruction, and
an instruction whose author cannot be reached at the moment it becomes expensive
is honoured by getting them their answer and telling them what was done. The
alternative — a headless run that waits forever on a machine that has gone
quiet — is the defect this design exists to remove, and it is worse in a
headless run than anywhere else, because nobody is watching to notice.

"No reader" is `provider.OnPhase` having no registered reader — the same seam
the surface uses — so nothing new has to be configured and the two cannot drift.

---

## F. Every call

The funnel law stands: **one function sends a completion on the wire, and
everything that goes through it is watched.** The role sets λ and the ceiling
multiplier; it never sets whether a ceiling exists.

| role | λ | patience | ceiling | why |
| --- | --- | --- | --- | --- |
| `talk` | interactive | ×1 | **10 s** | a person is reading this stream |
| `leaf.attached` | interactive | ×1 | **10 s** | somebody is watching the room it runs in |
| `leaf.unattended` | 0 | ×3 | 30 s | nobody is there; the seconds are only worth money once somebody is |
| `standing` | 0 | ×6 | 60 s | unattended by construction, and long by design |
| `memory` | 0 | ×3 | 30 s | a reflex nobody waits on |
| `auxiliary` | 0 | ×3 | 30 s | a title, a route question, a fold-up |
| `judge` | 0, critical | ×6 | 60 s | on the path to something that is waiting, and worth being right |
| `design` | 0, critical | ×6 | 60 s | the same, and longer answers |
| `probe` | 0 | ×0.5 | 5 s | one token bought on a keystroke; a probe that is slow has already told us what we asked |
| `unknown` | 0 | ×3 | 30 s | the conservative reading |
| `media` | interactive | ×6 | 60 s | **outside the token controller — see below** |

**`RoleMedia` stays outside, and the exclusion is from the token controller
rather than from patience.** An image, a piece of music, a transcription: they
produce no token stream at all (`RoleFacts.Streams` is false), so there is no
first token, no gap between tokens and no rate to be surprised by. What bounds
them is a single expected duration and the transport's own wall, which is what
they have today. Giving them a phase machine with three distributions and no
tokens to drive it would be machinery that could only ever be wrong.

There is still **no role for a hedge**. The second request of a race is the same
errand as the first, so it inherits the role it is rescuing — and therefore its
λ, its ceiling and its place on the status line.

---

## G. Observability

**The HUD** says four things and no more:

```
via cloudflare · 0.6s · 61 t/s        an ordinary answer, and who wrote it
slow · trying parasail…               a rescue is in flight
via parasail · rescued                it worked, for this answer only
coreweave is slow · switch to auto? (y)   a pin, and a question
all lanes slow · still waiting        every reachable lane is believed slow
```

The last is new and it is the visible half of `Report`. It is the only honest
thing to say when there is nowhere better to go, and saying nothing was the old
behaviour.

**The call log** gains four fields on `calllog.Record`, beside the `DeadlineMs`,
`Lane` and `Hedged` it already carries:

| field | is |
| --- | --- |
| `SilenceMs` | how long the silence had run when something was done about it |
| `Action` | what was done: `hedge`, `ask`, `report`, `escalate`, `commit`, or absent |
| `Arms` | how many requests this one question put on the wire |
| `WasteUSD` | what the arms that did not answer cost |

Together with what is already there — the order that was sent, the lane that was
asked for, the lane that served, the deadline that was computed — a row now
answers the question this design was written for in one line: *when did it act,
what did it believe, what did it do, and what did it cost.*

**And the reasoning is on the row, not only the outcome.** `Act.Wait` and
`Act.Cost` are the two numbers the inequality was decided on; a log that records
the decision without them can only ever confirm what somebody already suspected.

---

## H. Module map

Packages and files, each with one responsibility.

| where | owns | may not |
| --- | --- | --- |
| `internal/lane/control` (new) | the hazard controller: `Survival`, `Plan`, `Act`, `Controller`. Pure functions over a moment and a belief. | import anything but the standard library — not even its parent |
| `internal/lane/hier.go` (new) | the four-level chain: fold, age, change-point | open a connection, read a clock |
| `internal/lane/journal.go` (new) | the append-only observation log and its compaction | be on a send path |
| `internal/lane/store.go` | the compacted state file, its lock and its migration | decide anything |
| `internal/lane/belief.go` | the ledger: one object that answers both `Ledger` and `Hierarchy` | hold two accounts of one lane |
| `internal/lane/choose.go` | the gate, the Pareto prune, the Thompson draw | say anything about time |
| `internal/lane/waiting.go` | the contract between them, and the one place the controller is installed | implement the controller |
| `internal/provider/hedge.go` | the race: arms, cancel, the voice | decide *when* |
| `internal/provider/offer.go` (new) | open offers and their answers | draw anything |
| `internal/provider/client.go` | the stream loop, and one `control.Reading` per event | hold a policy |
| `internal/session/agent.go` | `SetModel` → the beat | know how a belief is computed |
| `internal/tui3` | the offer keystroke and the HUD | compute a belief |

**The contract, as written in this commit:**

```go
// internal/lane/control — the whole of when-to-act
type Survival struct{ Mu, Sigma float64 }        // log-normal over seconds
func (Survival) Remaining(silence float64) float64  // E[T−s | T>s]

type Plan struct {
    Lane     string
    Ceiling  time.Duration      // the role's, and every role has one
    Floor    time.Duration
    Lambda   float64            // seconds per dollar; 0 is "nobody is waiting"
    Margin   float64            // hysteresis, in seconds
    First    Survival           // the serving lane's first token
    Gap      Survival           // between two visible tokens
    Think    Survival           // this model's whole thinking phase
    Alts     []Alternative
    Pinned   bool
    Purse    Purse
    Expected int
    Began    time.Time
}

type Controller interface {
    Note(Reading) Act
    Quiet(now time.Time) Act
    Serving(lane string, first, gap Survival, now time.Time)
    Deadline() time.Time
    Phase() Phase
    Acted(Kind) bool
}
type Factory func(Plan) Controller

// internal/lane — the belief side
type Component struct{ X, P float64 }
type Chain [Levels]Component
func (Chain) Predict() (mu, variance float64)
func (Chain) Survival(draw, unit float64) control.Survival

type Hierarchy interface {
    Wait(id ID, now time.Time) Chain
    Rate(id ID, now time.Time) Chain
    Think(model, rung string, now time.Time) Chain
    Draw(id ID) (first, gap float64)
    Shifted(id ID) bool
    NoteThinking(model, rung string, took time.Duration, at time.Time)
}

// The plan is built in one place, from a routing answer and what is believed
// about the machine it names. A Pace is the pair of distributions a wait is
// judged against; PaceOf scripts one from a flat belief and PaceFor reads the
// hierarchy where there is one, so the transport never asserts on the door.
type Pace struct{ First, Gap control.Survival }
func PlanFor(Choice, Pace, Role, time.Time) control.Plan
func PaceOf(Belief) Pace
func PaceFor(ID, time.Time) Pace
func HeadOf(Choice) string
func Spending(*Budget) control.Purse   // asks Affordable; the race counts at send
func Thinks(model, rung string, now time.Time) control.Survival
func NoteThought(model, rung string, took time.Duration, at time.Time)

// Installed once, in one file, and it hands back what was there: a caller that
// swaps it has to be able to put back what it found, and one that put back nil
// would leave the process with no waiting policy at all.
func SetController(control.Factory) (previous control.Factory)
func Controller() control.Factory

const VisiblePatience = 10 * time.Second
const ActionFloor = 700 * time.Millisecond
func (Role) Ceiling() time.Duration
```

**Public names kept:** `lane.Choice`, `lane.Chooser`, `lane.Ledger`,
`lane.Sighting`, `lane.Outcome`, `lane.Belief`, `lane.Budget`,
`lane.PerceivedSeconds`, `lane.Lambda`, `provider.HedgeReport`,
`provider.PhaseNews`, `provider.SetLanePin`.

**Removed, and why:**

| removed | reason |
| --- | --- |
| `Choice.Deadline`, `Choice.Alt` | routing and waiting must not share a nil |
| `Watch.Hedged()` as once-only | a request may earn more than one arm; the purse bounds it, not a boolean |
| `commitTokens = 64` | replaced by the same inequality with `V` set to what was written |
| `lumpGap = 15s` | replaced by the gap distribution, which says the same thing about a slow lane and a different thing about a fast one |
| `deadlineCeiling = 8s` | replaced by the role ceiling, which exists whether or not a belief does |
| `derivedDeadline` reading a raw quantile | replaced by `Chain.Survival`, floored at the lane's own published dispersion |
| `internal/lane/watch.go` as a policy | it becomes a thin adapter, or it goes |

---

## I. The laws, held by the build

Two files, written in this commit, each law named as a sentence and each failing
with a file name and a line number — the convention `internal/lane/structure_test.go`
set and `internal/provider/funnel_law_test.go` follows.

| file | holds |
| --- | --- |
| `internal/lane/law_test.go` | the invariant and the arithmetic: every role is bounded; routing and waiting are two types; a cold belief still yields a deadline; thinking does not stop the clock; a heartbeat does not reset the silence; a pin is asked; nothing to hedge to is reported; beliefs decay; the controller comes from one factory; no test writes the real home |
| `internal/provider/waiting_law_test.go` | the wiring: every funnel call has a controller; the controller is installed exactly once; a heartbeat is reported as a heartbeat; a pinned lane can raise an offer; the call log says why it waited and what it did; `SetModel` reaches the beat; every streaming role is bounded and media is not |

Most of them are red today. They are listed with their lane in "Laws currently
red" below, and a law that is red for a reason nobody wrote down is a law
somebody will delete.

**`TestNoTestWritesTheRealHome` is a law about this repo's own tests**, and it is
here because breaking it costs somebody else their belief file. The junk found
in `~/.aforge/v3/lanes.json` on the machine this was written on came from the
sheet beat's own test, which used the default registry without moving
`AFORGE_HOME`. That one is fixed in this commit; two more are red and belong to
W5.

---

## J. Build plan

Four lanes in parallel worktrees off `feat/waiting-policy`, then one integration
lane. File ownership is strict and disjoint; no two lanes edit one file.

**One rule makes the parallelism real:** W2, W3 and W4 must not read
`Choice.Deadline` or `Choice.Alt`. They are left in place by this commit so the
tree compiles, they are read by nobody once the lanes land, and **W5 deletes
them** — a pure deletion that cannot conflict.

| lane | owns | goal |
| --- | --- | --- |
| **W1 beliefs** | `internal/lane/{hier.go, hier_test.go, journal.go, journal_test.go, belief.go, belief_test.go, posterior.go, store.go, store_test.go, quality_test.go}` | the four-level chain, its decay, its change-point, and a store two processes can share without losing an afternoon |
| **W2 controller** | `internal/lane/control/**`, `internal/lane/{waiting.go, watch.go, watch_test.go, watchserving_test.go, roles.go}` | one hazard controller for all three phases, installed once, testable on a fake clock |
| **W3 the wire** | `internal/provider/{hedge.go, client.go, lanes_ctx.go, offer.go, phase.go, calllog.go, hedge_test.go, offer_test.go}`, `internal/calllog/calllog.go` | a controller on every call, k arms under one purse, the offer seam, and a log an autopsy can read |
| **W4 choice & surface** | `internal/lane/{choose.go, choose_test.go, frontier.go, sheet.go}`, `internal/session/{agent.go, phasenews.go, lanenews.go}`, `internal/tui3/{lanes.go, phase.go, keys.go}`, `internal/manual/chat/lanes.md` | cold start answered from the hierarchy, `SetModel` → beat, the `y` key and the HUD |
| **W5 integration** | `internal/lane/contract.go`, `internal/lane/{e2e_test.go, lane_test.go}`, `bench/lanelab/**`, `docs/ARCHITECTURE.md`, `docs/changes/unreleased/` | delete the two `Choice` fields, extend the sim, fix the last two home-pollution tests, retire the clock rows |

### W1 — beliefs

*Implements* `lane.Hierarchy`, `lane.Chain` folding and ageing, the journal.
*Unit tests*: a fold moves an unknown level and not a certain one; four
half-lives age four ways; a CUSUM resets the leaf and not the parents; two
`Ledger`s over one file both see each other's observations; a v1 `lanes.json`
migrates without loss; an unparseable journal line is skipped and counted.
*Turns green*: `TestBeliefsDecayWhenUnobserved`.

### W2 — controller

*Implements* `control.New`, and `SetController` at `internal/lane`'s init.
*Unit tests*: a fake clock and a scripted `Plan`; `W(s)` rises for a log-normal;
the crossing is found where the closed form says; the ceiling fires with no
belief; a heartbeat does not move `s`; a thinking delta moves the phase and not
`s`; a pinned plan asks; an empty `Alts` reports; commit is the same inequality;
`Deadline()` never returns a moment in the past.
*Turns green*: `TestUnknownBeliefStillYieldsADeadline`,
`TestThinkingDoesNotStopTheClock`, `TestHeartbeatsDoNotResetSilence`,
`TestAPinnedLaneRaisesAnOfferNotAHedge`,
`TestWithNothingToHedgeToTheWaitIsReported`,
`TestTheControllerIsInstalledExactlyOnce`.

### W3 — the wire

*Implements* the unconditional controller in `completeWithMessagesStreaming`,
one `control.Reading` per stream event, k arms under the purse, `offer.go`,
`PhaseAsking`, the four log fields.
*Unit tests*: against `lanestub` — a cold store still cuts over; a heartbeat-only
stream is acted on; a stalled thinker is acted on and a long thinker is not; an
offer is raised, answered, and withdrawn by a token; a headless run borrows and
logs; losers are cancelled and the stub counts the cancels.
*Turns green*: `TestEveryFunnelCallHasAController`,
`TestAHeartbeatIsReportedAsAHeartbeat`, `TestAPinnedLaneCanRaiseAnOffer`,
`TestTheCallLogSaysWhyItWaitedAndWhatItDid`.

### W4 — choice & surface

*Implements* cold start from the hierarchy, the one-shot async refresh,
`SetModel` → beat, the `y` key, the two new HUD lines, the manual page.
*Unit tests*: a model with no ledger entry produces an order; the refresh is
queued and never awaited; `SetModel` adds to the beat; the key is ignored while
the composer has text; `tuiwords` for the two new strings.
*Turns green*: `TestSetModelReachesTheBeat`.

### W5 — integration & proof

*Implements* the `Choice` field deletions, the sim scenarios (§K), the
`ARCHITECTURE.md` table, the changelog entry, and the two remaining
home-pollution fixes in `e2e_test.go` and `lane_test.go`.
*Turns green*: `TestRoutingAndWaitingNeverShareANil`,
`TestNoTestWritesTheRealHome`.

### The e2e recipe

`bench/lanelab/gosim` already drives the real `lane` package over `lanestub`.
Four scenarios are added, and each is a row of the pass table in §K:

| scenario | staged with |
| --- | --- |
| **cold store** | `-prime=none`, a fresh `AFORGE_HOME` per seed, no sheet |
| **stalled lane** | `lanestub.Profile.StallAfter` / `StallFor` on the modal lane |
| **thinking model** | `lanestub.Profile.Reasoning` set long, with and without a stall inside it |
| **pinned lane** | a policy that sends `only:[pin]`, with and without a reader for the offer |

Real models, under the existing tag:

```sh
go test -tags e2e ./internal/lane/ -run TestReal -v
```

with one new case — a pinned slow lane on a real router raises an offer, and `y`
lands the answer from somewhere else.

---

## K. Proof

### The simulator

`bench/lanelab/sim.py` and `bench/lanelab/gosim` keep their existing gate — **p90
at least 30% better than the `default` arm at no more than the wait it bought,
priced through λ** — and gain four pass criteria that are about the invariant
rather than about the average:

| criterion | threshold | why that number |
| --- | --- | --- |
| **time-to-action, stalled lane, cold store, talk role** | ≤ 10 s in **100%** of trials | it is the invariant. Not a percentile: a ceiling that holds 99% of the time is not a ceiling. |
| **false hedges on a healthy lane** | ≤ **2%** of requests | Dean & Barroso's measured figure for how much extra traffic removes most of a p99. Above it the deadline is firing early and the money is real. |
| **spend overhead** | ≤ **3%** of the arm's own bill | the sheet's own price spread is 4.7× between the dearest and the median lane, so 3% is well inside the noise of choosing a different lane at all. |
| **long think, not hedged** | ≥ **95%** of legitimate thinking phases finish without an arm | the failure mode this design most risks introducing. |

Two more, reported and not gated, because they are what the next argument will
be about: the distribution of `s` at the moment of action, and the share of
actions that were `Report` rather than `Hedge`.

### The live A/B

`bench/lanelab/live.sh --compare` runs two arms against one real router on one
account, alternating request by request so a slow ten minutes hits both:

- **Arm X** is `dev` as it stands. **Arm Y** is this design.
- **The labels are opaque** until the numbers are in. The operator does not know
  which is which while reading them, because the person who built a thing is the
  worst possible reader of its first run — `ideation/provider-routing.md` Part IV
  records a live test that congratulated itself, and this is the guard against a
  second one.
- **One price table for both arms**, and the diff is what is judged: p90 wait,
  time-to-action on the stalls that occurred, arms per request, dollars per
  thousand requests.
- **The cold-store half is run first**, from an empty `AFORGE_HOME`, because that
  is the state the reported defect happened in and a warmed ledger would hide it.

### Laws currently red — none, as of the integration lane

Written before the work, failing honestly, and now every one of them green. The
table is kept rather than deleted because the interesting column is the third
one: what each law was red FOR is the shape of the defect this design was
written from, and a list of green test names with no reasons beside them is a
list nobody can read back.

| law | file | was red because | landed in |
| --- | --- | --- | --- |
| `TestRoutingAndWaitingNeverShareANil` | `internal/lane/law_test.go` | `Choice` still carried `Deadline` and `Alt` | W5 |
| `TestUnknownBeliefStillYieldsADeadline` | `internal/lane/law_test.go` | no controller was installed | W2 |
| `TestThinkingDoesNotStopTheClock` | `internal/lane/law_test.go` | no controller was installed | W2 |
| `TestHeartbeatsDoNotResetSilence` | `internal/lane/law_test.go` | no controller was installed | W2 |
| `TestAPinnedLaneRaisesAnOfferNotAHedge` | `internal/lane/law_test.go` | no controller was installed | W2 |
| `TestWithNothingToHedgeToTheWaitIsReported` | `internal/lane/law_test.go` | no controller was installed | W2 |
| `TestBeliefsDecayWhenUnobserved` | `internal/lane/law_test.go` | the ledger did not answer `Hierarchy` | W1 |
| `TestNoTestWritesTheRealHome` | `internal/lane/law_test.go` | two tests used the default registry without a home of their own | W5 |
| `TestEveryFunnelCallHasAController` | `internal/provider/waiting_law_test.go` | the race refused a call whose choice named no alternative | W3 |
| `TestTheControllerIsInstalledExactlyOnce` | `internal/provider/waiting_law_test.go` | nothing installed one — and then, briefly, the law could not SEE the one that did: it grepped for a qualified call and the install is unqualified, in the package that owns the seam. It reads the syntax tree now, so both spellings count and the declaration excludes itself | W2, W5 |
| `TestAHeartbeatIsReportedAsAHeartbeat` | `internal/provider/waiting_law_test.go` | the stream loop filled no `Reading` | W3 |
| `TestAPinnedLaneCanRaiseAnOffer` | `internal/provider/waiting_law_test.go` | there was no `PhaseAsking` and no `AnswerOffer`. It was a source grep while those were being written and is a behaviour now: the shipped pin path has to yield a plan that is `Pinned` AND names somewhere a `y` would go | W3, W5 |
| `TestTheCallLogSaysWhyItWaitedAndWhatItDid` | `internal/provider/waiting_law_test.go` | `calllog.Record` carried no action fields | W3 |
| `TestSetModelReachesTheBeat` | `internal/provider/waiting_law_test.go` | `SetModel` never told the beat | W4 |

Green before the work started, so that the set was never vacuous:
`TestEveryRoleWaitsForABoundedTime`, `TestTheControllerIsBuiltFromTheOneFactory`,
`TestEveryStreamingRoleIsBoundedAndMediaIsNot`.

And one law the integration lane added, because the finding it holds was found
rather than designed: `TestEveryTransportBoundSitsAboveTheCeilingTheControllerActsAt`
(`internal/provider/streamguard_ceiling_test.go`). Decision 10 says the
transport's bounds are the LAST resort; they were not. A mid-stream gap derived
at fifteen seconds pre-empted the thirty- and sixty-second ceilings of every
unattended role, so for most of this build's calls the first thing to act on a
stall was the one act that throws the whole attempt away. The bounds are scaled
by the role's patience now and floored at twice its ceiling, and the law walks
every role in the table at every rate the derivation can be handed.

## What this deliberately does not do

- **It does not race every lane on every request.** Three to five arms is three
  to five times the bill for a p50 the hedge already captures at about 2%.
  The purse is what bounds arms, and it is deliberately tight.
- **It does not add a second escalation ladder.** `Escalate` hands the wait to
  the one that exists. A controller that changed a model would be answering a
  person's question with a model nobody chose for it.
- **It does not fit a diurnal term**, and §C says what would change that.
- **It does not touch the transport's bounds.** Rows 10–18 of the clock table
  stay exactly where they are: they are the last resort for the case where no
  action is possible at all, and every one of them is deliberately far past
  every one of the controller's.
- **It does not make `routing off` wait on a policy.** A person who asked for no
  steering gets none — and the ceiling still applies, because a ceiling is not
  steering. What it can do at the ceiling is `Report`, and nothing else.
