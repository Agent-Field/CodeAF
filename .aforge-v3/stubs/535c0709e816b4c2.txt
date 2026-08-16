# NVIDIA Corporation (NASDAQ: NVDA) — Deep Research Report

**Report date:** 2026-08-16 · **Most recent data:** stock price as of 2026-08-14 close; financials through Q1 FY2027 (quarter ended 2026-04-26). All figures are USD. Every key number is cited to a dated web source.

---

## 1. Snapshot

| Metric | Value | Source / date |
|---|---|---|
| Price (close) | **$225.16** (−0.06% on the day) | NVIDIA investor relations stock quote, 2026-08-14 |
| Market cap | **$5.45 trillion** | stockanalysis.com, 2026-08-14 |
| Enterprise value | $5.41 trillion | stockanalysis.com, 2026-08-14 |
| 52-week range | **$164.07 – $236.54** | NVIDIA IR / CNBC, 2026-08-14 |
| Shares outstanding | 24.22 billion | stockanalysis.com, 2026-08-14 |
| 1-year price change | +23.99% | stockanalysis.com, 2026-08-14 |

Sources: NVIDIA investor relations stock quote page (investor.nvidia.com, "August 14, 2026 4:00 PM"); CNBC NVDA quote page (2026-08-14); stockanalysis.com market-cap page (2026-08-14). NVIDIA is the world's most valuable publicly traded company (Investopedia, 2026-05-20).

---

## 2. Business Overview

NVIDIA designs accelerated-computing platforms — GPUs, CPUs, networking, and the CUDA software stack — sold as chips, boards, and rack-scale systems. It is now overwhelmingly a data-center AI company.

**Revenue mix — Q1 FY2027 (quarter ended 2026-04-26), new reporting framework** (NVIDIA Q1 FY27 press release, 2026-05-20; CFO commentary, SEC):

- **Data Center: $75.2B** (+92% YoY, +21% QoQ) — ~92% of revenue. Split roughly 50/50 between **Hyperscale** ($37.9B, +115% YoY) and **AI Clouds, Industrial & Enterprise (ACIE)** ($37.4B, +74% YoY).
- **Edge Computing: $6.4B** (+29% YoY) — PCs, game consoles, workstations, robotics, automotive, AI-RAN. Gaming is no longer broken out separately; it now sits inside Edge Computing (Digital Citizen, 2026-05-21).
- Under the old sub-markets, Data Center **compute** was $60.4B (+77% YoY) and Data Center **networking** was $14.8B (+199% YoY) (NVIDIA Q1 FY27 release).

**FY2026 (fiscal year ended 2026-01-25):** Data Center was $193.7B of $215.9B total — **90% of revenue** (mungomash.com, from EDGAR XBRL; Yahoo Finance, 2026-02-26). Gaming was ~$16B for the year (PC Gamer, 2026-02-26). Data Center has gone from ~20% of revenue in FY2018 to ~90% in FY2026 (Lambda Finance, from 10-K filings).

**Key products and roadmap:**

