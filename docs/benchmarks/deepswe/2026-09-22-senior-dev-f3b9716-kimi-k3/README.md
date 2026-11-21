# senior-dev `f3b9716` — DeepSWE 113, Kimi K3

**78/113 solved — 69.0%**, one attempt per task, official verifiers.

The question was whether the harness holds up when the backend model is swapped to
Kimi K3, with everything the rig can pin held at the values of the
[V4.1 Flash run](../2026-09-15-senior-dev-278a076-v41-flash/).

| Metric | Value |
|---|---|
| Solved | **78/113 — 69.0%** (exact Clopper-Pearson 95% CI 59.6%–77.4%) |
| Mean F2P | 0.9485 |
| Mean P2P | 0.9876 |
| Valid grades | 113/113 — no invalid outcomes |
| Model cost | **$562.47** billed — $4.98 per attempt |
| Mean agent time | 58 min (longest 172 min) |
| Runs that hit the 3 h budget | 0/113 |
| Model calls | 11,311 total |
| Ran | 2026-09-22, launched 14:37Z, last attempt 17:30Z, graded 17:53Z |

## By difficulty

| Cut | Passes | Rate |
|---|---|---|
| Hard (38 tasks) | 12/38 | 31.6% |
| Medium (38 tasks) | 31/38 | 81.6% |
| Easy (37 tasks) | 35/37 | 94.6% |

## Setup

| | |
|---|---|
| Binary | senior-dev `f3b9716` (branch `zeropoint95/improvements`), linux/amd64, SHA-256 `3f570d77d2f4d1bb07cdcf50360f32cd9e7ff4830dff3595cf5301439532a104` |
| Coder prompt | `prompt_sha256` `32cda36e…`, identical on all 113 attempts |
| Model | `openrouter/moonshotai/kimi-k3`, reasoning effort max |
| Routing | `{"only": ["modal", "moonshotai"]}` — the only two Kimi K3 endpoints sharing one quantisation (mxfp4), price, 1,048,576 context and an output cap above 131,072, both supporting tools |
| Compaction | window policy, 500,000 / 300,000 / 200,000, 60,000 tail |
| Output cap / budget | 131,072 tokens per call; 3 h agent cap, 10,920 s watchdog |
| Population | the frozen 113-task list, hash-pinned, four shards (29/28/28/28), one attempt per task, one seed |
| Verifiers | official DeepSWE at `0b9fabb` |
| Substrate | four `n2-standard-64`, `us-central1-a`, 2 CPU / 8 GiB per container, contained egress, 113 containers concurrent |

**Delivery was clean.** All 113 attempts passed the delivery gate — routing,
compaction watermarks, effort, model id, output cap, a single campaign-wide prompt
hash, and the control-plane record. Zero rate limits, zero attempts ended by
anything external, zero empty patches, no upstream outside the allowlist, and no
run reached the 3 h cap.

## The comparison, and why it resolves nothing

**The intended control was never run.** A DeepSeek V4.1 Flash re-anchor on this same
binary would have isolated the model; it was deprioritised. The only available
reference is the [88/113 V4.1 Flash run](../2026-09-15-senior-dev-278a076-v41-flash/),
which used a **different model**, a **different binary** and a **different coder
prompt** (`008bd219…` against `32cda36e…`, the rename having moved six lines of
`coder.md`). Those three differences are not separable.

| Reading | Result |
|---|---|
| Unpaired | 69.0% [59.6, 77.4] against 77.9% [69.1, 85.1] — intervals **overlap**, not resolved |
| Paired, 112 tasks valid in both | 77 against 88; both pass 65, neither 12, discordant 12 / 23; exact McNemar **two-sided p = 0.0895** — not resolved |

**No resolved difference, on either reading.** The preregistered decision rule is
the two-sided test. A one-sided p of 0.0448 exists and was registered as
supplementary; it is not the decision rule, and reading the result off it would be
choosing the test after seeing which one clears 0.05. This is also not a finding
that the models are equivalent, and it is not a ranking.

## Honest limits

1. **No re-anchor exists**, so nothing here isolates the model from the binary and the coder prompt.
2. **One seed, one attempt per task** — no within-configuration variance estimate. Four runs of one identical configuration previously measured **SD 0.183** mean-of-means, so some discordance would appear pairing an arm against *itself*; McNemar cannot see that.
3. **A ~10-point difference is not resolvable at n = 113.** Two arms must differ by roughly fifteen points before their exact intervals separate.
4. **Concurrency and host type were not held constant** — 113-way on `n2-standard-64` here, 24-way and 60-way on `e2-standard-32` in the references. Neither changes what is sent to the model.
5. **The earlier Kimi K3 campaign is not a control.** It scored **77/113**, three era lines back (binary `4bfb927`, default routing, `adopt_image_tree` off). Some documents cite it as 78/113; the store and that campaign's own result both say 77.
6. **The serving endpoint per call is not recorded** by this binary.
7. **No per-task claims** — per-task behaviour on this corpus is bimodal.
8. **This result exists because another arm was stopped for it.** A companion GLM 5.3 lane was killed mid-run at 15:26:32Z to free the shared credit pool. That lane has no corpus rate and must not be quoted: its partial salvage is length-biased and excludes the 58 longest tasks.

## The file

`tasks.csv` — one row per task, same columns as the other runs in `runs/`: reward,
F2P, P2P and the underlying test counts, `started` / `finished` / `seconds`,
`cost_usd`, `model_calls`, token counts, `exit`, `patch_files`, `patch_bytes`, and
the model, binary and attempt id, alongside the task's band, difficulty rank,
language and development-set flag.

## Source

Preregistration and amendments: `PREREGISTRATION-full113-kimi-k3-sweep.md` at commit
`7a509fa` on branch `kimi-k3-sweep/full113-kimi-k3`. Manifest:
`full113/manifest-kimi-k3-sweep.json`. Profile
`deepswe-full113-senior-dev-kimi-k3-sweep-v2`, labels
`full113-kimi-k3-sweep-s1r2-shard{1..4}-wave1`. Reproduce the headline with
`FULL113_MANIFEST=manifest-kimi-k3-sweep.json ./full113/report.py --require-complete`.

The per-attempt artifacts stay on the machines that produced them. This campaign ran
while the binary was called `swe-pro`; the values here are rewritten to `senior-dev`.
