# The plan-gate experiment — planner arm × split-gate mode

*On 2026-09-02 the owner chose planner B with the split gate OFF from this front.
The gate had shipped armed, with a six-item floor under every division, and
[#418](https://github.com/Agent-Field/aforge-v2/issues/418) said the floor folds
real divisions; #384 proposed three planner repairs. Neither question was going
to be settled by argument, so both were made factors of one designed experiment
and the winner was taken from the measurement. What landed in PR #435 is the
decision: unpinned, the gate has no say; `AFORGE_SPLITGATE=1` puts the shipped
count back and `judgment` asks the plan's own sizing. The `lanes` counting arm
lost and its code is gone.*

*What follows is the report as it was written, and it is
planner arm × split-gate mode, plan door, deepseek-v4-flash (kimi-k3 on R1d), 2026-09-02.*

## DECISION

**Planner B (stages), split gate off.** From the front table below, on the
pre-registered tiebreak of quality > cost > wall, with unrecoverable draws as
the disqualifier — the report's own front table, quoted:

## The front (arm × gate)
| arm | gate | draws | quality | unrecoverable | cost $ | wall s | front |
|---|---|---|---|---|---|---|---|
| D | lanes | 21 | 0.71 | 12/21 | 0.0277 | 25 | YES |
| C | lanes | 21 | 0.71 | 18/21 | 0.0209 | 22 | YES |
| B | off | 21 | 0.71 | 4/21 | 0.0129 | 27 | YES |
| B | lanes | 21 | 0.61 | 6/21 | 0.0186 | 25 | YES |
| A | lanes | 21 | 0.60 | 4/21 | 0.0142 | 21 | YES |
| D | off | 21 | 0.56 | 12/21 | 0.0143 | 28 |  |
| C | off | 21 | 0.56 | 15/21 | 0.0136 | 28 |  |
| B | shipped | 14 | 0.50 | 3/14 | 0.0105 | 24 | YES |
| A | off | 14 | 0.48 | 5/14 | 0.0194 | 28 |  |
| A | shipped | 14 | 0.43 | 1/14 | 0.0175 | 22 | YES |
| B | judgment | 14 | 0.36 | 1/14 | 0.0138 | 22 | YES |
| A | judgment | 14 | 0.33 | 4/14 | 0.0144 | 28 |  |
| D | judgment | 14 | 0.29 | 5/14 | 0.0141 | 26 |  |
| D | shipped | 14 | 0.29 | 6/14 | 0.0160 | 25 |  |
| C | shipped | 14 | 0.29 | 6/14 | 0.0255 | 21 |  |
| C | judgment | 14 | 0.21 | 3/14 | 0.0120 | 19 | YES |


Three cells tie at the top on quality, 0.71. **B off** carries the fewest
unrecoverable draws of the three — 4 of 21, against 12 and 18 for the two
`lanes` cells that reach the same quality — and the lowest cost, $0.0129 against
D lanes' $0.0277 for no more quality. `shipped`, the gate as it was, is on the
front only at 0.50 and 0.43, below the same arm's off cell in both cases.

The gate's own main effect is **not consistent across planners**: `lanes` beats
`off` under planner A (0.60 vs 0.48) and loses to it under planner B (0.61 vs
0.71). An interaction that changes sign is not an improvement to ship, and it is
why the counting arm was deleted rather than kept as a fourth pin. `judgment` is
kept as a pin because it is one-directional — it can only add keeps to the count
— and because nothing in the front argues it does harm where somebody wants a
floor with an escape.

**What this decision does not say.** No cell satisfied the pre-registered null on
every brief, and n=3 differences under ~0.15 are coins (planner A under `lanes`
fell from 0.76 to 0.60 on one more draw per brief). This picked the best front
cell on the responses that were measured; it did not prove the gate harmful.
Tier 2 — full runs on real issues — was never run.

Pre-registered before any cell ran: the factors, the briefs, the responses, the tiebreak. Every draw was judged
blind from the graph JSON alone, against a lanes-matched rule written for each brief before its first plan.
Draws: 16 cells × 7 briefs × 2, plus a third draw on the seven cells within reach of the n=2 front (273 judged draws, ≈$5).
Tiebreak stated in advance: quality > cost > wall; unrecoverable = oversized-undivided / no graph / dead fan-out.

## The front (arm × gate)
| arm | gate | draws | quality | unrecoverable | cost $ | wall s | front |
|---|---|---|---|---|---|---|---|
| D | lanes | 21 | 0.71 | 12/21 | 0.0277 | 25 | YES |
| C | lanes | 21 | 0.71 | 18/21 | 0.0209 | 22 | YES |
| B | off | 21 | 0.71 | 4/21 | 0.0129 | 27 | YES |
| B | lanes | 21 | 0.61 | 6/21 | 0.0186 | 25 | YES |
| A | lanes | 21 | 0.60 | 4/21 | 0.0142 | 21 | YES |
| D | off | 21 | 0.56 | 12/21 | 0.0143 | 28 |  |
| C | off | 21 | 0.56 | 15/21 | 0.0136 | 28 |  |
| B | shipped | 14 | 0.50 | 3/14 | 0.0105 | 24 | YES |
| A | off | 14 | 0.48 | 5/14 | 0.0194 | 28 |  |
| A | shipped | 14 | 0.43 | 1/14 | 0.0175 | 22 | YES |
| B | judgment | 14 | 0.36 | 1/14 | 0.0138 | 22 | YES |
| A | judgment | 14 | 0.33 | 4/14 | 0.0144 | 28 |  |
| D | judgment | 14 | 0.29 | 5/14 | 0.0141 | 26 |  |
| D | shipped | 14 | 0.29 | 6/14 | 0.0160 | 25 |  |
| C | shipped | 14 | 0.29 | 6/14 | 0.0255 | 21 |  |
| C | judgment | 14 | 0.21 | 3/14 | 0.0120 | 19 | YES |

## Per brief, front cells against the two nulls (mean; draws; u = unrecoverable draws)
| brief | A shipped | A off | A lanes | B off | B lanes | D lanes | C lanes |
|---|---|---|---|---|---|---|---|
| R1a | 1.00 (1.00/1.00) u0 | 0.00 (0.00/0.00) u1 | 0.67 (1.00/1.00/0.00) u0 | 1.00 (1.00/1.00/1.00) u0 | 0.33 (0.00/0.00/1.00) u0 | 0.67 (0.00/1.00/1.00) u2 | 0.33 (0.00/1.00/0.00) u1 |
| R1b | 0.50 (0.00/1.00) u0 | 1.00 (1.00/1.00) u0 | 0.67 (1.00/1.00/0.00) u0 | 0.67 (1.00/0.00/1.00) u0 | 1.00 (1.00/1.00/1.00) u0 | 0.67 (1.00/1.00/0.00) u1 | 1.00 (1.00/1.00/1.00) u3 |
| R1c | 0.50 (1.00/0.00) u1 | 0.50 (0.00/1.00) u2 | 0.11 (0.33/0.00/0.00) u1 | 0.33 (0.00/0.00/1.00) u1 | 0.67 (0.00/1.00/1.00) u1 | 0.00 (0.00/0.00/0.00) u0 | 0.67 (1.00/0.00/1.00) u2 |
| R1d | 1.00 (1.00/1.00) u0 | 0.50 (1.00/0.00) u1 | 0.67 (1.00/1.00/0.00) u1 | 0.56 (0.00/1.00/0.67) u1 | 0.33 (0.00/1.00/0.00) u3 | 1.00 (1.00/1.00/1.00) u1 | 0.33 (0.00/1.00/0.00) u3 |
| H1 | 0.00 (0.00/0.00) u0 | 0.38 (0.75/0.00) u0 | 0.42 (0.25/0.00/1.00) u0 | 0.50 (0.00/1.00/0.50) u0 | 0.25 (0.50/0.00/0.25) u1 | 1.00 (1.00/1.00/1.00) u3 | 0.75 (0.25/1.00/1.00) u3 |
| H2 | 0.00 (0.00/0.00) u0 | 0.50 (1.00/0.00) u0 | 1.00 (1.00/1.00/1.00) u1 | 0.89 (0.67/1.00/1.00) u0 | 0.67 (1.00/1.00/0.00) u1 | 1.00 (1.00/1.00/1.00) u3 | 1.00 (1.00/1.00/1.00) u3 |
| H3 | 0.00 (0.00/0.00) u0 | 0.50 (1.00/0.00) u1 | 0.67 (1.00/1.00/0.00) u1 | 1.00 (1.00/1.00/1.00) u2 | 1.00 (1.00/1.00/1.00) u0 | 0.67 (1.00/0.00/1.00) u2 | 0.89 (0.67/1.00/1.00) u3 |

## Reading
- No cell satisfies the pre-registered null on every brief: the original #252 brief on the default model (R1c) is a coin for
  every arm (A shipped 0.50, B off 0.33, D lanes 0.00, C lanes 0.67), and R1d (kimi-k3) stays at 0.56–1.00.
- Among the 0.71 tie, B off has the fewest unrecoverable draws (4/21) and the lowest cost ($0.013); D lanes and C lanes
  reach the same quality only by carrying the measurement flag on 12/21 and 18/21 draws (see finding 2), and D costs 2×.
- Gate main effect is not consistent across arms (interaction): lanes beats off in A (0.60 vs 0.48) and loses to off in
  B (0.61 vs 0.71), because B under lanes collapses R1a on 2 of 3 draws (one atomic node, "no two pieces could be named").
  Shipped and judgment fold every held-out brief in every arm (judgment's fallback count is the shipped one).
- Planner main effect at a fixed gate: B ≥ A (off 0.71 vs 0.48; shipped 0.50 vs 0.43); C and D are dominated by their flag mass.
- Unrecoverable-draw recovery (#384 null): A off 5/14 (R1a, R1c×2, R1d, H3) → B off 4/21 (R1c, R1d, H3×2): R1a's collapse is
  recovered, R1c/R1d are not.
- n=3 is still a coin: A lanes fell from 0.76 (n=2) to 0.60 with one more draw per brief. Differences under ~0.15 are not decisions.

## Findings for other owners
1. Plan door verdict vs tests (#428 family, for aforge-v2-3f): the door exits 0 with a graph on 103 of 273 draws that hold an
   oversized node carrying a refusal word (A 14, B 13, C 42, D 34); cell ids and node ids are with the raw draws.
2. Measurement defect in #425 (arms C, D): a per-lane node still names the whole file, so "its named material exceeds what one
   worker holds" is stamped on correctly divided lanes (R1a 3/3 lanes, R1b, H2, H3), and expansion cannot divide a lane over one
   file further. The measure needs the lane's share of the file, or must not veto a node whose summary scopes a region.
3. #424 under lanes: a stage that holds three lanes is sized atomic with no parts and refused as unnamed, so the stage question
   is never asked (B lanes R1a 2/3 draws). The atomic-until-proven pin and the measured window disagree on a 144 KB file.
4. Panel copies: C draws a three-identical-pass Panel on R1c 6/8 and R1b 3/8 — the ensemble hook fires where A splits.

## Tier 2 (full runs on real issues) not run: the pool has one anchor plus isort-2562, and the WIP freeze holds new work.
