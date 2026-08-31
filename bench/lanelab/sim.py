#!/usr/bin/env python3
"""Four routing policies over one real lane sheet, ten thousand requests each.

WHY A SIMULATOR AT ALL. The lane design in ideation/provider-routing.md
proposes to retire a shipped mechanism -- the fixed-threshold strike ledger in
internal/provider/velocity.go -- and replace it with a belief, a Pareto prune,
a per-request scalar and a hedge. That is a lot of machinery to put on the send
path, and the only responsible way to decide whether it earns its place is to
ask what it would have done, against the two things it claims to beat, on
numbers nobody in this repo chose. The sheet is those numbers: OpenRouter's own
thirty-minute percentiles for all seventeen lanes of one model, fetched by
fetch_sheet.py and committed next to this file.

WHAT A SIMULATOR CANNOT DECIDE. It cannot tell you the design works. It can
tell you the design's own arithmetic is or is not self-consistent, it can tell
you which of the four policies the sheet's spread actually separates, and it
can refuse a design cheaply before a live A/B spends money on it. REPORT.md
says where this model is wrong; read that section before quoting any number
from here.

THE HONESTY LAWS THIS FILE IS BUILT ON (bench/README.md):

  ONE PRICE TABLE FOR EVERY ARM. There is exactly one price function,
  `request_price`, it reads the sheet's own `pricing` strings, and all four
  policies are charged through it. A benchmark where the arms price themselves
  is a benchmark that measures its own bookkeeping.

  ONE CAPABILITY GATE FOR EVERY ARM. Tools, output ceiling, context, status and
  five-minute uptime are applied to all four policies, not just to the design's.
  The real router refuses a lane that cannot take a tool call whoever asks it,
  so handing that refusal to the design alone would credit it with a win it did
  not earn. What the design gets to itself is the belief, the prune, the scalar
  and the hedge -- and those have to carry the result on their own.

  JUDGE THE DIFF, NOT A COUNT. Nothing here reports how often a policy "won" a
  request. The comparison is the distribution: p50, p90 and p99 of the wait,
  and dollars per thousand requests, side by side, with the ship gate applied
  to EVERY arm against one named baseline and printed as PASS or FAIL with the
  two quantities that decided it. A policy that wins 70% of requests and loses
  the p90 has lost.

  MEASURE THE WAIT, NOT THE READING. The objective counts only the seconds a
  person spends waiting. It used to count the seconds they spend reading too,
  and since nobody can route around reading, every ratio computed from those
  numbers was a ratio of mostly reading -- which is how a router with no effect
  at all gets reported as a 5% win. See `wait_seconds` below.

  AUTOPSY BEFORE QUOTING. Three findings fell out of running this and all three
  are in REPORT.md rather than smoothed away here: the design's quality gate is
  unreachable from its own prior (Beta(8,1) has mean 0.889 against a q_need of
  0.97) and absorbing once it fires; its price term is worth microseconds
  against a scenario's lambda, so the scalar is pure wait at these token
  counts; and its hedge budget, denominated in wall clock, does not bind
  on the long requests that are expensive to hedge. `--sweep` exists because of
  the second of those: the `work` verdict is not stable across seeds.

Usage:  python3 sim.py [--seed 7] [--n 10000] [--sheet sheets/....json]
                       [--json out.json] [--sweep K]

`--sweep K` re-runs the ship gate over K seeds and reports whether the verdict
is a property of the design or of the seed. It exists because the first run of
this file produced a `work` verdict that reversed between seed 7 and seed 11,
and quoting seed 7 alone would have been a number chosen after the fact.
"""
import argparse
import json
import math
import os
import random
from collections import Counter

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_SHEET = os.path.join(HERE, "sheets", "deepseek-deepseek-v4-flash.json")

# ── THE OBJECTIVE ───────────────────────────────────────────────────────────

# ReadRate, ported from internal/lane/lane.go. Kept here as a bare number with
# the same name so a reader can diff the two by eye.
READ_RATE = 18.0


def wait_seconds(ttft_s, rate, visible, hidden):
    """How long a person WAITS on an answer, in seconds.

    A line-for-line port of `lane.PerceivedSeconds` in internal/lane/lane.go,
    named here for what it measures rather than for the Go symbol, because the
    whole point of the correction below is that this is a WAIT and not a
    duration. If the simulator and the chooser disagree about the objective,
    the simulator is measuring a design that was never proposed.

    Hidden tokens -- reasoning, tool-call JSON, anything a person never reads
    -- are worth their full rate, because every one of them is pure waiting.

    THE VISIBLE TERM IS THE WAIT AND NOT THE READING. Text a person reads as it
    arrives costs them the time it takes to read it no matter which lane wrote
    it: visible/ReadRate seconds, and NO ROUTER CAN REMOVE IT. What a router
    can remove is the part of the wait where the reader has caught up with the
    writer, which is the difference between the two rates and nothing else. A
    lane at or above the reading rate therefore contributes no visible wait at
    all, and two lanes above it are the SAME SPEED to the person.

    THE CORRECTION THIS FILE CARRIES. The objective used to end in
    `visible / min(rate, READ_RATE)`, which counts the reading. That term is a
    constant -- exactly `visible / READ_RATE` for a lane at or above the
    reading rate, and exactly the same constant on top of the catch-up for a
    lane below it -- so it changed no ranking anywhere. What it did was put a
    22.22-second block of reading into every `talk` number, and every RATIO
    taken from those numbers was then a ratio of mostly reading. The `talk`
    p90 that used to read as a 0.5% win over the strike ledger is a 5.9% win
    on the wait alone; the win over the router's default goes from 65.6% to
    96.3%. Neither policy moved. Only the arithmetic did.
    """
    if rate <= 0:
        return float("inf")
    return ttft_s + hidden / rate + visible * max(0.0, 1.0 / rate - 1.0 / READ_RATE)


def wall_seconds(ttft_s, rate, visible, hidden):
    """Clock time, for the reader who wants to see what the ceiling hides."""
    if rate <= 0:
        return float("inf")
    return ttft_s + (visible + hidden) / rate


# ── THE REQUEST SHAPE ───────────────────────────────────────────────────────

# One number, one place. Every request in every scenario reads a 4000-token
# prompt; the scenarios differ only in what the answer is made of.
PROMPT_TOKENS = 4000

SCENARIOS = [
    # name       lambda  visible  hidden  q_need  tools
    ("talk",     90.0,   400,     0,      0.90,   False),
    ("work",     90.0,   0,       2000,   0.97,   True),
    ("offpath",  0.0,    0,       2000,   0.97,   True),
]