- **Blackwell** (current generation): the GB200/GB300 "Blackwell 300" systems drove Q1 FY27 data-center growth (NVIDIA CFO commentary, 2026-05-20). Grace Blackwell with NVLink is described by CEO Jensen Huang as "the king of inference today" (NVIDIA Q4 FY26 release, 2026-02-25).
- **Rubin** (next generation): announced 2026-01-05 at CES. Six co-designed chips — Vera CPU, Rubin GPU, NVLink 6 Switch, ConnectX-9 SuperNIC, BlueField-4 DPU, Spectrum-6 Ethernet switch. Promises up to **10× lower inference token cost** and **4× fewer GPUs** to train MoE models vs. Blackwell (NVIDIA Newsroom, 2026-01-05). Vera Rubin is in full production with shipments beginning in Q3 FY2027 (Hudson Labs preview, 2026-08-04). UBS expects Rubin shipments to accelerate toward **500,000 GPUs/month** as Blackwell steps aside (Aliteq/UBS, 2026-08-15).
- **CUDA ecosystem:** the software moat that lets developers run the same code across NVIDIA hardware; Huang cites it as the platform that "runs every frontier AI model" and extends into robotics/AV (CNBC earnings coverage, 2026-05-20).
- **Networking:** InfiniBand, Spectrum-X Ethernet, and NVLink — networking revenue grew 199% YoY in Q1 FY27 (NVIDIA release).
- **Vera CPU:** a new standalone CPU opportunity Huang pegs at a **$200B TAM**, expected to contribute ~$20B revenue in FY2027 (Hudson Labs; CNBC, 2026-05-20). Huang says NVIDIA has already sold **$20B of standalone Vera CPUs** this year, positioning it as "the world's first CPU purpose-built for agentic AI" (TechCrunch, 2026-05-21).

**Physical AI (robotics, autonomous vehicles, embodied AI) — the next growth leg.** Huang calls robotics — including self-driving cars — NVIDIA's **second most important growth category after AI** (CNBC, 2026-01-05). The full-stack offering spans DGX systems (Blackwell/Vera Rubin), the Jetson edge robotics platform, Isaac simulation frameworks, Cosmos world models, and Isaac GR00T open models for humanoid robots (Motley Fool, 2026-07-25; NVIDIA GTC release, 2026-03-16).

