# senior-dev `f3b9716` — DeepSWE 113, Kimi K3

**78/113 solved — 69.0%**, official verifiers.

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
| Population | the frozen 113-task list, hash-pinned, four shards (29/28/28/28) |
| Verifiers | official DeepSWE at `0b9fabb` |
| Substrate | four `n2-standard-64`, `us-central1-a`, 2 CPU / 8 GiB per container, contained egress, 113 containers concurrent |

**Delivery was clean.** All 113 attempts passed the delivery gate — routing,
compaction watermarks, effort, model id, output cap, a single campaign-wide prompt
hash, and the control-plane record. Zero rate limits, zero attempts ended by
anything external, zero empty patches, no upstream outside the allowlist, and no
run reached the 3 h cap.

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