SCENARIO_WHY = {
    "talk": "a person is watching the stream",
    "work": "critical-path tool loop, nobody reads the tokens",
    "offpath": "background, nobody is waiting, price wins outright",
}

# ── THE GENERATIVE MODEL ────────────────────────────────────────────────────

Z90 = 1.2816            # z(0.90); the sheet gives p50 and p90, so this fits sigma
Z75 = 0.6745            # z(0.75); the prune compares lanes here, not at the mean

# A lane whose p90 is not above its p50 -- a lane the router saw twice, or a
# lane whose percentiles collided -- would fit sigma = 0 or negative, and a
# zero-variance belief is a claim of certainty nobody earned. Floor it instead:
# 0.15 in the log domain is about a +/-16% spread, which is the least surprise
# any real endpoint has ever shown.
SIGMA_FLOOR = 0.15

# THE TAIL IS THE POINT. CoreWeave is the fastest lane on this sheet at the
# median (587 ms) and one of the worst at the ninety-ninth percentile (12.5 s).
# A single log-normal fitted to p50 and p90 cannot produce that: it would put
# the p99 at about 3 s and quietly make CoreWeave the obvious answer. So a draw
# is a MIXTURE -- 99% of the time the body, 1% of the time a component whose
# median is the sheet's own p99. That one percent is what a person remembers,
# and pricing it is the whole reason the design scores the tail.
TAIL_P = 0.01
TAIL_SIGMA = 0.35

# Rate gets the mirror treatment with one asymmetry: a lane that occasionally
# writes FASTER than its p90 hurts nobody, so there is no fast tail. What does
# hurt is a lane that occasionally crawls, so rate carries a 1% slow component
# centred at a third of its median.
RATE_SLOW_DIV = 3.0
RATE_FLOOR = 1.0

# True accept rates, assumed from the sheet's quantization field. This is the
# softest assumption in the file and it is called out in REPORT.md: nobody has
# measured these, they are a plausible ordering dressed as numbers.
TRUE_ACCEPT = {"fp4": 0.85, "unknown": 0.95}
TRUE_ACCEPT_DEFAULT = 0.985

MAX_ATTEMPTS = 3        # a refused answer is retried on the next lane in order


class Lane:
    """One row of the sheet, plus the fit the simulator draws from.

    Nothing here is renamed on the way in. `p_in`, `p_out` and `p_cache` are
    dollars PER TOKEN, which is the unit the router publishes; the only
    conversion in this file is the float() on the router's decimal strings.
    """

    def __init__(self, e):
        self.name = e.get("provider_name") or "?"
        self.quant = (e.get("quantization") or "unknown").lower()
        self.context = int(e.get("context_length") or 0)
        self.max_out = int(e.get("max_completion_tokens") or 0)
        self.status = int(e.get("status") or 0)
        self.up5 = float(e.get("uptime_last_5m") or 0.0)
        self.up30 = float(e.get("uptime_last_30m") or 0.0)
        self.caches = bool(e.get("supports_implicit_caching"))
        self.tools = bool((e.get("supports_tool_choice") or {}).get("function"))

        pr = e.get("pricing") or {}
        self.p_in = float(pr.get("prompt") or 0.0)
        self.p_out = float(pr.get("completion") or 0.0)
        cache = pr.get("input_cache_read")
        self.p_cache = float(cache) if cache not in (None, "") else self.p_in

        lat = e.get("latency_last_30m") or {}
        thr = e.get("throughput_last_30m") or {}
        self.ttft_p50 = float(lat.get("p50") or 0.0)
        self.ttft_p90 = float(lat.get("p90") or 0.0)
        self.ttft_p99 = float(lat.get("p99") or 0.0)
        self.rate_p50 = float(thr.get("p50") or 0.0)
        self.rate_p90 = float(thr.get("p90") or 0.0)

        self.ttft_mu, self.ttft_sigma = _fit(self.ttft_p50, self.ttft_p90)
        self.rate_mu, self.rate_sigma = _fit(self.rate_p50, self.rate_p90)
        self.ttft_tail_mu = math.log(max(self.ttft_p99, self.ttft_p50, 1.0))
        self.rate_slow_mu = math.log(max(self.rate_p50 / RATE_SLOW_DIV, RATE_FLOOR))
        self.true_accept = TRUE_ACCEPT.get(self.quant, TRUE_ACCEPT_DEFAULT)

    def known(self):
        """A lane the sheet published no timing for cannot be simulated, and
        inventing one would be this file refusing or preferring a lane on a
        number nobody measured."""
        return self.ttft_p50 > 0 and self.rate_p50 > 0

    def draw_ttft_ms(self, rng):
        if rng.random() < TAIL_P:
            return math.exp(self.ttft_tail_mu + TAIL_SIGMA * rng.gauss(0, 1))
        return math.exp(self.ttft_mu + self.ttft_sigma * rng.gauss(0, 1))

    def draw_rate(self, rng):
        if rng.random() < TAIL_P:
            r = math.exp(self.rate_slow_mu + TAIL_SIGMA * rng.gauss(0, 1))
        else:
            r = math.exp(self.rate_mu + self.rate_sigma * rng.gauss(0, 1))
        return max(RATE_FLOOR, r)


def _fit(p50, p90):
    """mu = ln p50, sigma = (ln p90 - ln p50) / z(0.90), sigma floored."""
    if p50 <= 0:
        return 0.0, SIGMA_FLOOR
    mu = math.log(p50)
    if p90 <= p50:
        return mu, SIGMA_FLOOR
    return mu, max(SIGMA_FLOOR, (math.log(p90) - mu) / Z90)


# ── ONE PRICE TABLE, READ BY ALL FOUR POLICIES ──────────────────────────────

def request_price(lane, visible, hidden, cached=False):
    """Dollars for one request on one lane.

    THE ONLY PRICE FUNCTION IN THIS FILE. `cached` is the cache-aware term from
    Part II section 3: a lane that already holds our prefix charges the cache
    tariff for the prompt. On the committed sheet exactly one lane advertises
    `supports_implicit_caching` (Azure) and it is refused by the status gate, so
    this branch never fires here -- which is itself worth knowing, and is the
    reason "the simulator cannot see prompt-cache effects" is in the report's
    limitations rather than in its results.
    """
    p_in = lane.p_cache if (cached and lane.caches) else lane.p_in
    return p_in * PROMPT_TOKENS + lane.p_out * (visible + hidden)


