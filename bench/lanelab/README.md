# lanelab — does the lane router earn its place on the send path?

`ideation/provider-routing.md` proposes to retire the fixed-threshold strike
ledger in `internal/provider/velocity.go` and replace it with a belief, a
Pareto prune, a per-request scalar and a hedge. This directory decides, before
any money is spent, whether that machinery is worth putting on the path every
request takes.

It is a simulator and one live sheet. It cannot tell you the design works. It
can tell you whether the design's own arithmetic is self-consistent, which of
the four policies the real spread between lanes actually separates, and where
the design loses — and it can do all of that in under a second, which is the
only reason it is worth having before the live A/B.

**Read `REPORT.md` before quoting a number from here.** It has the results, the
ship-gate verdict, and the section on where this model is wrong.

## The files

| file | what it is |
|---|---|
| `fetch_sheet.py` | fetches one model's endpoint sheet from OpenRouter into `sheets/`, verbatim, with a timestamp |
| `sheets/deepseek-deepseek-v4-flash.json` | that sheet, 17 lanes, fetched 2026-08-31T04:00:21Z |
| `sim.py` | the **reference model**: four policies x three scenarios x 10 000 requests, in Python |
| `result.json` | the raw table from `--seed 7`, as committed |
| `gosim/` | the **Go simulator**: the same three scenarios against the SHIPPED code — the real registry, ledger, chooser, watch and budget, driven over `internal/lane/lanestub` |
| `gosim/result.json` | the raw table from the committed Go run |
| `gosim/proof.go` | the **four proof scenarios** of `docs/design/waiting/DESIGN.md` §J, and its §K pass table, run with `-proof` |
| `gosim/proof.json` | the raw rows and criteria from the committed `-proof` run |
| `REPORT.md` | the results of both, the verdict, and the limitations |
| `live.sh` | the blind live A/B skeleton. **Never run.** Read the header before you do. |

## Running it

```sh
export OPENROUTER_API_KEY=...
python3 fetch_sheet.py --model deepseek/deepseek-v4-flash    # refresh the sheet
python3 sim.py --seed 7                                      # the committed run, ~0.8 s
python3 sim.py --seed 7 --json out.json                      # same, plus the raw table
python3 sim.py --seed 7 --sweep 8                            # is the verdict stable? ~6 s
```

Standard library only — no httpx, no numpy — so it runs anywhere the repo does.
`--seed` fixes every draw and the seed is printed in the output; the run is
byte-reproducible.

And the Go one, from the repository root:

```sh
go run ./bench/lanelab/gosim                                   # the committed run, ~50 min
go run ./bench/lanelab/gosim -json bench/lanelab/gosim/result.json
go run ./bench/lanelab/gosim -requests 100 -seeds 1             # a quick look, ~20 s
go run ./bench/lanelab/gosim -scenario talk -policy belief -trace   # one line per request

go run ./bench/lanelab/gosim -proof                            # DESIGN.md §K's pass table, ~10 min
go run ./bench/lanelab/gosim -proof -json bench/lanelab/gosim/proof.json
go run ./bench/lanelab/gosim -proof -requests 40 -seeds 1       # a quick look, ~1 min
go run ./bench/lanelab/gosim -proof -pace flat -trace           # one line per trial, one door
```

| flag | what it is |
|---|---|
| `-requests` | requests per cell per seed; the committed run is 2000 |
| `-seeds`, `-seed` | how many seeds and where they start; the rest are `seed+2k`, as `sim.py --sweep` does |
| `-scenario`, `-policy` | run one cell rather than all nine |
| `-speedup` | how many times faster the wire runs than the world it describes; 100, the same figure `internal/lane/e2e_test.go` uses |
| `-json` | write the raw table, the ship gate and the per-seed verdicts here |
| `-trace` | one line per request on standard error — asked, served, timed — which is where every autopsy starts |
| `-sheet` | the endpoint sheet to draw lanes from |
| `-proof` | run `docs/design/waiting/DESIGN.md` §K's four scenarios and its pass table instead of the ship gate; it brings its own smaller `-requests` and `-seeds` unless you name them |
| `-pace` | proof only: which belief the plan waits against, `shipped` or `flat`. Both when unsaid, and the two tables side by side are most of the argument — see REPORT.md |

