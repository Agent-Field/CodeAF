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
| `sim.py` | the simulator: four policies x three scenarios x 10 000 requests |
| `result.json` | the raw table from `--seed 7`, as committed |
| `REPORT.md` | the results, the verdict, and the limitations |
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

The objective is **perceived** time, ported from `lane.PerceivedSeconds`:

```
T_perceived = ttft + hidden/rate + visible/min(rate, 18 tok/s)
```

Hidden tokens — reasoning, tool-call JSON — are pure waiting and cost their
full rate. Visible tokens are worth at most reading speed. Wall time is
reported next to it so a reader can see what the ceiling hides.

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

3. **`sheet-only`** — pick the best perceived time from the sheet's p50 TTFT
   and p50 rate and never learn anything. It is in the panel to separate two
   claims the design makes at once: if the prior alone captures most of the
   win, the belief and the hedge are being paid for something that was free.

4. **`belief+hedge`** — the design. Capability gate, then a quality gate on a
   Beta posterior; two scalar Kalman filters in the log domain (10-minute
   half-life) primed from the sheet as a pseudo-observation worth a quarter of
   a real sighting; a Pareto prune at the p75 of four axes so an *uncertain*
   lane stays in the set; a scalar `T_perceived + $/lambda` Thompson-sampled
   with the spread scaled by the horizon; and a hedge whose deadline `t*` is
   solved per request from the posterior's expected remaining wait rather than
   read off a constant.

## How the honesty laws show up here

- **One price table for every arm.** There is exactly one price function and
  all four policies are charged through it. A benchmark where the arms price
  themselves measures its own bookkeeping.
- **Judge the diff, not a count.** Nothing here reports how often a policy
  "won" a request. The comparison is the distribution — p50/p90/p99 of the
  wait and dollars per thousand — with the ship gate evaluated against **both**
  baselines and printed as PASS or FAIL with the actual percentages, unrounded.
- **Autopsy before quoting.** `--sweep` exists because the first run of this
  file produced a `work` verdict that reversed between two seeds. Reporting the
  first seed alone would have been a number chosen after the fact. The three
  findings that came out of the autopsy are the first half of `REPORT.md`.