# ── ONE CAPABILITY GATE, APPLIED TO ALL FOUR POLICIES ───────────────────────

def capability_gate(lanes, scen):
    """Deterministic, never sampled. A lane that would drop the tool call is a
    wrong answer rather than a fast one, so it leaves the set before anything
    is drawn.

    Given to every arm on purpose. OpenRouter itself will not route a
    tool-carrying request to an endpoint without tool support, so a baseline
    that did would be a strawman, and the ship gate would then be measuring the
    strawman rather than the design.
    """
    _, _, visible, hidden, _, needs_tools = scen
    out = []
    for L in lanes:
        if not L.known():
            continue
        if L.status != 0:
            continue
        if L.up5 < 95.0:
            continue
        if needs_tools and not L.tools:
            continue
        if L.max_out < visible + hidden:
            continue
        if L.context < PROMPT_TOKENS:
            continue
        out.append(L)
    return out


# ── THE BELIEF: TWO SCALAR KALMAN FILTERS IN THE LOG DOMAIN ─────────────────

HALFLIFE_S = 600.0      # ten minutes, as Posterior.Predict is called in the design
SHEET_R_MULT = 4.0      # k = 4: the sheet is worth a quarter of a real sighting


class Posterior:
    """Port of internal/lane/posterior.go. X is the mean of the log of the
    quantity, P the variance of that mean; exp(X) reads as the MEDIAN, which is
    what a person waits, not the log-normal's tail-dragged mean."""

    __slots__ = ("X", "P")

    def __init__(self):
        self.X = 0.0
        self.P = 0.0

    def known(self):
        return self.P > 0

    def predict(self, dt):
        # P *= 2 ** (dt / halflife): after one half-life with no evidence the
        # variance has doubled, so the INFORMATION has halved. The estimate is
        # not moved -- we have no reason to think the lane changed, only a
        # reason to be less sure. That is why there is no penalty box anywhere.
        if self.P > 0 and dt > 0:
            self.P *= 2.0 ** (dt / HALFLIFE_S)

    def update(self, z, R):
        if R <= 0:
            return
        if not self.known():
            # An unknown belief is an infinitely wide prior and the limit of the
            # update there is to adopt the observation outright. Averaging it
            # with a certainty we do not have would discard the first sighting.
            self.X, self.P = z, R
            return
        K = self.P / (self.P + R)
        self.X = self.X + K * (z - self.X)
        self.P = (1.0 - K) * self.P

    def quantile(self, z):
        return math.exp(self.X + z * math.sqrt(self.P)) if self.known() else 0.0


class Beta:
    """A successes, B failures, on 'the answer was usable'. A count pair rather
    than a rate, so that nine-out-of-ten and nine-hundred-out-of-a-thousand are
    not the same belief."""

    __slots__ = ("A", "B", "prior")

    def __init__(self, a, b):
        self.A, self.B = a, b
        # The prior's own mass, kept so that "how much has this lane shown us"
        # can be asked without the prior answering for it.
        self.prior = a + b

    def mean(self):
        return self.A / (self.A + self.B)

    def obs(self):
        """The lane's OWN outcomes, prior mass excluded."""
        return self.A + self.B - self.prior

    def observe(self, ok):
        if ok:
            self.A += 1.0
        else:
            self.B += 1.0


# THE QUALITY GATE AS THE DESIGN SPELLS IT REFUSES EVERY LANE ON REQUEST ONE.
#
# The prior is Beta(8, 1), whose mean is 0.889. q_need is 0.90 for talk and
# 0.97 for work. So on a fresh process every lane's quality posterior mean is
# below every scenario's requirement and the candidate set is empty before a
# single token has been drawn. That is arithmetic, not a property of any lane,
# and it is the first finding in REPORT.md.
#
# The resolution here is the one the package's own emptiness law already
# implies -- a gate may not refuse a lane on a number nobody measured. A lane
# becomes subject to the quality gate only once it has QUAL_MIN_OBS outcomes of
# its own. Fifty is not a taste: with a Beta(8,1) prior and a true accept rate
# of 0.985, (8+n)/(9+n) first clears 0.97 at n = 47, so fewer than fifty
# observations cannot distinguish a good lane from a bad one under this prior
# no matter what the lane did. Below that the lane passes on the prior and the
# belief, the prune and the scalar decide; above it the gate does real work and
# separates fp8 (0.985) from unknown (0.95) from fp4 (0.85).
QUAL_MIN_OBS = 50.0
PRIOR_BETA = (8.0, 1.0)
PRIOR_BETA_FP4 = (2.0, 2.0)     # four-bit weights start suspected, not accused


class Belief:
    """Everything one policy thinks about one lane."""

    def __init__(self, lane):
        self.lane = lane
        self.ttft = Posterior()
        self.rate = Posterior()
        a, b = PRIOR_BETA_FP4 if lane.quant == "fp4" else PRIOR_BETA
        self.qual = Beta(a, b)
        # sigma0**2 is the sheet's own prior variance and it is the observation
        # noise R for a real sighting. The sheet pseudo-observation arrives with
        # R inflated by k so a public aggregate keeps pulling the belief toward
        # reality without drowning our own answers.
        self.R_ttft = lane.ttft_sigma ** 2
        self.R_rate = lane.rate_sigma ** 2
        self.at = 0.0
        self.ttft.update(math.log(max(lane.ttft_p50, 1.0)), self.R_ttft * SHEET_R_MULT)
        self.rate.update(math.log(max(lane.rate_p50, RATE_FLOOR)), self.R_rate * SHEET_R_MULT)
        # Ageing may make a belief worthless but never worse than the public
        # sheet, so P is clamped at the sheet pseudo-observation's variance.
        self.P_cap_ttft = self.R_ttft * SHEET_R_MULT
        self.P_cap_rate = self.R_rate * SHEET_R_MULT

    def age(self, now):
        dt = now - self.at
        if dt > 0:
            self.ttft.predict(dt)
            self.rate.predict(dt)
            self.ttft.P = min(self.ttft.P, self.P_cap_ttft)
            self.rate.P = min(self.rate.P, self.P_cap_rate)
            self.at = now

    def note(self, now, ttft_ms, rate):
        self.age(now)
        self.ttft.update(math.log(max(ttft_ms, 1.0)), self.R_ttft)
        self.rate.update(math.log(max(rate, RATE_FLOOR)), self.R_rate)
        self.at = now


# ── THE POLICIES ────────────────────────────────────────────────────────────