## The Go simulator

`sim.py` is a **second implementation of the design**, and a second
implementation agreeing with the first is not evidence about the build. So
`gosim` replays the same three scenarios against the code that ships:
`lane.Default()`'s real ledger primed from this directory's sheet fixture at
`lane.SheetWeight`, the real chooser, the real `lane.Watch`, the real
`lane.DefaultBudget()`, over `internal/lane/lanestub` — the fake router, which
honours `provider.order`/`only`/`ignore`, names the serving lane on every chunk,
sends a usage frame with the exact cost, and counts the streams a client
abandoned. **The ship decision is taken on the Go run** (see
`ideation/provider-routing.md`, Part III, C6); the Python table is what you read
when the two disagree.

Three things about it are worth knowing before quoting a number:

- **The first token is measured; the rate is the world's own draw.** The wire
  runs 100× faster than the world, and a Go timer armed for less than about two
  milliseconds on the machine this was written on returns half a millisecond
  late whatever you asked for. A token gap at this scale is tens of
  microseconds, so a rate read off the socket would be a measurement of the
  scheduler. `gosim` therefore scripts the stub's inter-token gap at zero and
  carries the drawn rate beside the stream. What that costs on the *first*
  token is measured per request and printed with the table.
- **The `strike` arm is missing**, and the reason is printed at the top of every
  run. `internal/provider`'s velocity ledger is unexported, process-wide and
  has no reset, so nothing outside that package can prime it, clear it between
  seeds, or drive it except through a whole `provider.Client`. The gate is taken
  against `default` instead, which is a weaker baseline than the one the design
  proposes to retire.
- **Every arm sees the same world.** The per-request draw is keyed by the seed,
  the scenario, the request and the attempt, and never by the policy, so lane
  seven behaves identically on request 412 of every arm. `sim.py` keys its
  stream by the policy instead; this is one of the few places the Go program is
  deliberately not a port of it.

## The three scenarios

The scenarios exist because **lambda differs by two orders of magnitude between
a chat turn and a background node**, and a router that used one number for both
would be wrong twice.

| scenario | lambda (s/$) | visible | hidden | q_need | who is waiting |
|---|---:|---:|---:|---:|---|
| `talk` | 90 | 400 | 0 | 0.90 | a person, watching the stream |
| `work` | 90 | 0 | 2000 | 0.97 | a person, at the end of a critical-path tool loop |
| `offpath` | 0 | 0 | 2000 | 0.97 | nobody |

Every request in every scenario reads a 4000-token prompt.

The objective is the **wait**, ported from `lane.PerceivedSeconds`:

```
T_wait = ttft + hidden/rate + visible * max(0, 1/rate - 1/18 tok/s)
```

Hidden tokens — reasoning, tool-call JSON — are pure waiting and cost their
full rate. The visible term is the **catch-up and not the reading**: text a
person reads as it arrives costs them the same reading time whichever lane
wrote it, and no router can remove it, so what a lane costs is only the amount
by which it writes slower than 18 tok/s. A lane at or above the reading rate
contributes no visible wait at all. Wall time is reported next to the wait so a
reader can see the whole answer.

Counting the reading (`visible / min(rate, 18)`, as this lab first did) changes
no ranking — it is the same constant for every lane — but it puts 22.22 s of
reading into every `talk` number, and every RATIO taken from those numbers is
then a ratio of mostly reading. `REPORT.md` has the old and new figures side by
side.

## The four proof scenarios

`-proof` is a different question from everything above it. The three scenarios
compare arms on a p90; these four ask whether the **invariant of
`docs/design/waiting/DESIGN.md` §A holds** — that from zero history every call
knows how much longer the silence is expected to last, and acts by the role's
ceiling whatever it believes. That is not a quantity with a baseline: it held on
every trial or it did not.

