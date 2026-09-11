# Lane routing assessment — 2026-09-11

Three days of the call log (`~/.aforge/logs/calls.jsonl`, 2026-09-08 to 2026-09-10,
6,657 finished attempts), a replay of that log through the real chooser, and a live
probe of every lane those models were served by. The question asked was whether the
router gives a person the fastest usable answer, how it judges "fast enough", and
whether the algorithm needs an update or a replacement.

**Verdict: update, not replace.** The belief core (the hierarchical Kalman filter
over first-token and rate, the perceived-seconds objective, Thompson exploration) is
sound and, replayed in isolation, chooses well. What fails is the *policy around it*:
a price ceiling on the wire that vetoes every lane dearer than 1.25× the cheapest,
task calls routed on price alone, a refusal that teaches the belief nothing, a quality
gate that cannot close, cut streams that leave no timing evidence, and no absolute
floor at all. Each is a small change; together they account for the bulk of the
measured waste. A rewrite would spend a wave rediscovering the same six facts.

## How a lane is chosen today, in one paragraph

`lane.talk: auto` is **not** OpenRouter's routing. The chooser
(`internal/lane/choose.go`) ranks lanes by `price·λ + perceived seconds` and writes up
to three names into `provider.order` with fallbacks on and the sort word stripped
(`internal/provider/lanes.go` `applyLaneChoice`). OpenRouter's own sort only decides
when the ledger knows fewer than two lanes. `lane.talk: openrouter` is the setting that
hands routing back to OpenRouter (price sort). Beside the order, every watched call
also carries `max_price` = the model's list price × 1.25 (`velocity.go`
`latencyPriceCeiling`), and OpenRouter applies that filter *before* it reads the
order. λ is `AttentionValue = 90` s/$ for a watched turn and **0 for every task node**
(`loop.go` stamps `IntentBackground` when `InTask`; `laneValueOfTime` returns 0 for
it), so a task is routed on dollars with seconds as the tie-break, and the frontier
drops any lane over 1.25× the cheapest before scoring. A 429 goes to the
provider-side strike ledger (five-minute `ignore`, per process) and **never to the
belief**; a stream cut for time records a failed outcome but no timing. The quality
gate compares the Beta's *upper* 90% bound to the role's need, so a lane at a 0.65
mean with six observations still passes.

## The wire, recorded

One `aforge exec` against deepseek-v4.1-flash with `--debug` on 2026-09-11 00:15
(`~/.aforge/logs/trace/150dc017fd15a0b9`). The chooser's decision event said
"GMICloud starts in 2.8s and costs about $0.0024 for this answer", alternatives
Novita and Fireworks. The request went out as:

```json
{"order": ["GMICloud", "Novita", "Fireworks"], "allow_fallbacks": true,
 "require_parameters": true, "max_price": {"prompt": 0.1875, "completion": 0.75}}
```