class Policy:
    """A policy owns its own simulated clock and its own persistent state.

    The base class owns the request loop that every arm shares: walk the order,
    draw, pay for what was drawn, and on a refused answer pay again on the next
    lane. Only belief+hedge overrides it, and only to add the hedge.
    """

    name = "?"
    may_hedge = False

    def __init__(self, lanes, scen, rng):
        self.scen, self.rng = scen, rng
        self.all = lanes
        self.cands = capability_gate(lanes, scen)
        self.now = 0.0

    def order(self, i, n):
        raise NotImplementedError

    def note(self, lane, ttft_ms, rate, accepted):
        pass

    def run(self, i, n):
        _, _, visible, hidden, _, _ = self.scen
        order = self.order(i, n)
        if not order:
            order = self.cands or self.all
        elapsed = 0.0
        cost = 0.0
        retries = 0
        served = None
        eff_ttft = 0.0
        eff_rate = 0.0
        for lane in order[:MAX_ATTEMPTS]:
            ttft_s = lane.draw_ttft_ms(self.rng) / 1000.0
            rate = lane.draw_rate(self.rng)
            cost += request_price(lane, visible, hidden)
            accept_p = lane.true_accept if (lane.tools or not self.scen[5]) else 0.0
            ok = self.rng.random() < accept_p
            self.note(lane, ttft_s * 1000.0, rate, ok)
            if ok:
                served, eff_ttft, eff_rate = lane, elapsed + ttft_s, rate
                elapsed += wall_seconds(ttft_s, rate, visible, hidden)
                break
            # A refused answer costs the whole call: the tokens were written and
            # the wait was waited. Charging only the retry would make a lane that
            # returns unusable JSON look fast.
            elapsed += wall_seconds(ttft_s, rate, visible, hidden)
            retries += 1
        else:
            # Every attempt refused. The last one is what the caller is stuck
            # with; it is counted, not silently dropped.
            served, eff_ttft, eff_rate = order[min(len(order), MAX_ATTEMPTS) - 1], elapsed, rate
        self.now += elapsed
        return {
            "lane": served.name,
            "ttft_ms": eff_ttft * 1000.0,
            "wall_s": elapsed,
            "wait_s": wait_seconds(eff_ttft, eff_rate, visible, hidden),
            "usd": cost,
            "hedged": False,
            "hedge_waste_usd": 0.0,
            "retries": retries,
        }


class OpenRouterDefault(Policy):
    """The router's own documented default: price-weighted with inverse-square
    weights over stable lanes.

    No memory and no hedge, which is the honest description of what a request
    gets today if nothing in the harness has an opinion. Its weakness on this
    sheet is not subtle and is worth stating rather than discovering: the
    cheapest tool-capable lane, DigitalOcean, writes at 6 tok/s, and 1/price**2
    loves it.
    """

    name = "openrouter-default"

    def __init__(self, lanes, scen, rng):
        super().__init__(lanes, scen, rng)
        _, _, visible, hidden, _, _ = scen
        # "stable" is the router's own filter, applied on top of the shared
        # capability gate: status 0 and five-minute uptime at or above 95.
        self.pool = [L for L in self.cands if L.status == 0 and L.up5 >= 95.0]
        self.weights = [1.0 / (request_price(L, visible, hidden) ** 2) for L in self.pool]

    def order(self, i, n):
        # Sampled without replacement so the retry order is a second draw from
        # the same weights rather than the same lane three times.
        pool = list(self.pool)
        w = list(self.weights)
        picked = []
        for _ in range(min(MAX_ATTEMPTS, len(pool))):
            k = _weighted_index(self.rng, w)
            picked.append(pool.pop(k))
            w.pop(k)
        return picked


def _weighted_index(rng, w):
    total = sum(w)
    x = rng.random() * total
    acc = 0.0
    for i, wi in enumerate(w):
        acc += wi
        if x <= acc:
            return i
    return len(w) - 1


# The four constants this design exists to retire, copied from
# internal/provider/velocity.go so the baseline is the shipped law and not a
# reconstruction of it.
LAG_TTFT_S = 2.0
LAG_RATE = 30.0
DEMOTE_AFTER = 2
IGNORE_AFTER = 3
IGNORE_COOLDOWN_S = 300.0


class StrikeLedger(Policy):
    """Today's shipped law: fixed thresholds, strikes, a five-minute penalty box.

    Its notion of slow is a constant, which is wrong for a lane whose normal is
    587 ms and wrong for a lane whose normal is 2.9 s, and it starts every
    process empty. Both of those are on display here.
    """

    name = "strike-ledger"

    def __init__(self, lanes, scen, rng):
        super().__init__(lanes, scen, rng)
        self.base = sorted(self.cands, key=lambda L: L.ttft_p50)
        self.strikes = {L.name: 0 for L in self.base}
        self.ignored_until = {L.name: 0.0 for L in self.base}

    def order(self, i, n):
        live, demoted = [], []
        for L in self.base:
            if self.ignored_until[L.name] > self.now:
                continue
            (demoted if self.strikes[L.name] >= DEMOTE_AFTER else live).append(L)
        out = live + demoted
        # Never empty: a ledger that has refused everything has refused nothing,
        # because the request still has to go somewhere.
        return out or self.base

    def note(self, lane, ttft_ms, rate, accepted):
        laggy = (ttft_ms / 1000.0 > LAG_TTFT_S) or (rate < LAG_RATE)
        if not laggy:
            return
        self.strikes[lane.name] = self.strikes.get(lane.name, 0) + 1
        if self.strikes[lane.name] >= IGNORE_AFTER:
            self.ignored_until[lane.name] = self.now + IGNORE_COOLDOWN_S
            # velocity.go leaves the entry one strike short of a refusal when
            # the cooldown expires, so a returning lane is on probation rather
            # than forgiven.
            self.strikes[lane.name] = IGNORE_AFTER - 1


class SheetOnly(Policy):
    """The sheet, believed. Pick the shortest wait from p50 TTFT and p50 rate
    and never learn anything.

    It is in the panel to separate two claims the design makes at once. If
    sheet-only already captures most of the win, the belief and the hedge are
    paying for something the prior gave away for free.
    """

    name = "sheet-only"

    def __init__(self, lanes, scen, rng):
        super().__init__(lanes, scen, rng)
        _, lam, visible, hidden, _, _ = scen
        self.fixed = sorted(
            self.cands,
            key=lambda L: wait_seconds(L.ttft_p50 / 1000.0, L.rate_p50, visible, hidden),
        )

    def order(self, i, n):
        return self.fixed