- **Ecosystem:** at GTC 2026 NVIDIA announced partnerships with ABB, FANUC, KUKA, YASKAWA, Agility, Figure, Medtronic, CMR Surgical, Universal Robots and others building on its platform; FANUC/ABB/YASKAWA/KUKA (a combined install base of 2M+ robots) are integrating Omniverse and Isaac for digital-twin commissioning and Jetson for on-line inference (NVIDIA press release, 2026-03-16).
- **Japan coalition:** in July 2026 Huang signed Toyota, Fujitsu, Kawasaki Heavy Industries, FANUC and Kioxia (among others) into a physical-AI coalition; Kawasaki, FANUC and Yaskawa already use NVIDIA tech (Motley Fool, 2026-07-25).
- **Automotive / robotaxis:** NVIDIA is working with robotaxi operators to power Level-4 fleets with its Drive AV software stack and Drive AGX Thor computer (~$3,500/chip) as soon as 2027, and announced a robotaxi partnership with Uber in October 2025 (CNBC, 2026-01-05). CFO Colette Kress sees "hundreds of billions" in future robotaxi revenue (Investor's Business Daily, 2026-02-26). BYD, Geely, Isuzu and Nissan adopted NVIDIA DRIVE Hyperion for Level-4 vehicles (NVIDIA press release, 2026-03-16). Automotive/robotics was ~1% of revenue (~$592M in the quarter ended Oct 2025) but is the strategic long-term wedge (CNBC, 2026-01-05).
- **Market size:** Barclays Research estimates the global humanoid-robotics market, currently **$2–3B**, could reach **$200B by 2035** under optimistic scenarios (Barclays, 2026-01-14).

**Near-term caveat:** physical AI is still small — the entire Edge Computing segment (robotics, automotive, PCs, consoles) was only **$6.4B** in Q1 FY27 (+29% YoY) vs. $75.2B data center (NVIDIA CFO commentary, 2026-05-20). It is a long-duration option on the next computing transition, not a current earnings driver (Motley Fool, 2026-07-25).

---

## 3. Financials

**Most recent quarter — Q1 FY2027 (ended 2026-04-26)** (NVIDIA press release, 2026-05-20):

- Revenue: **$81.6B**, +85% YoY, +20% QoQ
- Data Center: **$75.2B**, +92% YoY
- Gross margin: **74.9% GAAP / 75.0% non-GAAP**
- Net income: **$58.3B** (+211% YoY); GAAP diluted EPS **$2.39**; non-GAAP EPS **$1.87**
- Beat consensus: revenue $81.62B vs. $78.86B est.; adj. EPS $1.87 vs. $1.76 est. (CNBC, 2026-05-20)

**Prior quarter — Q4 FY2026 (ended 2026-01-25):** revenue $68.1B (+73% YoY), Data Center $62.3B, GAAP GM 75.0%, net income $43.0B, GAAP EPS $1.76 (NVIDIA release, 2026-02-25).

**Full year — FY2026 (ended 2026-01-25)** (NVIDIA release, 2026-02-25):

- Revenue: **$215.9B**, +65% YoY (vs. $130.5B in FY2025)
- Data Center: **$193.7B**, +75% YoY
- Gross margin: **71.1% GAAP / 71.3% non-GAAP** (down from 75.0%/75.5% in FY2025)
- Net income: **$120.1B** (+65%); GAAP EPS **$4.90**; non-GAAP EPS **$4.77**

**Cash flow** (stockanalysis.com cash-flow statement, from filings):

- FY2026 operating cash flow **$102.7B**; capex $6.0B → **free cash flow ≈ $96.7B**
- TTM (through 2026-04-26): operating cash flow **$125.6B**; capex $6.6B → **FCF ≈ $119B**
- Balance sheet: $53.2B cash, $12.8B debt, net cash ~$40.4B (stockanalysis.com statistics)

**Capital returns:** returned ~$20B to shareholders in Q1 FY27; added **$80B** to buyback authorization (now no expiration) and raised the quarterly dividend from $0.01 to **$0.25/share** (NVIDIA release, 2026-05-20). Returned $41.1B in FY2026 (NVIDIA release, 2026-02-25).

**Guidance — Q2 FY2027 (reports 2026-08-26):** revenue **$91B** (±2%), gross margin 74.9% GAAP / 75.0% non-GAAP (±50 bps) (Hudson Labs, 2026-08-04). Consensus revenue $91.8B, EPS $2.06–2.08 (Hudson Labs; TickerLeague). Implied YoY growth ~+94–96% (Hudson Labs).

---

## 4. Valuation

As of 2026-08-14 (stockanalysis.com statistics page):

- **Trailing P/E: 34.5×** (TTM EPS $6.53)
- **Forward P/E: 22.6×** (on FY2027 non-GAAP EPS consensus ~$8.96)
- **P/S: 21.5×** (trailing); forward P/S 12.5×
- **EV/EBITDA: 32.7×**
- **PEG: 0.51** (low because growth is so high)
- P/FCF: 45.8×; P/B: 27.9×

**History:** NVIDIA's forward P/E has compressed sharply as earnings have exploded — from ~45× (FY2022) to ~25× (FY2027E) on the stockanalysis forecast table. The stock trades at a premium to its own recent history on absolute terms but the PEG is low.

**Peers** (MarketScreener sector table, 2026-08-12; TSMC/Broadcom valuation pages):

| Company | P/E (current) | EV/EBITDA |
|---|---|---|
| **NVIDIA** | ~23× | ~19× |
| **TSMC** | ~22.4× | ~14.8× |
| **Broadcom** | ~46.2× | ~27.8× |
| **AMD** | ~82.9× | ~56.4× |

NVIDIA's forward P/E (~22.6×) is roughly in line with TSMC and well below Broadcom and AMD, reflecting that its earnings have grown into its valuation. Note: NVIDIA's P/E is computed on non-GAAP EPS; GAAP EPS is lower.

---

## 5. Competitive Position and Moat

**The moat is the software + full-stack platform, not just the chip.** NVIDIA's CUDA ecosystem, rack-scale systems (NVL72), NVLink/InfiniBand/Spectrum-X networking, and annual product cadence create switching costs and a "runs every frontier model" position (CNBC, 2026-05-20). Huang frames the platform as "the only platform that runs in every cloud" (NVIDIA release, 2026-05-20).

**Market share:** NVIDIA holds roughly **80–90% of the AI accelerator market by revenue** (Silicon Analysts, 2026-02-21). By unit count, share is eroding faster — from ~95% (2022) to an estimated **62–66% in 2026** — because hyperscaler custom silicon is cheaper per accelerator (Presenc AI, 2026-05). Bloomberg Intelligence projects NVIDIA holds **70–75% of the AI chip market through 2030** (Quartz, 2026-07-09).

**Supply chain:** NVIDIA depends on **TSMC** for leading-edge manufacturing and CoWoS advanced packaging. In March 2026 NVIDIA reallocated TSMC capacity from China-bound H200 chips to Vera Rubin production (Reuters, 2026-03-05). TSMC is also the foundry for most of NVIDIA's custom-silicon rivals (Quartz, 2026-07-09).

**The custom-silicon threat is real and growing.** NVIDIA's own 10-Q acknowledges "some of our customers are developing their own ASICs... optimized for certain workloads" (CNBC, 2026-05-20). In 2026:

- **Google TPU v7** ramping (TSMC N3); ~900k TPU units estimated in 2026
- **AWS Trainium 3** in customer sampling, GA targeted late 2026; Anthropic is the anchor customer; Trainium2 was a multibillion-dollar business growing 150%/quarter (Quartz, 2026-07-09)
- **Microsoft Maia 2** in volume production for OpenAI inference; Maia 200 claims 30% better perf/dollar (Quartz, 2026-07-09)
- **Meta MTIA 2** in volume for ranking/recommendation
- Combined hyperscaler custom silicon ≈ **1.9M accelerators in 2026** (Presenc AI, 2026-05)

The counterweight: custom chips are workload-specific (inference, recommendation), while NVIDIA captures the majority of revenue dollars and the training/frontier-model workloads. NVIDIA's share decline by unit count is "more dramatic than the share decline by revenue" (Presenc AI, 2026-05).

---

## 6. Risks

**China export controls (material, ongoing).** NVIDIA has generated **zero revenue from China chip sales** despite US approval of H200 shipments (CFO Colette Kress, via CNBC, 2026-02-26). The H20 was halted under April 2025 rules; H200 was approved in December 2025 with a 25% US revenue cut, but sales have stalled amid scrutiny in both countries. No Hopper products shipped to China in Q1 FY27, vs. $4.6B in Q1 FY26 (NVIDIA CFO commentary, 2026-05-20). China once accounted for at least one-fifth of data-center revenue (CNBC, 2026-02-26). AP reports Huawei and local chipmakers are taking the lead in China's market (AP, 2026-06-29). Kress warned Chinese rivals "have the potential to disrupt the structure of the global AI industry over the long-term" (CNBC, 2026-02-26).

**Customer concentration.** The top customer was **22% of revenue** and the top two **36%** in FY2026 (mungomash.com, from 10-K). Hyperscalers are both NVIDIA's biggest customers and its most credible future competitors.

**AI capex cyclicality / "digestion" risk.** NVIDIA's growth depends on hyperscaler and neocloud AI infrastructure spending continuing to accelerate. Bears argue capex digestion and valuation compression are near-term risks (Seeking Alpha, 2026-07-01). The $500B financing plan (below) is itself a bet that AI capex keeps growing.

**GPU-as-collateral / financing risk.** NVIDIA struck agreements with BlackRock, Blackstone, Apollo, KKR, Brookfield and Goldman Sachs to mobilize **$500B** to finance data centers and GPU clusters (CNBC, 2026-08-11; NVIDIA blog, 2026-08-12). Analysts warn rapid hardware depreciation — worsened if China floods the market with low-cost compute — could erode the collateral backing these loans; one estimate puts required investor yields at **11–17%** (CNBC, 2026-08-11). BofA notes NVIDIA's ~$70B in ecosystem equity commitments is ~15% of projected two-year FCF, manageable "as long as the revenue assumptions hold" (StockwireX/BofA, 2026-08-09).

**Memory cost / margin pressure.** HBM memory now represents **40–50% of AI hardware build costs**, up from 15–20% historically. BofA models only ~60 bps of gross-margin headwind at the rack level, but up to **500 bps** of dilution at full pod level — product mix is the swing factor (StockwireX/BofA, 2026-08-09). Samsung, SK Hynix, and Micron are redirecting ~89% of DRAM supply to HBM (StockwireX, 2026-08-09).

**Valuation risk.** At $5.45T market cap and ~34× trailing P/E, the stock prices in continued hypergrowth; any slowdown in AI capex or a miss on Aug 26 would hit a high-multiple stock hard. Beta is 2.21 (stockanalysis.com).

**Other:** geopolitical escalation (Huang cited potential "business uncertainty" from an Iran war escalation — CNBC, 2026-05-20); competition from AMD (MI350X/MI450) and Chinese chipmakers.

---

## 7. Catalysts and Headwinds

**Catalysts:**

- **Q2 FY2027 earnings, 2026-08-26** (after close). Guidance $91B; UBS expects a $94–95B beat and an October-quarter guide of **$107–108B** (Aliteq/UBS, 2026-08-15). Consensus revenue $91.8B, EPS ~$2.06–2.08 (Hudson Labs; TickerLeague).
- **Rubin ramp:** Vera Rubin in full production, shipments beginning Q3 FY2027; UBS sees 500k GPUs/month (Aliteq, 2026-08-15). Rubin's 10× inference-cost reduction is the next growth leg (NVIDIA, 2026-01-05).
- **Vera CPU:** a new ~$200B TAM opportunity, ~$20B expected FY2027 revenue (Hudson Labs; CNBC).
- **Physical AI / robotics:** Huang calls it NVIDIA's second-most-important growth category after AI; the Japan industrial coalition (Toyota, FANUC, Kawasaki, Yaskawa), robotaxi push (Uber partnership, Level-4 fleets by 2027), and a potential $200B humanoid market by 2035 are the long-duration upside (CNBC, 2026-01-05; Motley Fool, 2026-07-25; Barclays, 2026-01-14).
- **$500B AI financing platform** with six major asset managers — could fund incremental demand (CNBC, 2026-08-11).
- **Agentic AI / inference demand:** Huang says "demand has gone parabolic" and agentic AI is the driver (CNBC, 2026-05-20). Sovereign AI growing 80%+ YoY (Futurum, 2026-05-22).
- **Capital returns:** $80B buyback + 25× dividend increase (NVIDIA, 2026-05-20).

**Headwinds:**

- China revenue still zero; H200 sales stalled (CNBC, 2026-02-26).
- Custom-silicon share erosion in inference workloads (Presenc AI, 2026-05).
- Memory-cost margin pressure (StockwireX/BofA, 2026-08-09).
- Post-earnings stock slides: the stock fell after Q1 FY27 despite a beat — a fourth straight post-earnings decline (CNBC, 2026-05-20).

---

## 8. Analyst Sentiment

- **Consensus: Strong Buy / Buy.** 61 analysts polled by S&P Global: consensus "Strong Buy," average price target **$302.83** (+34.5% vs. $225.16), range $180–$500 (stockanalysis.com forecast, 2026-08-14). MarketBeat: 53 analysts, average target **$305.94**, range $218–$500 (MarketBeat, 2026-08-14).
- Recent actions (stockanalysis.com, Aug 2026): UBS Buy $280 (08-14), Goldman Sachs Buy $285 (08-12), Susquehanna Buy $275 (08-12), Wells Fargo Buy $315 (08-12), RBC Buy $300 (08-11).
- **Bull thesis:** Blackwell demand "steady, not decelerating"; Rubin ramping faster than modeled; UBS sees a $3–4B beat and $107–108B October guide (Aliteq, 2026-08-15). Huang's $1T Blackwell+Rubin order forecast through 2027 (SiliconReport, 2026-03-18). CNBC Investing Club raised its price target after Q1 FY27 (CNBC, 2026-05-20).
- **Bear thesis:** valuation compression and capex digestion risk (Seeking Alpha, 2026-07-01); memory-cost margin dilution up to 500 bps at pod level (StockwireX/BofA, 2026-08-09); correlated downside if AI partners' financial health deteriorates — pressuring both chip demand and NVIDIA's ~$70B equity portfolio (StockwireX/BofA, 2026-08-09); China flooding the market with low-cost compute could crash GPU collateral values (CNBC, 2026-08-11).

---

## 9. Balanced Outlook

**Bull case:** NVIDIA is the dominant, full-stack platform for the largest infrastructure buildout in history. Revenue grew 85% YoY in Q1 FY27 with ~75% gross margins, ~$119B TTM free cash flow, and a net-cash balance sheet. The Rubin/Vera roadmap extends the moat into inference and CPUs, and the $500B financing platform plus $80B buyback support demand and shareholder returns. Physical AI — robotics, robotaxis, and the Vera CPU for agentic workloads — is a long-duration second growth engine beyond data center (CNBC, 2026-01-05; Motley Fool, 2026-07-25). At ~22.6× forward earnings — below Broadcom and AMD — the stock is not obviously expensive if growth continues.

**Bear case:** The business is a single-cylinder engine — ~90% data-center revenue, top-two customers at 36% of revenue, and zero China revenue. The biggest customers are building their own chips. Memory costs threaten margins, AI capex is cyclical, and the GPU-as-collateral financing model is untested against a China price war. At $5.45T, the market cap prices in years of uninterrupted hypergrowth; a single miss on Aug 26 would hit a 2.2-beta stock hard.

**What would change the thesis:**
- **Bullish confirmation:** a Q2 FY27 beat with a $107B+ October guide, evidence Rubin is ramping ahead of plan, and stable/expanding gross margins despite memory costs.
- **Bearish confirmation:** a Q2 miss or weak guide; gross-margin compression toward the 500-bps pod-level scenario; hyperscalers shifting meaningful training (not just inference) workloads to custom silicon; China flooding the market with cheap compute and eroding GPU collateral values; or a slowdown in hyperscaler AI capex.

**Synthesis:** NVIDIA's fundamentals are extraordinary and its moat is real, but the stock's valuation and the concentration of its growth in a single, geopolitically exposed, competitively contested segment mean the risk is concentrated too. Physical AI is the most credible path to a second growth engine, but it is still ~8% of revenue today and years from material contribution — it is an option on the future, not a current earnings driver. The next decisive data point is the August 26, 2026 earnings report and its October-quarter guidance.

---

*Sources: NVIDIA investor relations and newsroom press releases (2026-01-05, 2026-02-25, 2026-03-16, 2026-05-20); SEC filings (Q1 FY27 CFO commentary, FY2026 10-K); CNBC (2026-01-05, 2026-02-26, 2026-05-20, 2026-05-23, 2026-08-11); Reuters (2026-03-05); AP (2026-06-29); stockanalysis.com (2026-08-14); MarketBeat (2026-08-14); MarketScreener (2026-08-12); Hudson Labs (2026-08-04); Aliteq/UBS (2026-08-15); Presenc AI (2026-05); Quartz (2026-07-09); Silicon Analysts (2026-02-21); StockwireX/BofA (2026-08-09); mungomash.com (EDGAR XBRL); Lambda Finance; Digital Citizen (2026-05-21); PC Gamer (2026-02-26); Futurum (2026-05-22); SiliconReport (2026-03-18); TechCrunch (2026-05-21); Motley Fool (2026-07-25); Barclays (2026-01-14); Investor's Business Daily (2026-02-26); Automotive News (2026-03-18).*