OpenRouter answered 404: "0 endpoints out of 1 requested are available … Paid model
training violation (account settings): 1 endpoint excluded". GMICloud and Novita
charge $1.20 per million out; the ceiling is $0.75 (DeepInfra's $0.60 × 1.25), so
both were filtered before the order was read; Fireworks fits the ceiling and the
account's data policy excludes it. The rescue arm then sent `only: ["Novita"]` with no
ceiling and got the answer in 1.5 s. Re-sent by hand the same night, the same object
without `max_price` was served by GMICloud in 1.8 s. So for this model the chooser's
first two choices can never be reached except through a rescue, and the one lane the
ceiling leaves is the one that rate-limits 83% of the time — which is the 429 loop:
DeepInfra refuses, the ledger writes it into `ignore`, the set is empty, the release
rule lets it back, and it refuses again two seconds later (825 of 1,010 requests
that followed a 429 asked the same lane; median gap 2.0 s).

## What the log shows

| Fact | Number |
| --- | --- |
| Finished attempts, three days | 6,657 |
| Answered 200 | 5,227 (78.5%) |
| 429 | 1,052 (15.8%), median 276 ms each |
| Next request for the same model after a 429 asked the **same lane** | 825 of 1,010, median 2.0 s later, 824 in the same run |
| Attempt chains | 5,863 first attempts; 970 retries; chains reach 16 attempts |
| Wall-clock inside rows the watchdog flagged `ceiling` | 40,519 s (11.3 h) against 153,564 s in answered rows |
| Watchdog rescues refused for `budget` | 2,489 of 3,198 flagged rows |
| Rows whose `wait_s` was `+Inf` | 1,295; 136 rows priced a rescue at over an hour (`cost_s` up to 139,367 s) |

Per lane, for the two models doing most of the work (200 rows only; `tps` is
completion tokens over generation time, p50/p10):

| Model / lane | n | ok | first token p50/p90 | tok/s p50/p10 | 429 |
| --- | --- | --- | --- | --- | --- |
| deepseek-v4.1-flash / **DeepInfra** | 297 | 17.2% | 6.5 s / 34.3 s | 22 / 12 | 242 |
| deepseek-v4.1-flash / **Fireworks** | 206 | 13.6% | 3.4 s / 242 s | 203 / 71 | 173 |
| deepseek-v4.1-flash / Novita | 196 | 95.9% | 1.8 s / 2.9 s | 234 / 199 | 8 |
| deepseek-v4.1-flash / GMICloud | 70 | 98.6% | 2.9 s / 4.3 s | 211 / 174 | 0 |
| deepseek-v4.1-flash / Morph | 43 | 100% | 26.9 s / 260 s | 6 / 3 | 0 |
| glm-5.3-flash / Relace | 798 | 96.5% | 1.3 s / 3.2 s | 94 / 53 | 21 |
| glm-5.3-flash / DeepInfra | 328 | 82.9% | 2.4 s / 8.2 s | 24 / 12 | 53 |
| glm-5.3-flash / Wafer | 176 | 69.9% | 1.2 s / 23.6 s | 50 / 8 | 19 |
| glm-5.3-flash / Makora | 60 | 48.3% | 5.2 s / 17.4 s | 80 / 27 | 26 |
| glm-5.3-flash / CoreWeave | 49 | 53.1% | 4.1 s / 13.1 s | 82 / 37 | 19 |

The router asked DeepInfra 402 times and Fireworks 312 times for deepseek-v4.1-flash
on 2026-09-10 alone, against Novita 119 times. Those are the two cheapest lanes in the
persisted beliefs (`$0.60` and `$0.66` per million out, against `$1.20` for Novita
and GMICloud). With λ = 0 the chooser is doing exactly what it was told. The saving
is about six hundredths of a cent per thousand tokens; the cost was a 20-second
slower answer and an 83% chance of a 429 first.

Morph is the other shape: it answers every time, so its quality belief is fine, and
it takes 27 seconds to the first token at 6 tokens a second. Nothing in the gate
refuses it because no gate is absolute.

## Replay: the chooser is right when it is told the truth and allowed to act

`internal/lane/replay_bench_test.go` (build tag `replay`) feeds every answered row of
the log into a fresh ledger as a sighting, in order, and at every request asks the
real chooser what it would send. The oracle is the lane with the lowest perceived
wait, computed from that lane's own answers in the surrounding ±15 minutes. Regret is
seconds of perceived wait above the oracle.

```sh
REPLAY_LOG=$HOME/.aforge/logs/calls.jsonl REPLAY_SINCE=2026-09-08 \
  go test -tags replay -run '^TestReplayLog$' -count=1 -v ./internal/lane/
REPLAY_FACTS=1 REPLAY_TASK_LAMBDA0=1 ...   # tasks at λ=0, facts and prices seeded from ~/.aforge/v3/lanes.json
```

| deepseek-v4.1-flash, 913 requests | asked a lane that 429'd < 5 min ago | mean regret | median regret |
| --- | --- | --- | --- |
| The real router (from the log) | 622 | 31.9 s | 17.8 s |
| Replay, prices seeded, tasks at λ = 0 (reproduces the real picks: DeepInfra 363, Fireworks 331) | 584 | 30.2 s | 14.6 s |
| Replay, λ = 90 everywhere, 429 teaches nothing | 283 | 3.8 s | 0.0 s |
| Replay, λ = 90 everywhere, 429 recorded as a refused outcome | 172 | 0.4 s | 0.0 s |
| Oracle | 265 | 0 | 0 |

| glm-5.3-flash, 2,063 requests | recent-429 picks | mean regret |
| --- | --- | --- |
| The real router | 185 | 10.2 s |
| Replay, λ = 90, 429 as outcome | 27 | 3.4 s |

The same ranking holds for qwen3.8-27b (4.8 s → 1.0 s), kimi-k3 (5.4 → 1.7) and
deepseek-v4-flash-0731 (4.9 → 3.0). The replay carries no sheet, no affinity pin and
no hedge arms, so the residual gap between it and the log is those three things plus
λ. λ is most of it.

## Live probe, 2026-09-11 00:30 EDT

Every lane the log had seen for each model, `provider.only`, one 300-token answer:

| Model | Lane and result |
| --- | --- |
| deepseek-v4.1-flash | Novita 0.8 s / 155 tok/s · GMICloud 1.3 s / 151 · Fireworks 1.3 s / 125 · DeepInfra 1.7 s / **18** · Morph **66.7 s** / 57 · Io Net 429 · Parasail 404 (not serving) |
| glm-5.3-flash | Relace 2.4 s total · Together 2.1 · Friendli 2.1 · Z.AI 4.5 · Novita 4.8 · GMICloud 5.8 · Wafer 10.0 · DeepInfra **17.5** (18 tok/s) |
| qwen3.8-27b | AkashML 0.7 s / 80 · CoreWeave 0.3 s / 60 · Parasail 0.3 s / 56 · Cloudflare 0.2 s / 51 · Reka 0.4 s / **22** · Mancer 2 11.6 s / 8 |
| kimi-k3 | DeepInfra 0.6 s / 62 · Relace 1.1 s / 59 · Makora 0.6 s / 43 · DigitalOcean 0.5 s / 30 · Wafer 0.7 s / 26 · Morph 2.1 s / 8 |

OpenRouter's own default routing for deepseek-v4.1-flash, asked with no preference
at all, answered from Novita in 2.6 s once and from DeepInfra in 22.8 s the next
time. Handing routing back to OpenRouter is not the fix.

## The six defects, and the change for each

1. **The wire price ceiling vetoes speed.** `max_price` = list × 1.25 rides every
   latency-sorted and belief-ordered request, and OpenRouter's list price for a model
   is its cheapest endpoint, so any lane more than 25% dearer than the cheapest is
   unreachable whatever λ says. Change: drop `max_price` from the wire when the
   chooser has written an order — the frontier already priced those lanes with the
   person's λ — and keep it only on the sort-word path where nothing else bounds
   spend. Where a ceiling is wanted, derive it from λ the way `underPriceCeiling`
   does, never from a fixed multiple of the cheapest.

2. **Tasks are routed on price alone (λ = 0).** `IntentBackground` sets
   `ValueOfTime` to zero, `Lambda()` returns zero for a node "off the critical path
   with slack", and the frontier then prunes to 1.25× the cheapest before scoring.
   Every task node in this log had a person watching the room. Change: λ never
   reaches zero while any window has the conversation open; floor it at
   `TaskWallValue` for attached tasks and a quarter of it for unattended ones. The
   `price` routing word keeps zero, because it is a person saying so.

3. **A refusal teaches the belief nothing.** `noteLaneOutcome` is called for answers
   and cuts, never for a 429 or a `no heartbeat`, so the only memory of a refusal is
   one client's five-minute `ignore`, which the release rule undoes when it empties
   the set. Change: every terminal refusal, pace, dead path and cut is an
   `Outcome{Accepted: false}` on an *availability* Beta with a short half-life (five
   minutes), separate from quality; the objective divides expected wait by
   availability and adds the measured cost of a refused attempt (a round trip plus
   backoff), so a lane refusing four times in five is priced at five times its
   nominal wait rather than at zero. The set-empty release then never picks a lane
   that refused inside its own cooldown while any other lane exists on the sheet.

4. **The quality gate cannot close.** `capable()` tests `Quality.Upper(z90)`, which is
   right for a lane nobody has judged and wrong for one judged twenty times. Change:
   the upper bound until five outcomes, the posterior mean after. Same rule for the
   availability axis.

5. **Cut streams are survivorship bias.** A stream the wall ended at 90 s teaches TTFT
   nothing, so a lane's timing belief is built only from its answers that finished.
   Change: a cut is a right-censored sighting — fold it in at the cut time with the
   observation variance doubled, which the Kalman update already accepts as a wide
   measurement; the `ceiling` and `rate collapsed` reasons carry the time.

6. **Nothing is absolute.** Every threshold is relative to the best lane in the set,
   so Morph at 27 s to first token stays a candidate whenever it is the cheapest that
   answers. Change: a service floor per role, read as a gate before ranking — talk:
   first token p50 under 3 s, rate over 30 tok/s, availability over 0.8; tasks: 6 s,
   30 tok/s, 0.8. A lane under the floor on its own last ten answers is out until it
   earns back in through a probe, never through a person's turn.

Two smaller repairs ride along: the survival math in `control/hazard.go` returns
`+Inf` and hour-long rescue prices (1,431 rows), which should be clamped at the
role's ceiling; and `drift` fired 491 times on Relace answers running at 92 tok/s,
because the gap quantile is tight — it should require the visible rate to have
fallen under the lane's own p10.

The wire test that settles a related worry: `order: [X]` and `ignore: [X]` on one
object is served from a third lane, so OpenRouter reads `ignore` first and the
belief's order overwriting the ledger's is wasteful but not harmful.

## Acceptance

The replay is the bench. A change is done when, on the same log window, for every
model with over a hundred requests: mean regret is under 5 s, the count of picks
into a lane that 429'd in the previous five minutes is at or under the oracle's,
and no lane below the service floor is picked more than the probe cadence allows.
The live probe (`docs/design/routing/probe.py`, `OPENROUTER_API_KEY` in the environment) is the "now" check before
and after.

## What shipped

The pull request after this one carries four of the six: the wire ceiling on a
belief-ordered request is raised until every lane the order names fits under it, λ
has a floor of a quarter of a person's attention (`lane.UnattendedValue`), a paced pool
is an `Outcome{Refused: true}` on a new availability axis that divides the expected
wait, and a service floor (6 s first token, 15 tok/s,
half of requests answered; the rate floor sits under the reading rate) refuses a lane the ledger is sure about without ever
emptying the set. Not shipped: the quality gate keeps its upper bound — measured
again, a 0.65 mean on six observations already fails it, and the lanes that looked
well-judged while failing were failing through refusals the availability axis now
records — and censored timing for cut streams is still owed.