# ── belief+hedge: the design ────────────────────────────────────────────────

H0 = 50.0               # horizon at which exploration is worth full width
HORIZON_CAP = 500.0
HEDGE_OVERHEAD_S = 0.05
HEDGE_MIN_S = 0.7
HEDGE_MAX_S = 8.0
HEDGE_GRID_S = 0.05
HEDGE_PER_MIN = 6.0     # token bucket, per minute of simulated time


def _phi(x):
    return 0.5 * (1.0 + math.erf(x / math.sqrt(2.0)))


def hedge_deadline(mu, s, e_alt_s, hedge_cost_usd, lam):
    """The smallest wait at which sending a second request is worth it.

    For a log-normal the expected REMAINING wait grows with how long you have
    already waited -- that is the whole reason a hedge can ever pay -- so there
    is a crossing point and it is per lane and per request rather than a
    constant. Part I section 4:

        P(T > t)         = 1 - Phi((ln t - mu) / s)
        E[T*1{T>t}]      = exp(mu + s^2/2) * Phi((mu + s^2 - ln t) / s)
        E[T - t | T > t] = E[T*1{T>t}] / P(T > t) - t

    t* is the smallest t where that exceeds what the alternative would cost in
    time and money. The search is a 50 ms grid rather than a closed form: the
    grid is honest about what the computation costs on the send path, and a
    closed form for this crossing does not exist anyway.
    """
    thresh = e_alt_s + HEDGE_OVERHEAD_S + (hedge_cost_usd / lam if lam > 0 else 0.0)
    t = HEDGE_MIN_S
    while t <= HEDGE_MAX_S + 1e-9:
        lt = math.log(t)
        p_gt = 1.0 - _phi((lt - mu) / s)
        if p_gt > 1e-9:
            e_tail = math.exp(mu + 0.5 * s * s) * _phi((mu + s * s - lt) / s)
            if e_tail / p_gt - t > thresh:
                return t
        t += HEDGE_GRID_S
    return HEDGE_MAX_S


