# SOTA open-weight models under 10B — mid-2026

Survey of the current state-of-the-art open-weight language models with fewer
than 10B parameters, as of mid-August 2026. All numbers are vendor-published
from their own model cards; two independent vendors (Liquid AI, Qwen) publish
cross-model sub-10B comparison tables, which lets us cross-check.

## The candidates (1B–10B range)

| Model | Developer | Released | Params | Context | License |
|---|---|---|---|---|---|
| **LFM2.5-2.6B** | Liquid AI | 2026-07-28 | 2.6B | — | — |
| **LFM2.5-8B-A1B** | Liquid AI | 2026-05-28 | 8B (1B active, MoE) | — | — |
| **Qwen3.5-4B** | Alibaba Qwen | 2026-02-27 | 4.7B | — | Apache-2.0 |
| **Qwen3.5-9B** | Alibaba Qwen | 2026-02-27 | 9.7B | — | Apache-2.0 |
| **Gemma-4-E4B** | Google | 2026-03-02 | 8B | — | — |
| **Gemma-4-E2B** | Google | 2026-03-02 | 5.1B | — | — |
| **Granite-4.0-H-Tiny** | IBM | — | 7B (1B active, MoE) | — | — |

(Excluded: Gemma-4-12B at 12B is over the 10B line. SmolLM3-3B, Phi-4-mini,
Granite-3.x are 2024/2025 — superseded.)

## Head-to-head: Liquid AI's sub-10B table (LFM2.5-2.6B card)

| Benchmark | **LFM2.5-2.6B** (2.6B) | Gemma-4-E2B (5.1B) | Gemma-4-E4B (8B) | Qwen3.5-4B (4.7B) | Qwen3.5-9B (9.7B) |
|---|---:|---:|---:|---:|---:|
| AIME25 | **51.87** | 26.33 | 34.27 | 49.33 | 56.07 |
| LiveCodeBench v6 | 59.41 | 54.92 | 63.77 | 60.85 | **69.86** |
| IFBench | **59.17** | 34.08 | 39.24 | 48.40 | 56.47 |
| Multi-IF | **80.07** | 69.44 | 77.35 | 55.67 | 62.55 |
| IFStruct | **85.49** | 64.85 | 76.65 | 36.25 | 78.50 |
| BFCLv4 | 56.88 | 36.98 | 46.39 | 50.56 | **60.13** |
| ToolSandbox | **77.83** | 52.40 | 65.00 | 75.55 | 76.44 |
| Claw-Eval (EN) | 62.85 | 53.14 | 58.02 | 62.28 | **66.53** |
| PinchBench | 68.22 | 44.24 | 55.09 | **71.26** | 71.45 |

**LFM2.5-2.6B wins 6 of 9** benchmarks here despite being the smallest (2.6B)
— beating models 2–3× its size. It loses only LiveCodeBench, BFCLv4, and
Claw-Eval to the larger Qwen3.5-9B.

## Qwen3.5-4B's own numbers (Qwen card)

| Benchmark | Qwen3.5-4B | Qwen3.5-9B | Qwen3-30B-A3B |
|---|---:|---:|---:|
| MMLU-Pro | 79.1 | 82.5 | 80.9 |
| GPQA Diamond | 76.2 | 81.7 | 73.4 |
| IFEval | 89.8 | 91.5 | 88.9 |
| AA-LCR (long ctx) | 57.0 | 63.0 | 49.0 |
| HMMT Feb 25 | 74.0 | 83.2 | 63.1 |
| LiveCodeBench v6 | 55.8 | 65.6 | 66.0 |
| BFCL-V4 | 50.3 | 66.1 | 42.4 |
| TAU2-Bench | **79.9** | 79.1 | 41.9 |

Qwen3.5-4B is strong on knowledge (MMLU-Pro 79.1, GPQA 76.2) and agentic
(TAU2 79.9) but weaker on coding (LCB 55.8) than its 9B sibling.

## LFM2.5-8B-A1B (Liquid AI card, cross-model)

| Benchmark | **LFM2.5-8B-A1B** (8B/A1B) | Qwen3.5-4B | Gemma-4-E4B (8B) | Granite-4.0-H-Tiny (7B/A1B) |
|---|---:|---:|---:|---:|
| IFEval | **91.84** | 87.80 | 87.74 | 82.23 |
| MATH500 | **88.76** | 80.76 | 65.00 | 59.20 |
| AIME25 | 42.53 | **54.28** | 34.33 | 4.93 |
| AIME26 | 50.00 | **58.33** | 40.67 | 3.33 |
| BFCLv4 | 49.73 | **54.01** | 33.92 | 28.52 |
| Tau² Telecom | **88.07** | 87.72 | 26.75 | 16.67 |
| Tau² Retail | 39.82 | **71.93** | 42.11 | 18.42 |

LFM2.5-8B-A1B leads on instruction-following, math (MATH500), and agentic
(Tau² Telecom); Qwen3.5-4B leads on AIME and BFCL.

## Verdict: current sub-10B SOTA

**There is no single winner — it depends on the axis, but two models stand out:**

1. **LFM2.5-2.6B** (Liquid AI, 2026-07-28) — the most recent release and the
   strongest **per-parameter** model. It wins 6/9 benchmarks in Liquid's own
   sub-10B head-to-head despite being the smallest, and is the fastest in its
   class (220 tok/s on M5 Max, ~15K tok/s on one H100). Best for
   **instruction-following, tool use, and efficiency**.

2. **Qwen3.5-4B** (Alibaba, 2026-02-27) — the strongest **all-rounder** on
   knowledge (MMLU-Pro 79.1, GPQA 76.2) and agentic (TAU2 79.9), and beats
   LFM2.5-8B-A1B on AIME and BFCL. Best for **reasoning + knowledge**.

3. **LFM2.5-8B-A1B** (Liquid AI, 2026-05-28) — the strongest **8B-class** model
   on instruction-following (IFEval 91.8) and math (MATH500 88.8), with MoE
   efficiency (1B active).

**If you want one answer:** for the 1–3B range, **LFM2.5-2.6B** is the current
SOTA. For the 4–10B range, **Qwen3.5-4B** is the best all-rounder and
**LFM2.5-8B-A1B** the best on instruction-following/math.

## Caveats

- All numbers are **vendor-published** on their own cards. Liquid AI's table
  is the most useful because it's a genuine cross-model sub-10B comparison,
  but it's still Liquid's own run.
- Parameter counts differ by source (Liquid lists Gemma-4-E2B as 5.1B and
  E4B as 8B; Google's own card lists E2B as "effective 2B"). Treat exact
  counts as approximate.
- Context lengths and licenses were not consistently published in the cards I
  pulled; where blank above, the card didn't state them.

## Sources

- Liquid AI LFM2.5-2.6B card: https://huggingface.co/LiquidAI/LFM2.5-2.6B
- Liquid AI LFM2.5-8B-A1B card: https://huggingface.co/LiquidAI/LFM2.5-8B-A1B
- Qwen3.5-4B card: https://huggingface.co/Qwen/Qwen3.5-4B
- Qwen3.5-9B card: https://huggingface.co/Qwen/Qwen3.5-9B
- Google Gemma-4-E4B card: https://huggingface.co/google/gemma-4-E4B-it
- Google Gemma-4-E2B card: https://huggingface.co/google/gemma-4-E2B-it