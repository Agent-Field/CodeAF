# Toward a Backprop Replacement: Theory & Experimental Log

Working question: can we train a deep net with **drastically fewer flops than
backpropagation**, by rethinking what the gradient *is*?

## The hard constraint (shapes everything)

**Baur–Strassen (1983):** the exact gradient of a scalar costs ≥ ~1 forward
pass (and ≤ ~5).  So *no exact-gradient method beats backprop on flops.*
Every honest flop-win must come from giving up exactness.  The game is to find
the **cheapest sufficient gradient estimate**, not a cheap exact gradient.

## The five levers (what you may sacrifice)

1. **Precision → stochastic gradient.**  You need a descent direction, not the
   gradient.  (SGD itself; random sketching; zeroth-order.)
2. **Global optimality → local losses.**  Per-layer objectives kill the global
   chain (Forward-Forward, greedy InfoMax).  A parallelism win, not fewer flops.
3. **Discrete time → equilibrium/ODE.**  Backprop is the discrete adjoint;
   equilibrium nets (EqProp) get the gradient from two settles, no backward pass.
4. **Naive solver → structured solve.**  The equilibrium is a linear system;
   solve it directly instead of iterating.
5. **Digital model → physics.**  Fluctuation-dissipation: a physical system
   reads its own gradient from thermal noise.  ~Zero digital flops.

## Experimental log (this repo)

Testbed: 3-layer tanh MLP, dims (32,64,16), regression, 512 samples.
`research/sela.py`, `research/dsel.py`.

### ✅ RESULT 1 — partial-settle equilibrium learning (the real win)
Equilibrium Propagation (SELA) trains with **no backward pass** (two forward
settles, local updates).  Run the settle to only **3 steps** (not convergence)
→ a cheap, noisy gradient.  Measured, 3 seeds:

| | backprop | SELA (settle=3) |
|---|---|---|
| total GFLOP to loss<0.05 | 0.57–0.60 | **0.21–0.23** |
| ratio | 1.0× | **0.35–0.40× (2.5–2.9× cheaper)** |

Mechanism: truncated settle = biased-but-cheap gradient; the noise acts like
SGD's minibatch noise (implicit exploration/regularization), so it converges
in *fewer* steps.  **This is the one reproducible flop win.**  Novel framing:
*truncated-equilibrium learning as a stochastic-gradient method.*

### ❌ REFUTED 1 — direct-solve equilibrium (block-Thomas)
The equilibrium fixed-point map is exactly block-tridiagonal (verified:
dF_l/ds_{l+2}=0).  Idea: linearize, solve A s = b directly in O(L d^2).
**Killed by dimensions:** the equilibrium is *per-sample*, so the Jacobian is
per-sample → O(n·L·d³).  Since width d=64 > settle=30, the direct solve is
**2.1× MORE expensive** than iterating.  Batch-averaged Jacobian (to dodge d³)
diverges (residual 47→6558 over Newton steps).  Dead for wide nets.

### ❌ REFUTED 2 — low-rank gradient sketching
Measured the true gradient spectrum: needs rank 15/32 and 12/16 for 90%
energy.  Not low-rank on this small testbed.  The structure only emerges in
*large* overparameterized nets.  Weak lever here.

## Open directions (the frontier)

- **Scale test:** does the 2.5× survive MNIST / deeper nets?  (decides if it's
  a contribution or a curiosity)
- **Optimal settle depth:** why 3?  Bias-variance optimum (settle error vs
  gradient noise) — characterize and pick adaptively.
- **Learned/surrogate gradients** (see below).

---

## RESULT 2 — gradient-field caching with trust-region refresh (strongest)

**Reframe:** the gradient ∇L(θ) is not a point to recompute each step — it is a
smooth *vector field* over parameter space.  Measured on the testbed: the true
gradient keeps cosine similarity > 0.7 for **40 steps** along the training
trajectory (the field is smooth / the trajectory is a slow manifold).  So a
*cached* gradient stays a valid descent direction for many steps.

**Algorithm (Gradient-Field Caching + Trust-Region Refresh):**
1. Compute the true gradient (backprop) — an *anchor*.
2. Reuse it for several Adam steps.
3. **Trust-region test:** keep the cached gradient only while it keeps
   decreasing the loss.  When it stalls (loss stops dropping), refresh with a
   new true gradient.  This is the analytical guarantee — no blind faith.

**Measured (3 seeds, dims (32,64,16)):**

| target loss | backprop GFLOP | adaptive-cache | speedup | true-grads |
|---|---|---|---|---|
| 0.05  | 0.588 | 0.075 | **7.8×** | 8  |
| 0.01  | 2.101 | 0.462 | **4.5×** | 49 |
| 0.005 | 3.246 | 0.812 | **4.0×** | 86 |

Converges at every precision.  The trust-region test automatically uses few
gradients early (smooth field) and more late (curvature matters).  Fixed-k
caching breaks at high precision (k=20 fails to reach 0.01); adaptive does not.

**Why this is the real thing:** it is the only lever that (a) survives
Baur–Strassen (it amortizes the exact gradient over many steps), (b) has an
analytical convergence guarantee (the loss-decrease test), and (c) is *simple*.
It beats partial-settle SELA (2.5×) and needs no equilibrium machinery.

**Caveats (honest):** toy regression testbed; the 4–8× could shrink on a hard,
high-curvature task where the gradient field is rougher.  Adam's second moment
is doing some of the work between anchors.  Scale test is the next gate.

**Next:** (a) scale to MNIST/deeper nets; (b) replace the cached *point* with a
learned *extrapolator* (Koopman / slow-manifold model of the gradient field) to
push the refresh interval further; (c) characterize the refresh rate vs the
local curvature (should track the Hessian's top eigenvalue).