class BeliefHedge(Policy):
    """The design: gate, Pareto prune at the p75, one scalar, and a hedge whose
    deadline comes from the belief rather than from a constant."""

    name = "belief+hedge"
    may_hedge = True

    def __init__(self, lanes, scen, rng):
        super().__init__(lanes, scen, rng)
        self.bel = {L.name: Belief(L) for L in self.cands}
        self.bucket = HEDGE_PER_MIN     # start full; a fresh session may hedge
        self.last_lane = None
        self.hedges = 0

    # ---- gate ------------------------------------------------------------
    def _gated(self):
        q_need = self.scen[4]
        out = []
        for L in self.cands:
            b = self.bel[L.name]
            if b.qual.obs() >= QUAL_MIN_OBS and b.qual.mean() < q_need:
                continue
            out.append(L)
        # A gate that has refused everything has told us nothing usable; fall
        # back to the capability-gated set rather than fail the request. On this
        # sheet it DOES fire, for the first couple of thousand requests of
        # `work` and `offpath`, because no lane's posterior can clear 0.97 until
        # it has about fifty clean sightings -- see REPORT.md, finding 1.
        #
        # AND THE GATE IS ABSORBING. Section 6 of Part II says a refused lane
        # comes back because its Beta "decays toward the prior like the Kalman
        # states". No decay is specified for the Beta anywhere the sim was asked
        # to be faithful to, so none is implemented, and the consequence is
        # visible in the output: a lane condemned at the evidence threshold gets
        # no further outcomes and can never be re-measured. That is finding 2,
        # and it is a property of the design's constants rather than of this
        # file -- with the decay section 6 asks for, at a ten-minute half-life
        # and forty-second requests, the effective sample size saturates near 22
        # and (8 + 0.985*22) / 31 = 0.958 never clears 0.97 either. Neither
        # setting of that switch admits a working quality gate at q_need = 0.97.
        return out or self.cands

    # ---- prune -----------------------------------------------------------
    def _prune(self, lanes):
        """Drop a lane that another lane beats on all four axes at the p75.

        p75 and NOT the mean. A lane nobody has measured much has a wide
        posterior, and at the p75 that width keeps it in the set: it might be
        good. Pruning on the mean would quietly make this a router that only
        ever uses what it already knows, which is exactly the exploration the
        design is trying to buy.
        """
        _, _, visible, hidden, _, _ = self.scen
        pts = []
        for L in lanes:
            b = self.bel[L.name]
            ttft75 = b.ttft.quantile(Z75)
            # p75 of 1/rate is the p25 of rate: the same posterior, negated.
            inv75 = math.exp(-b.rate.X + Z75 * math.sqrt(b.rate.P))
            price = request_price(L, visible, hidden, cached=(L is self.last_lane))
            pts.append((L, ttft75, inv75, price, 1.0 - b.qual.mean()))
        keep = []
        for a in pts:
            dominated = False
            for c in pts:
                if c is a:
                    continue
                if (c[1] <= a[1] and c[2] <= a[2] and c[3] <= a[3] and c[4] <= a[4]
                        and (c[1] < a[1] or c[2] < a[2] or c[3] < a[3] or c[4] < a[4])):
                    dominated = True
                    break
            if not dominated:
                keep.append(a[0])
        return keep or lanes

    # ---- score -----------------------------------------------------------
    def _scored(self, lanes, i, n):
        _, lam, visible, hidden, _, _ = self.scen
        # Exploration sized to the horizon: Thompson sampling is blind to how
        # many decisions are left, so the sampling spread is scaled by
        # min(1, H/H0). A three-call session should never explore; a 500-call
        # swarm should explore early. It is one multiply.
        H = min(HORIZON_CAP, float(n - i))
        hs = min(1.0, H / H0)
        rows = []
        for L in lanes:
            b = self.bel[L.name]
            b.age(self.now)
            t_ms = math.exp(b.ttft.X + math.sqrt(b.ttft.P) * self.rng.gauss(0, 1) * hs)
            r = max(RATE_FLOOR,
                    math.exp(b.rate.X + math.sqrt(b.rate.P) * self.rng.gauss(0, 1) * hs))
            price = request_price(L, visible, hidden, cached=(L is self.last_lane))
            per = wait_seconds(t_ms / 1000.0, r, visible, hidden)
            if lam > 0:
                # score in SECONDS. At lambda = 90 and per-request prices around
                # $7e-4 the price term is about 8 microseconds, so this scalar is
                # effectively pure wait at these token counts. That is a
                # property of the design's own numbers, not of the sim, and it
                # is written up in REPORT.md.
                rows.append((per + price / lam, L, per, price))
            else:
                # lambda = 0 is price only: nobody is waiting, so money decides
                # and time is the tie-break.
                rows.append(((price, per), L, per, price))
        rows.sort(key=lambda r: r[0])
        return rows

    def order(self, i, n):
        return [r[1] for r in self._scored(self._prune(self._gated()), i, n)]

    def note(self, lane, ttft_ms, rate, accepted):
        b = self.bel.get(lane.name)
        if b is None:
            return
        b.note(self.now, ttft_ms, rate)
        b.qual.observe(accepted)

    # ---- the request -----------------------------------------------------
    def run(self, i, n):
        _, lam, visible, hidden, _, _ = self.scen
        rows = self._scored(self._prune(self._gated()), i, n)
        order = [r[1] for r in rows]
        if not order:
            return super().run(i, n)

        # Refill the token bucket by simulated time, not by request count: six
        # hedges a minute is a budget on money and connections, and both are
        # spent in wall time.
        dt = self.now - getattr(self, "_bucket_at", 0.0)
        self.bucket = min(HEDGE_PER_MIN, self.bucket + dt * HEDGE_PER_MIN / 60.0)
        self._bucket_at = self.now

        elapsed = 0.0
        cost = 0.0
        waste = 0.0
        retries = 0
        hedged = False
        served = eff_ttft = eff_rate = None

        for attempt, lane in enumerate(order[:MAX_ATTEMPTS]):
            b = self.bel[lane.name]
            b.age(self.now)
            ttft_s = lane.draw_ttft_ms(self.rng) / 1000.0
            rate = lane.draw_rate(self.rng)
            cost += request_price(lane, visible, hidden, cached=(lane is self.last_lane))
            w = wall_seconds(ttft_s, rate, visible, hidden)
            this_ttft, this_rate, this_wall = ttft_s, rate, w
            winner = lane

            alt = order[1] if len(order) > 1 else None
            if attempt == 0 and lam > 0 and alt is not None and self.bucket >= 1.0:
                ab = self.bel[alt.name]
                ab.age(self.now)
                mu = b.ttft.X - math.log(1000.0)           # log-SECONDS
                s = math.sqrt(b.ttft.P + b.R_ttft)
                e_alt = math.exp(ab.ttft.X) / 1000.0        # alternative's median TTFT
                alt_price = request_price(alt, visible, hidden)
                t_star = hedge_deadline(mu, s, e_alt, alt_price, lam)
                if ttft_s > t_star:
                    hedged = True
                    self.hedges += 1
                    self.bucket -= 1.0
                    a_ttft = alt.draw_ttft_ms(self.rng) / 1000.0
                    a_rate = alt.draw_rate(self.rng)
                    a_start = t_star + HEDGE_OVERHEAD_S
                    a_wall = a_start + wall_seconds(a_ttft, a_rate, visible, hidden)
                    cost += alt_price
                    # A HEDGE IS A MEASUREMENT. The second request a slow stream
                    # earns is also the only cheap way to learn what the
                    # alternative would have done, so it goes into the ledger
                    # like any other sighting.
                    ab.note(self.now, a_ttft * 1000.0, a_rate)
                    if a_wall < w:
                        # The hedge won. The primary is cancelled having already
                        # written elapsed_after_ttft * rate tokens; those are
                        # billed and are hedge_waste.
                        lost = max(0.0, a_wall - ttft_s) * rate * lane.p_out
                        waste += lost
                        cost += lost
                        winner, this_ttft, this_rate = alt, a_start + a_ttft, a_rate
                        this_wall = a_wall
                    else:
                        lost = max(0.0, w - (a_start + a_ttft)) * a_rate * alt.p_out
                        waste += lost
                        cost += lost

            accept_p = winner.true_accept if (winner.tools or not self.scen[5]) else 0.0
            ok = self.rng.random() < accept_p
            wb = self.bel[winner.name]
            wb.note(self.now, this_ttft * 1000.0, this_rate)
            wb.qual.observe(ok)
            if ok:
                served, eff_ttft, eff_rate = winner, elapsed + this_ttft, this_rate
                elapsed += this_wall
                break
            elapsed += this_wall
            retries += 1
        else:
            served, eff_ttft, eff_rate = winner, elapsed, this_rate

        self.now += elapsed
        self.last_lane = served
        return {
            "lane": served.name,
            "ttft_ms": eff_ttft * 1000.0,
            "wall_s": elapsed,
            "wait_s": wait_seconds(eff_ttft, eff_rate, visible, hidden),
            "usd": cost,
            "hedged": hedged,
            "hedge_waste_usd": waste,
            "retries": retries,
        }


POLICIES = [OpenRouterDefault, StrikeLedger, SheetOnly, BeliefHedge]


# ── RUNNING AND REPORTING ───────────────────────────────────────────────────

def pct(vals, q):
    """Nearest-rank percentile on the sorted sample. Not interpolated: with
    N = 10000 the difference is invisible, and a rank is a value that actually
    happened."""
    if not vals:
        return float("nan")
    s = sorted(vals)
    k = min(len(s) - 1, max(0, int(math.ceil(q * len(s))) - 1))
    return s[k]


