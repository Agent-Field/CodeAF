# Small models (0.5–1B) — SOTA survey, mid-2026

Window: last ~3 months (mid-May → mid-Aug 2026). The only genuinely new base
release in that window is **MiniCPM5-1B** (2026-05-21). The other two current
small-model leaders — **Qwen3.5-0.8B** and **Gemma-4-E2B** — are from Feb/Mar
2026, just outside the window but still the ones to know. All numbers are the
vendors' own published benchmark tables.

## The three current leaders

| | **MiniCPM5-1B** | **Qwen3.5-0.8B** | **Gemma-4-E2B** |
|---|---|---|---|
| Developer | OpenBMB | Alibaba Qwen | Google |
| Released | 2026-05-21 | 2026-02-28 | 2026-03-02 |
| Params | 1.08B (679M non-emb) | 0.8B | ~2B (E2B = effective 2B) |
| Context | 131K | — | — |
| License | Apache-2.0 | Apache-2.0 | — |
| Architecture | LlamaForCausalLM (dense) | Gated Delta + sparse MoE, vision | — |
| Claim | "1B-class open-source SOTA" | current 0.8B SOTA | — |

## Benchmark numbers (vendor-published)

### MiniCPM5-1B vs its cited baselines (from OpenBMB leaderboard image)

| Benchmark | MiniCPM5-1B (think) | Qwen3-0.6B (think) | Qwen3.5-0.8B (think) | LFM2.5-1.2B (think) |
|---|---|---|---|---|
| **Average** | **42.57** | 26.77 | 25.14 | 35.61 |
| MMLU-Pro | 48.85 | 35.63 | 42.74 | 47.98 |
| MMLU-Redux | 70.06 | 55.47 | 61.50 | 66.08 |
| GPQA-Diamond | 26.26 | 25.42 | 30.98 | 34.85 |
| SuperGPQA | 23.14 | 20.79 | 22.92 | 22.83 |
| LCB-Pro 2502 (Easy) | 22.68 | 4.12 | 0.00 | 6.19 |
| OJBench | 7.33 | 0.86 | 0.43 | 1.94 |
| LCB-V6 (@Avg3) | 33.52 | 16.00 | 5.33 | 21.33 |
| IFBench | 46.67 | 25.67 | 29.33 | 41.67 |
| IFEval | 80.41 | 59.89 | 59.89 | 84.84 |
| Multi-IF | 43.54 | 36.56 | 32.31 | 55.61 |
| MultiChallenge | 19.48 | 18.97 | 23.97 | 23.28 |
| AIME-2025 (@Avg16) | 40.42 | 16.25 | 1.04 | 31.88 |
| AIME-2026 (@Avg16) | 40.42 | 12.29 | 0.21 | 31.67 |
| HMMT Feb 2026 (@Avg16) | 25.76 | 9.85 | 0.57 | 21.21 |
| MATH-500 | 91.60 | 72.60 | 30.40 | 89.00 |
| BBH | 71.89 | 47.86 | 54.58 | 57.32 |
| BBEH | 12.14 | 3.78 | 8.53 | 8.64 |
| BFCLv4 | 25.15 | 25.43 | 25.53 | 10.60 |
| t2-Bench Telecom-AA | 79.53 | 21.10 | 47.70 | 19.60 |

### Qwen3.5-0.8B (from Qwen card, non-thinking unless noted)

| Benchmark | Qwen3.5-0.8B | Qwen3.5-2B | Qwen3-1.7B |
|---|---|---|---|
| MMLU-Pro | 29.7 | 55.3 | 40.2 |
| MMLU-Redux | 48.5 | 69.2 | 64.4 |
| C-Eval | 46.4 | 65.2 | 61.0 |
| SuperGPQA | 16.9 | 30.4 | 21.0 |
| IFEval | 52.1 | 61.2 | 68.2 |
| MMMLU | 34.1 | 56.9 | 46.7 |
| MMLU-Pro (thinking) | 42.3 | 66.5 | 56.5 |
| GPQA (thinking) | 11.9 | 51.6 | 40.1 |
| AA-LCR (long ctx) | 4.7 | 25.6 | 6.7 |

### Gemma-4-E2B (from Google card)

| Benchmark | Gemma-4-E2B | Gemma-4-E4B | Gemma-3-27B (no think) |
|---|---|---|---|
| MMLU Pro | 60.0% | 69.4% | 67.6% |
| AIME 2026 (no tools) | 37.5% | 42.5% | 20.8% |
| LiveCodeBench v6 | 44.0% | 52.0% | 29.1% |
| GPQA Diamond | 43.4% | 58.6% | 42.4% |
| MMMLU | 67.4% | 76.6% | 70.7% |
| Tau2 (avg 3) | 24.5% | 42.2% | 16.2% |
| BigBench Extra Hard | 21.9% | 33.1% | 19.3% |

## Reading

- **MiniCPM5-1B is the current 1B-class SOTA** and the only major new release
  in the last 3 months. Its average (42.57) beats Qwen3.5-0.8B (25.14) and
  Qwen3-0.6B (26.77) by a wide margin, and edges LFM2.5-1.2B (35.61). Its
  biggest wins are in **math** (MATH-500 91.6, AIME 40.4), **coding** (LCB-V6
  33.5), and **agentic tool use** (t2-Bench 79.5). It trails LFM2.5-1.2B only
  on instruction-following (IFEval 80.4 vs 84.8, Multi-IF 43.5 vs 55.6).
- **Qwen3.5-0.8B** is the current 0.8B SOTA but is ~5 months old and notably
  weaker on reasoning (GPQA 11.9 thinking) and long context (AA-LCR 4.7).
- **Gemma-4-E2B** is a strong all-rounder (best GPQA 43.4 of the three) but is
  ~2B effective, not really in the 0.5–1B class.

## Caveats

- All numbers are **vendor-published** on their own model cards / leaderboards,
  not an independent head-to-head. MiniCPM5-1B's table is from an image (OCR'd
  here); Qwen3.5-0.8B and Gemma-4-E2B from their card HTML tables.
- The "SOTA" claims are against the specific baselines each vendor chose, not a
  universal ranking.
- BenchLM's independent composite ranks MiniCPM5-1B low overall (#215/215,
  score 12.8) — but that index is dominated by frontier-model benchmarks
  (Terminal-Bench, GDPval, SciCode) where a 1B model cannot compete; within its
  own size class it is competitive. Treat the composite and the vendor tables
  as answering different questions.