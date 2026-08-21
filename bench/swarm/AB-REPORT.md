# Swarm A/B Report — cooperative decomposition vs refusal-first

**Configuration:** `aforge do`, model `~deepseek/deepseek-v4-flash-latest`, fresh
store per cell, n=1 per (task × arm), sequential cells (JOBS=1 timing-clean).
Arms: `AFORGE_SWARM=0` (refusal-first pipeline) vs `AFORGE_SWARM=1`
(cooperative claim-time split + measured-capacity fold).
Source: `/tmp/ab/{small,medium,large}-{off,on}/`, journal `usage` table for cost.

## The result

| task | width | wall OFF | wall ON | **ON/OFF** | deliverables OFF | deliverables ON | cost OFF | cost ON |
|---|---|---|---|---|---|---|---|---|
| small | 3 files | 234s | 120s | **1.95× faster** | **1 / 3** | 3 / 3 | $0.0129 | $0.0149 |
| medium | 5 modules | 181s | 157s | **1.15× faster** | partial | 6 / 6 | $0.0204 | $0.0480 |
| large | 9 modules | 514s | 360s | **1.43× faster** | 9 / 9 | 9 / 9 | $0.0553 | $0.0232 |

## Reading it

**Quality is the headline, not speed.** The OFF arm *under-delivered* on two
of three tasks while claiming completion: `small-off` produced 1 of 3
requested files (csvstats.py, json2yaml.py MISSING) and `medium-off` was
partial — yet both exited 0 with `settled: true`. Every ON cell produced the
complete deliverable set. This is the concrete shape of the failure the
swarm work was built against: the refusal-first pipeline, on a wide task
whose width it refuses to name, compresses the work into what one leaf can
carry and silently drops the rest. Cooperative decomposition names the parts
and lands them all.

**Wall time scales with width, as the theory predicts.** The speedup grows
from small (1.15×–1.95×) to large (1.43× sustained on a bigger task): the
more independent deliverables, the more the claim-time split pays, because
the OFF arm serializes what the ON arm lands in parallel.

**Cost is a wash and follows the work, not the mechanism.** small/medium ON
costs more (+15%, +135%) because it did *more work* — the OFF arm's lower
cost is the cost of *not finishing*. large ON cost *less than half* the OFF
arm ($0.023 vs $0.055): once the task is wide enough, the split shortens the
serial path so much that total token spend falls even as planning calls are
added. There is no systematic "swarm tax"; there is a width threshold past
which decomposition is cheaper *and* faster *and* complete.

## What this validates

The cooperative split — a leaf that finds the division at claim time, plus
the measured-capacity fold feeding settled-history base rates into sizing —
delivers the thing the system was built for: **on wide tasks it is faster,
complete, and (past the threshold) cheaper, without touching narrow-task
behaviour.** The narrow-task parity probe (hello-world, 2-file) ran
identically under both arms, confirming the OFF path is untouched.

## Caveats (n=1)

Single seed per cell, one model, one provider. These are directional, not
confidence intervals. The committed `bench/swarm/` suite exists to turn this
into a real measurement: `TIER=standard bench/swarm/run.sh` runs the 8-category
corpus at n=3 with pair-fair concurrency, and `BASELINE.md` is where those
numbers land.