def run_cell(lanes, scen, policy_cls, n, seed):
    name, lam, visible, hidden, q_need, tools = scen
    # One Random per (policy, scenario), derived from the seed by name. A policy
    # that draws more numbers -- belief+hedge draws two per candidate lane --
    # must not shift the stream another policy sees.
    rng = random.Random(f"{seed}|{policy_cls.name}|{name}")
    pol = policy_cls(lanes, scen, rng)
    recs = [pol.run(i, n) for i in range(n)]
    lanes_used = Counter(r["lane"] for r in recs)
    modal, modal_n = lanes_used.most_common(1)[0]
    ttft = [r["ttft_ms"] for r in recs]
    wall = [r["wall_s"] for r in recs]
    per = [r["wait_s"] for r in recs]
    usd = sum(r["usd"] for r in recs)
    return {
        "policy": policy_cls.name,
        "scenario": name,
        "n": n,
        "ttft_p50": pct(ttft, 0.50), "ttft_p90": pct(ttft, 0.90), "ttft_p99": pct(ttft, 0.99),
        "answer_p50": pct(wall, 0.50), "answer_p90": pct(wall, 0.90), "answer_p99": pct(wall, 0.99),
        "wait_p50": pct(per, 0.50), "wait_p90": pct(per, 0.90),
        "wait_p99": pct(per, 0.99),
        # The MEAN wait, which the ship gate's money clause reads. Lambda
        # prices a second saved on the average request; a percentile cannot be
        # divided by a rate and come out as dollars per request.
        "wait_mean": sum(per) / len(per),
        "usd_per_1k": usd / n * 1000.0,
        "hedge_pct": 100.0 * sum(1 for r in recs if r["hedged"]) / n,
        "hedge_waste_usd_per_1k": sum(r["hedge_waste_usd"] for r in recs) / n * 1000.0,
        "modal_lane": modal,
        "modal_share_pct": 100.0 * modal_n / n,
        "lane_mix": dict(lanes_used.most_common()),
        # A refused answer is a retry, and a retry is money and seconds nobody
        # asked for. It is not a table column because the table's columns are
        # the ones the ship gate reads; it is printed as a footnote so that a
        # cost difference between arms can be attributed rather than guessed.
        "retry_pct": 100.0 * sum(1 for r in recs if r["retries"]) / n,
        "retries_per_1k": 1000.0 * sum(r["retries"] for r in recs) / n,
    }


# ── THE SHIP GATE ───────────────────────────────────────────────────────────
#
# The gate has a speed half and a money half, and both are stated in the
# design's own terms rather than as numbers picked to look strict.
#
# THE MONEY HALF IS LAMBDA, NOT A PERCENTAGE. The gate used to demand "no more
# than 3% extra cost", and three percent is a number from nowhere: nobody in
# this design derived it and no scenario means anything by it. The design
# already carries a price for a second -- lambda, in seconds per dollar, chosen
# per scenario in Part II section 1 -- and a router that spends a dollar to buy
# more than lambda seconds has, by the design's own arithmetic, made a good
# trade. So the money clause IS that trade, per request:
#
#     delta_dollars_per_request  <=  delta(mean wait) / lambda
#
# At lambda = 0 -- off the critical path, nobody waiting -- the right-hand side
# is zero and the rule degenerates to "it must not cost more than the
# baseline", which is exactly what background work should demand. The mean and
# not the p90 is on the left of that division because lambda prices the seconds
# actually saved across the run, and a percentile is not a quantity you can
# divide by a rate and get dollars per request out of.
#
# THE SPEED HALF IS THE P90 OF WHAT THE SCENARIO ACTUALLY BUYS. For `work` and
# `offpath` that is the wait. For `talk` it is the FIRST TOKEN alone: above the
# reading rate every lane is the same speed to a person, so a talk turn's whole
# prize is the empty line before the stream starts, and gating it on the wait
# would grade the design partly on a term it has no way to move.
GATE_P90_IMPROVE = 0.30

# THE NAMED BASELINE IS THE MECHANISM THIS DESIGN PROPOSES TO RETIRE. Beating
# the router's own default is necessary and decides nothing, because nobody is
# proposing to keep the default. `openrouter-default` and `sheet-only` are
# graded against the same baseline as ordinary arms, so the table still shows
# what today's default and the prior-alone would cost.
BASELINE = "strike-ledger"

# Which p90 each scenario's speed half reads, and what to call it in the table.
GATE_SPEED = {
    "talk": ("ttft_p90", "p90 first token"),
    "work": ("wait_p90", "p90 wait"),
    "offpath": ("wait_p90", "p90 wait"),
}


def gate_one(scen, base, design):
    """One arm against the baseline in one scenario, with both quantities.

    Returns the verdict AND the two numbers that decided it, because a gate
    that prints only PASS or FAIL is a gate nobody can argue with.
    """
    name, lam = scen[0], scen[1]
    key, label = GATE_SPEED[name]
    improve = (base[key] - design[key]) / base[key]
    # Per REQUEST, because dollars per request is the unit lambda is quoted in.
    d_usd = (design["usd_per_1k"] - base["usd_per_1k"]) / 1000.0
    saved_s = base["wait_mean"] - design["wait_mean"]
    budget = saved_s / lam if lam > 0 else 0.0
    return {
        "scenario": name, "baseline": base["policy"], "policy": design["policy"],
        "speed_metric": label, "speed_improve": improve,
        "d_usd_per_request": d_usd,
        "mean_wait_saved_s": saved_s,
        "usd_budget_per_request": budget,
        "speed_ok": improve >= GATE_P90_IMPROVE,
        "money_ok": d_usd <= budget,
        "pass": improve >= GATE_P90_IMPROVE and d_usd <= budget,
    }


def ship_gate(rows):
    out = []
    by = {(r["scenario"], r["policy"]): r for r in rows}
    for scen in SCENARIOS:
        base = by[(scen[0], BASELINE)]
        for cls in POLICIES:
            # An arm graded against itself decides nothing, so the baseline is
            # left out rather than printed as a guaranteed row of zeroes.
            if cls.name == BASELINE:
                continue
            out.append(gate_one(scen, base, by[(scen[0], cls.name)]))
    return out


