# Response to the referee

The report in `REVIEW.md` was written against the draft at the head before this one. The paper was `learn.tex` then and is `pareto-crewing.tex` now; each numbered defect is answered below with what changed; the section numbers are the revised document's.

## Defects

1. **Proposition 4, the split of the cap.** Corrected. The external count is now one capped quantity `W = Kℓ/(K+ℓ)` with `K = k + ñ`, `ñ = n_pool·ℓ/(ℓ+1)`, shared between catalog and pool as `k/K` and `ñ/K` of `W`; the proof integrates `μ_rm` out once and states the precision-weighted mean. The corollary's statement ("worth at most ℓ own rows") now applies to catalog and pool together, and the sentence that follows says which form the shipped pick is (`ℓ = ∞`, `b = 0`).
2. **Drift estimator.** The factor ½ is gone from (13); the difference of adjacent day means has variance `ω² + σ²(1/n_d + 1/n_{d-1})`.
3. **Additive local sheet versus max-join.** Section 5.2 now names both stores: the install's own sheet (`internal/pool/tally`) merges by addition because each row is folded once; the relay's store is keyed by (install, day, cell) and merges by the join.
4. **Status paragraph.** Rewritten for the head this document ships with (judge hook, install nonce, push, relay, mirror) and lists what is specified but not landed.
5. **Relay code.** `relay/` lands on this head; Section 9 describes it as present.
6. **Public key.** Compiled into the binary on this head; the private-relay setting overrides it.
7. **Run counts.** The crew table is "56 combinations fitted by a ridge model on 72 scored runs over nine tasks" (caption of Figure 1 and Section 8); the seed cells come from the ledger's 120 runs (Figure 6). The abstract no longer quotes a number.
8. **Role-level ratio.** Section 8 and the abstract now state both comparisons: the role-level learner sits at half the crew-level learner fed the same scores and a third of the one fed the clean bit.
9. **Proposition 3.** Restated: identified up to one constant per connected component, one centring constraint per component; proof names the kernel.
10. **Subset of crews.** The regret experiment now runs on all 23 chat-door crews; the table and figure were regenerated and the text quotes the new numbers.
11. **`min_installs`.** Section 5.4 says the relay publishes a distinct-install count per cell and that the reader's floor applies to it; the flood claim is reduced to what the design bounds (a minority of broken or hostile installs) and says what it does not (fabricated majorities).
12. **Row fields.** The row is (role, model, score, judge, door, size, day); the install nonce is on the batch; dollars and minutes are stated to stay in the install's ledger. The cell is spelled the same way in Definition 2, the join and the row.
13. **Abstract.** The constants sentence says what the user still sets (the price of a defect and of time); "stay out and still learn" is replaced by the three modes as they behave.
14. **`explore`.** Marked "specified, not on this head" in the settings table.
15. **`E` as a surrogate.** Section 4 states that (3) is `E_H` at `r = 1`, when the ranking agrees, and that the picker can use `E_H` directly where a delivered rate is measured.
16. **Imputation.** Section 3 describes the implemented rule (median ratio over at least three donors per present index, averaged, capped at the pool's maximum, scale-proportional standing below three donors).
17. **Loader.** Section 9 says the fresher of cache and seed is read, by generated day.

## Overstated claims

- Privacy in the abstract is now a statement of what a row contains, not of what can be inferred.
- Proposition 5 is stated for the product posterior and says which dependence the draw ignores.
- The exploration tax is stated as forty runs in which Thompson is no better than the table.
- `figures.py` is said to reproduce the figures from the CSVs and the constants at its head.
- `pool status` and `pool show` are described as they print.
- Rollback is caught by the version; the join makes a replayed old publication harmless.
- The judge floor is stated as absolute.
- "Cents per run" is replaced by "one short completion per role".

## What the referee asked for and is not in this revision

- A catalog bias term `b` is added to the hierarchy with its estimator, and the seed's own `b̂ = −13.7`, `τ̂₀² = 0` are reported with the reason the day-one `k = 30` stands.
- Experiments on judge effects, drift, the settled probability and the pair term, and a flood analysis, remain future work; the Status paragraph lists them among what is specified but not landed.

## Second round (`REVIEW-2.md`)

Verdict was minor revision; the eight new defects are answered here.

1. The relay folds rows additively; the paper said the join. §5.2 now says which merge is which: addition within a store (own sheet, relay), the join between copies of sheets, and names the retry double-count and the quota that bounds it. Status and §9 no longer say the relay's store is join-merged.
2. Proposition 3's proof: the sentence the referee quotes is not in the revised proof; the proof ends at the kernel and the per-component constraint.
3. §5.3 now says the reference relay centres by one global weighted mean and that per-component levels are a convention on a disconnected graph.
4. Drift is out of §9's job description and both figures' relay nodes; it stays in Status as next to land.
5. §6 now states the constants' publication and the ten-degrees-of-freedom rule as the design, and that this head's index carries the cells and the day-one constants.
6. "the runbook beside this document".
7. `pool status` reaches the relay on this head (client B, after the referee's clone); the sentence stands.
8. README's field list now names the row's seven fields.

Camera-ready items not taken here: join-merged relay writes or batch ids; per-component centring in the relay; the distinct-install count through the readers; a checkable home for the 72 runs; the seed's offset numbers computed by `figures.py`.