Each row is one line of §J's e2e table, run under `talk` — the shortest ceiling
in `lane.Role`'s table and the role the reported defect happened under — with
its fault staged for the **middle half** of its requests, exactly as the three
scenarios above break their victim between request n/4 and 3n/4. The other half
is the healthy one, and it is where a false hedge would have to come from.

| row | staged with |
|---|---|
| `cold store` | a fresh `AFORGE_HOME` per seed, nothing primed, no sheet — and the lane that serves goes quiet, because a cold store with nothing to wait for proves nothing about a clock |
| `stalled lane` | the sheet primed, the serving lane quiet five visible words into its answer (`lanestub.Profile.StallAfter`/`StallFor`) |
| `thinking model` | `Profile.Reasoning` deltas before the first visible word, on a world published at the rate it really writes; the healthy half is a legitimate long think, the sick half stalls inside one |
| `pinned lane` | `only:[pin]`, twice: with a reader for the offer, and headless, where §E has the offer become a borrow |

It drives the real chooser, the real ledger, the real `lane.PlanFor` and the
real controller `internal/lane` installs at init, over `lanestub`, with **one
`control.Reading` per stream event** — a visible word, a hidden delta, or the
router's own comment line. What it cannot reach is the offer registry: "no
reader" is `provider.OnPhase` with nothing registered, and this package does not
import `internal/provider`. REPORT.md says which half of §E that leaves
unexercised.

## The four policies

All four read **the same price table** (`request_price`, from the sheet's own
`pricing` strings) and pass **the same capability gate** (tools, output
ceiling, context, status, five-minute uptime). Giving the design a gate the
baselines do not get would credit it with a refusal the real router already
makes for free.

1. **`openrouter-default`** — the router's own documented default: sample a
   lane with weight `1/price^2` over stable lanes. No memory, no hedge. This
   is what a request gets today if nothing in the harness has an opinion.

2. **`strike-ledger`** — today's shipped law, constants copied from
   `internal/provider/velocity.go`: TTFT over 2 s or rate under 30 tok/s is a
   strike, two strikes demote, three refuse for five minutes, the ledger is
   in memory and empty at start. This is the mechanism the design retires, so
   it is the baseline that matters.

3. **`sheet-only`** — pick the shortest wait from the sheet's p50 TTFT and p50
   rate and never learn anything. It is in the panel to separate two
   claims the design makes at once: if the prior alone captures most of the
   win, the belief and the hedge are being paid for something that was free.

4. **`belief+hedge`** — the design. Capability gate, then a quality gate on a
   Beta posterior; two scalar Kalman filters in the log domain (10-minute
   half-life) primed from the sheet as a pseudo-observation worth a quarter of
   a real sighting; a Pareto prune at the p75 of four axes so an *uncertain*
   lane stays in the set; a scalar `T_wait + $/lambda` Thompson-sampled
   with the spread scaled by the horizon; and a hedge whose deadline `t*` is
   solved per request from the posterior's expected remaining wait rather than
   read off a constant.

## How the honesty laws show up here

- **One price table for every arm.** There is exactly one price function and
  all four policies are charged through it. A benchmark where the arms price
  themselves measures its own bookkeeping.
- **Judge the diff, not a count.** Nothing here reports how often a policy
  "won" a request. The comparison is the distribution — p50/p90/p99 of the
  wait and dollars per thousand — with the ship gate applied to **every arm**
  against one named baseline, `strike-ledger`, the mechanism the design
  proposes to retire, and printed as PASS or FAIL with the two quantities that
  decided it, unrounded.
- **Autopsy before quoting.** `--sweep` exists because the first run of this
  file produced a `work` verdict that reversed between two seeds. Reporting the
  first seed alone would have been a number chosen after the fact. The three
  findings that came out of the autopsy are the first half of `REPORT.md`.