def sweep(lanes, n, seeds):
    """The ship gate over several seeds.

    A single seed is one draw from the experiment, and a verdict that flips
    between draws is not a verdict. This is here because the first run of this
    file produced a `work` verdict that reversed between seed 7 and seed 11,
    and reporting seed 7 alone would have been a number chosen after the fact.
    """
    print("── seed sweep ── is the verdict a property of the design "
          "or of the seed? " + "─" * 8)
    print()
    print(f"   {'seed':<6}{'scenario':<10}{'design converges on':<24}"
          f"{'$/1k':>8}{'p90 wait':>11}{'p90 ttft ms':>13}{'gate':>9}")
    print("   " + "-" * 78)
    tally = {sc[0]: Counter() for sc in SCENARIOS}
    passes = Counter()
    for seed in seeds:
        for scen in SCENARIOS:
            rs = {c.name: run_cell(lanes, scen, c, n, seed) for c in POLICIES}
            b = rs["belief+hedge"]
            g = gate_one(scen, rs[BASELINE], b)
            tally[scen[0]][b["modal_lane"]] += 1
            if g["pass"]:
                passes[scen[0]] += 1
            where = "%s (%d%%)" % (b["modal_lane"], round(b["modal_share_pct"]))
            print(f"   {seed:<6}{scen[0]:<10}{where:<24}"
                  f"{b['usd_per_1k']:>8.3f}{b['wait_p90']:>11.2f}"
                  f"{b['ttft_p90']:>13.0f}"
                  f"{('PASS' if g['pass'] else 'FAIL'):>9}")
    print()
    for scen in SCENARIOS:
        mix = tally[scen[0]]
        print(f"   {scen[0]:<10}converged on {len(mix)} different lane(s) across "
              f"{len(seeds)} seeds: {dict(mix)}")
    print()
    for scen in SCENARIOS:
        print(f"   {scen[0]:<10}vs {BASELINE:<16}"
              f"passes on {passes[scen[0]]}/{len(seeds)} seeds")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--sheet", default=DEFAULT_SHEET)
    ap.add_argument("--n", type=int, default=10000, help="requests per cell")
    ap.add_argument("--seed", type=int, default=7)
    ap.add_argument("--sweep", type=int, default=0, metavar="K",
                    help="run the ship gate over K seeds and report whether "
                         "the verdict is stable")
    ap.add_argument("--json", default=None, help="dump the raw table here")
    args = ap.parse_args()

    doc = json.load(open(args.sheet))
    lanes = [Lane(e) for e in doc["data"]["endpoints"]]
    known = [L for L in lanes if L.known()]

    print(f"sheet:   {os.path.relpath(args.sheet, HERE)}")
    print(f"model:   {doc.get('model', doc['data'].get('id'))}")
    print(f"fetched: {doc.get('fetched_at')}")
    print(f"lanes:   {len(lanes)} on the sheet, {len(known)} with p50 timing")
    print(f"seed:    {args.seed}    requests per cell: {args.n}")
    print(f"prompt:  {PROMPT_TOKENS} tokens every request; read rate {READ_RATE} tok/s")
    print()

    if args.sweep:
        seeds = [args.seed + 2 * k for k in range(args.sweep)]
        sweep(known, args.n, seeds)
        return

    rows = []
    for scen in SCENARIOS:
        name, lam, visible, hidden, q_need, tools = scen
        gated = capability_gate(known, scen)
        print(f"── {name}  ({SCENARIO_WHY[name]}) "
              + "─" * max(3, 74 - len(name) - len(SCENARIO_WHY[name])))
        print(f"   lambda={lam:g} s/$   visible={visible}  hidden={hidden}  "
              f"q_need={q_need}  tools={'yes' if tools else 'no'}")
        print(f"   {len(gated)}/{len(known)} lanes past the shared capability gate: "
              + ", ".join(L.name for L in gated))
        print()
        hdr = (f"   {'policy':<19}{'TTFT p50/p90/p99 (ms)':>26}"
               f"{'answer p50/p90/p99 (s)':>26}{'wait p50/p90 (s)':>23}"
               f"{'$/1k':>9}{'hedge%':>8}  modal lane")
        print(hdr)
        print("   " + "-" * (len(hdr) - 3))
        for cls in POLICIES:
            r = run_cell(known, scen, cls, args.n, args.seed)
            rows.append(r)
            print(f"   {r['policy']:<19}"
                  f"{r['ttft_p50']:>8.0f}{r['ttft_p90']:>9.0f}{r['ttft_p99']:>9.0f}"
                  f"{r['answer_p50']:>9.2f}{r['answer_p90']:>8.2f}{r['answer_p99']:>9.2f}"
                  f"{r['wait_p50']:>12.2f}{r['wait_p90']:>11.2f}"
                  f"{r['usd_per_1k']:>9.3f}{r['hedge_pct']:>8.1f}"
                  f"  {r['modal_lane']} ({r['modal_share_pct']:.0f}%)")
        # Footnotes, because a cost difference has to be attributable. The
        # visible term is the CATCH-UP and not the reading, so a lane at or
        # above the reading rate contributes no visible wait whatever; how many
        # of the gated lanes clear that line is what says whether this scenario
        # is decided by the first token alone.
        print()
        if visible > 0:
            fast = sum(1 for L in gated if L.rate_p50 >= READ_RATE)
            print(f"   {fast}/{len(gated)} gated lanes write at or above "
                  f"{READ_RATE:g} tok/s at their median, and on those the "
                  f"{visible} visible tokens add NOTHING to the wait — they are "
                  f"read as they arrive. So this scenario is decided by the "
                  f"first token, which is what its gate reads.")
        else:
            print("   no visible tokens, so nothing is read as it arrives and "
                  "the wait is the whole answer")
        for r in rows[-len(POLICIES):]:
            print(f"     {r['policy']:<19} refused-and-retried {r['retry_pct']:.1f}% of requests"
                  f"   hedge waste ${r['hedge_waste_usd_per_1k']:.4f}/1k"
                  f"   lanes used: {len(r['lane_mix'])}")
        print()

    print(f"── ship gate ── against {BASELINE}: the scenario's p90 improves by "
          f">= {GATE_P90_IMPROVE * 100:.0f}%,")
    print("   AND the extra dollars per request are no more than the mean "
          "seconds saved, priced at lambda")
    print()
    gates = ship_gate(rows)
    hdr = (f"   {'scenario':<10}{'policy':<20}{'speed metric':<17}{'improve':>9}"
           f"{'extra $/req':>14}{'budget $/req':>14}   verdict")
    print(hdr)
    print("   " + "-" * (len(hdr) - 3))
    for g in gates:
        why = []
        if not g["speed_ok"]:
            why.append("p90")
        if not g["money_ok"]:
            why.append("cost")
        tag = ("PASS" if g["pass"] else "FAIL") + (" (" + "+".join(why) + ")" if why else "")
        print(f"   {g['scenario']:<10}{g['policy']:<20}{g['speed_metric']:<17}"
              f"{g['speed_improve'] * 100.0:>+8.1f}%"
              f"{g['d_usd_per_request']:>+14.6f}{g['usd_budget_per_request']:>14.6f}"
              f"   {tag}")
    print()
    design = [g for g in gates if g["policy"] == "belief+hedge"]
    n_pass = sum(1 for g in design if g["pass"])
    print(f"   the design passes in {n_pass}/{len(design)} scenarios; "
          f"{sum(1 for g in gates if g['pass'])}/{len(gates)} rows pass overall.")

    if args.json:
        with open(args.json, "w") as f:
            json.dump({"seed": args.seed, "n": args.n,
                       "sheet": os.path.basename(args.sheet),
                       "fetched_at": doc.get("fetched_at"),
                       "rows": rows, "ship_gate": gates}, f, indent=2)
        print(f"\n   wrote {args.json}")


if __name__ == "__main__":
    main()
