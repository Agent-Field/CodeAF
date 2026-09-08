# pin experiment — arm means (n per arm = 8)

| arm | passes | cost $ | wall s | redemand | ttft med ms | visible lines | excluded |
|---|---|---|---|---|---|---|---|
| A | 0/8 | 0.0001 | 902 | 2.1 | 1435 | 1.0 | 0 |
| B | 8/8 | 0.0356 | 668 | 3.2 | 1488 | 0.8 | 0 |
| C | 8/8 | 0.0271 | 598 | 3.0 | 1213 | 0.6 | 0 |
| N | 8/8 | 0.0288 | 688 | 0.0 | 1378 | 0.0 | 0 |

noise band (mean |replicate difference|): cost 0.0105, wall 109, quality 0.00

Pareto front (minimise cost, wall, 1-quality; ties inside the band): A, B, C, N
Tiebreak order (quality, cost, wall): C, N, B, A

## Cells

| id | arm | brief | rep | pass | reason | cost | wall | redemand | ttft first | visible | unclosed |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1b57877a3a | A | Human-Agent-Society-reef-145 | 1 | 0 | wall / red | 0.0001 | 902 | 2 | 1808 | 1 | 0 |
| 68c038cb44 | A | Human-Agent-Society-reef-145 | 2 | 0 | wall / red | 0.0001 | 903 | 2 | 1526 | 1 | 0 |
| 43c455ed9b | B | Human-Agent-Society-reef-145 | 1 | 1 | wall / green | 0.0425 | 901 | 3 | 573 | 1 | 50 |
| fcd2e6e48d | B | Human-Agent-Society-reef-145 | 2 | 1 | ok / green | 0.0220 | 577 | 4 | 828 | 0 | 26 |
| b4c5c1b3f7 | C | Human-Agent-Society-reef-145 | 1 | 1 | wall / green | 0.0525 | 902 | 7 | 612 | 0 | 64 |
| 151c89cd30 | C | Human-Agent-Society-reef-145 | 2 | 1 | ok / green | 0.0252 | 452 | 4 | 534 | 0 | 41 |
| 4835a3b6fc | N | Human-Agent-Society-reef-145 | 1 | 1 | ok / green | 0.0460 | 714 | 0 | 334 | 0 | 70 |
| e13be0103b | N | Human-Agent-Society-reef-145 | 2 | 1 | wall / green | 0.0218 | 903 | 0 | 527 | 0 | 39 |
| 43a638a550 | A | pallets-click-3740 | 1 | 0 | wall / red | 0.0001 | 903 | 2 | 2160 | 1 | 0 |
| 0f0ebcea38 | A | pallets-click-3740 | 2 | 0 | wall / red | 0.0001 | 903 | 2 | 1830 | 1 | 0 |
| afb7e903bc | B | pallets-click-3740 | 1 | 1 | ok / green | 0.0066 | 135 | 1 | 590 | 0 | 0 |
| 9b00fe72e0 | B | pallets-click-3740 | 2 | 1 | ok / green | 0.0068 | 326 | 2 | 900 | 0 | 15 |
| 8d64be03a4 | C | pallets-click-3740 | 1 | 1 | ok / green | 0.0074 | 133 | 1 | 886 | 0 | 0 |
| 4d2a84a40b | C | pallets-click-3740 | 2 | 1 | ok / green | 0.0053 | 99 | 1 | 666 | 0 | 0 |
| b9b14f5221 | N | pallets-click-3740 | 1 | 1 | ok / green | 0.0069 | 214 | 0 | 642 | 0 | 0 |
| 3d5f19609c | N | pallets-click-3740 | 2 | 1 | ok / green | 0.0045 | 71 | 0 | 950 | 0 | 0 |
| bed059cbad | A | python-attrs-attrs-1416 | 1 | 0 | wall / red | 0.0001 | 901 | 2 | 579 | 1 | 0 |
| a40753e97e | A | python-attrs-attrs-1416 | 2 | 0 | wall / red | 0.0001 | 901 | 2 | 1890 | 1 | 0 |
| 676fd2f3ac | B | python-attrs-attrs-1416 | 1 | 1 | ok / green | 0.0399 | 701 | 3 | 532 | 1 | 27 |
| 2b173a4896 | B | python-attrs-attrs-1416 | 2 | 1 | wall / green | 0.0690 | 903 | 3 | 566 | 2 | 52 |
| d7733921ef | C | python-attrs-attrs-1416 | 1 | 1 | ok / green | 0.0355 | 799 | 3 | 593 | 1 | 62 |
| b71a68febf | C | python-attrs-attrs-1416 | 2 | 1 | ok / green | 0.0203 | 589 | 2 | 529 | 1 | 36 |
| e9f9996059 | N | python-attrs-attrs-1416 | 1 | 1 | wall / green | 0.0424 | 902 | 0 | 595 | 0 | 55 |
| 77bc6318e1 | N | python-attrs-attrs-1416 | 2 | 1 | wall / green | 0.0376 | 902 | 0 | 440 | 0 | 57 |
| 28f3a56faa | A | tox-dev-tox-4031 | 1 | 0 | wall / red | 0.0001 | 901 | 2 | 689 | 1 | 0 |
| 4bf9eecf1f | A | tox-dev-tox-4031 | 2 | 0 | wall / red | 0.0002 | 901 | 3 | 724 | 1 | 0 |
| 67c028ec58 | B | tox-dev-tox-4031 | 1 | 1 | wall / green | 0.0439 | 904 | 7 | 561 | 1 | 27 |
| c5854addfc | B | tox-dev-tox-4031 | 2 | 1 | wall / green | 0.0540 | 901 | 3 | 312 | 1 | 50 |
| 15775ac481 | C | tox-dev-tox-4031 | 1 | 1 | wall / green | 0.0254 | 904 | 3 | 561 | 1 | 56 |
| 7a638f9163 | C | tox-dev-tox-4031 | 2 | 1 | wall / green | 0.0450 | 904 | 3 | 297 | 2 | 48 |
| 13fa8fd8e7 | N | tox-dev-tox-4031 | 1 | 1 | wall / no grade | 0.0293 | 901 | 0 | 318 | 0 | 38 |
| bb3d742840 | N | tox-dev-tox-4031 | 2 | 1 | wall / green | 0.0421 | 901 | 0 | 324 | 0 | 57 |
