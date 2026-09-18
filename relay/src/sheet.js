// sheet.js folds a install's rows into running totals and aggregates all
// installs' totals into the cells of an index document.
//
// Each stored triple is {n, s, s2}: how many scores, their sum and their sum
// of squares. A triple is a running total, so two copies of the same triple
// carry the same or more observations and the larger one is the more complete
// — that is what makes `join` a least upper bound and the fold a CRDT.
//
// Everything here is pure: plain values in, plain values out. No KV, no fetch.

// METRIC_ROLE_QUALITY is the metric the sheets carried first, and the one a
// five-segment key (written before the metric was part of a key) belongs to.
const METRIC_ROLE_QUALITY = 'role_quality';

// METRIC_ACCEPTABLE is the harness's own grade of a task, 100 or 0 per seat.
// Its cells are published per source — the judge column, which for this
// metric names the grader — so a reader can fit a reliability per source and
// never blends two graders in one cell.
const METRIC_ACCEPTABLE = 'acceptable';

// ZERO is an empty triple.
const ZERO = Object.freeze({ n: 0, s: 0, s2: 0 });

// cellKey renders the place one triple lives: the dimensions that are kept
// apart, in a fixed order. The day is the KV key's own segment and is not
// part of this key. The dimensions hold no "|" (the model pattern forbids it),
// so the key splits back cleanly. A role_quality key keeps the five-segment
// form every stored triple was written under; any other metric leads with
// its own name, so the two never collide and nothing already stored moves.
export function cellKey(payload) {
  const tail = `${payload.role}|${payload.model}|${payload.judge}|${payload.door}|${payload.size}`;
  return payload.metric === METRIC_ROLE_QUALITY || !payload.metric ? tail : `${payload.metric}|${tail}`;
}

// fold adds one score to a triple and answers the new triple. A missing
// triple folds as if it were empty.
export function fold(triple, score) {
  const t = triple || ZERO;
  return { n: t.n + 1, s: t.s + score, s2: t.s2 + score * score };
}

// join is the componentwise maximum of two triples — the least upper bound of
// two running totals of the same measurement. It is commutative, associative
// and idempotent.
export function join(a, b) {
  const x = a || ZERO;
  const y = b || ZERO;
  return {
    n: Math.max(x.n, y.n),
    s: Math.max(x.s, y.s),
    s2: Math.max(x.s2, y.s2),
  };
}

// splitKey reads a cellKey back into its dimensions: five segments are a
// role_quality key, six lead with the metric.
function splitKey(key) {
  const parts = key.split('|');
  if (parts.length === 6) {
    const [metric, role, model, judge, door, size] = parts;
    return { metric, role, model, judge, door, size };
  }
  const [role, model, judge, door, size] = parts;
  return { metric: METRIC_ROLE_QUALITY, role, model, judge, door, size };
}

// judgeSeverity fits one additive judge severity per judge over the finest
// cells (one per cellKey), so a judge who scores high or low on everything is
// taken out of every score. It alternates beta_j = weighted mean over cells
// scored by j of (cellMean - mu_rm) with mu_rm = weighted mean over judges of
// (cellMean - beta_j), weights n, five sweeps, re-centring beta to weighted
// mean zero each sweep, and holds each beta inside ±10: the rubric's own
// width is 100, and a judge further off than a tenth of it is not a severity,
// it is a different rubric. It answers {beta, judges}.
function judgeSeverity(cells) {
  const judges = [...new Set(cells.map((c) => c.judge))].sort();
  const beta = new Map(judges.map((j) => [j, 0]));
  const mu = new Map();
  for (const c of cells) {
    mu.set(rmOf(c), 0);
  }
  for (let sweep = 0; sweep < 5; sweep++) {
    // beta_j from the current mu_rm.
    const num = new Map();
    const den = new Map();
    for (const c of cells) {
      const w = c.n;
      num.set(c.judge, (num.get(c.judge) || 0) + w * (c.mean - (mu.get(rmOf(c)) || 0)));
      den.set(c.judge, (den.get(c.judge) || 0) + w);
    }
    for (const j of judges) {
      beta.set(j, den.get(j) ? num.get(j) / den.get(j) : 0);
    }
    // Re-centre beta so its weighted mean is zero; the split between beta and
    // mu is then pinned.
    let bnum = 0;
    let bden = 0;
    for (const c of cells) {
      bnum += c.n * beta.get(c.judge);
      bden += c.n;
    }
    const centre = bden ? bnum / bden : 0;
    for (const j of judges) {
      beta.set(j, beta.get(j) - centre);
    }
    // Hold each severity inside ±10 after the re-centring, before the next
    // sweep reads it: the rubric is 100 points wide, and a tenth of it is
    // the most a judge's taste can be worth before it is a different
    // rubric rather than a severity.
    for (const j of judges) {
      beta.set(j, Math.max(-10, Math.min(10, beta.get(j))));
    }
    // mu_rm from the fresh beta_j.
    const mnum = new Map();
    const mden = new Map();
    for (const c of cells) {
      const w = c.n;
      const rm = rmOf(c);
      mnum.set(rm, (mnum.get(rm) || 0) + w * (c.mean - beta.get(c.judge)));
      mden.set(rm, (mden.get(rm) || 0) + w);
    }
    for (const rm of mu.keys()) {
      mu.set(rm, mden.get(rm) ? mnum.get(rm) / mden.get(rm) : 0);
    }
  }
  return { beta, judges };
}

function rmOf(cell) {
  return `${cell.role}|${cell.model}`;
}

// median of a numeric list (the list is copied, not sorted in place).
function median(values) {
  const xs = [...values].sort((a, b) => a - b);
  const mid = Math.floor(xs.length / 2);
  return xs.length % 2 ? xs[mid] : (xs[mid - 1] + xs[mid]) / 2;
}

// huberMean is the Huber M-estimate of a location from values: scale from the
// median absolute deviation (1.4826 · MAD), tuning 1.345, twenty iterations.
// Each value's weight is 1 inside c·scale and c·scale/|x-mu| beyond it, so a
// single far-off value cannot drag the estimate.
function huberMean(values) {
  const centre = median(values);
  const mad = median(values.map((v) => Math.abs(v - centre)));
  const scale = 1.4826 * mad;
  if (!(scale > 0)) {
    // A zero MAD says half the values sit at the median, so the data's own
    // centre is the median; the plain mean would let the far half drag the
    // estimate below (or above) every repeated value.
    return centre;
  }
  const c = 1.345 * scale;
  let mu = centre;
  for (let i = 0; i < 20; i++) {
    let num = 0;
    let den = 0;
    for (const v of values) {
      const dev = Math.abs(v - mu);
      const w = dev <= c ? 1 : c / dev;
      num += w * v;
      den += w;
    }
    if (den === 0) {
      break;
    }
    mu = num / den;
  }
  return mu;
}

// aggregate turns sheet entries into the cells of an index document.
//
// entries are {install, day, key, triple}: one install's running total for one
// cellKey on one day. Cells group by (role, model); each install's mean over
// its triples is taken after judge severity is removed — bounded, and with
// every adjusted score clamped back into the rubric before it is folded —
// and the cell mean is the Huber M-estimate over those install means once
// there are at least five installs, the n-weighted mean of them otherwise.
// The sd is pooled from the same adjusted triples, so the published mean and
// sd describe one scale. A cell whose installs are below minInstalls is
// dropped: the floor keeps any single install's numbers from being
// published, and the cells have no reader but the publish path that renders
// them into the document.
export function aggregate(entries, { minInstalls = 0 } = {}) {
  const cells = [];
  for (const entry of entries) {
    const dims = splitKey(entry.key);
    const n = entry.triple.n;
    if (!(n > 0)) {
      continue;
    }
    cells.push({ ...dims, n, mean: entry.triple.s / n });
  }
  const { beta } = judgeSeverity(cells.filter((c) => c.metric !== METRIC_ACCEPTABLE));

  // Group the adjusted entries by role and model, and keep each install's own
  // observations so its mean over them can be taken.
  const groups = new Map();
  for (const entry of entries) {
    const n = entry.triple.n;
    if (!(n > 0)) {
      continue;
    }
    const dims = splitKey(entry.key);
    // A judged opinion groups by role and model with the judge's severity
    // taken out; a grade groups by role, model and the grader that gave it,
    // with nothing taken out — a grade is a fact about the landing, and which
    // grader saw it is a dimension the document keeps, not a bias to remove.
    const graded = dims.metric === METRIC_ACCEPTABLE;
    const rm = graded
      ? `${dims.metric}|${dims.role}|${dims.model}|${dims.judge}`
      : `${dims.metric}|${dims.role}|${dims.model}`;
    let group = groups.get(rm);
    if (!group) {
      group = { metric: dims.metric, role: dims.role, model: dims.model, source: graded ? dims.judge : undefined, installs: new Map(), n: 0, s: 0, s2: 0 };
      groups.set(rm, group);
    }
    // Judge severity removal shifts each score by -beta[judge], and the
    // rubric holds every score in [0, 100]: a shift can carry a triple past
    // either end, so the adjusted scores are clamped back into the rubric
    // before they are folded. Over a triple the shift itself is exact —
    // Sum(x - beta) = s - n*beta and Sum((x - beta)^2) = s2 - 2*beta*s +
    // n*beta^2, from expanding (x - beta)^2 — but the store holds {n, s, s2}
    // and not the scores, so the clamp acts on what the triple still knows:
    // its mean. An adjusted mean outside [0, 100] says the scores as adjusted
    // lie past the rubric, and the nearest triple inside it is every score at
    // the bound it crossed — sums n*b and n*b^2, which is also the case the
    // exact shift cannot express.
    const shift = graded ? 0 : (beta.get(dims.judge) || 0);
    const adjustedMean = entry.triple.s / n - shift;
    let adjustedSum;
    let adjustedSum2;
    if (adjustedMean > 100) {
      adjustedSum = 100 * n;
      adjustedSum2 = 100 * 100 * n;
    } else if (adjustedMean < 0) {
      adjustedSum = 0;
      adjustedSum2 = 0;
    } else {
      adjustedSum = entry.triple.s - n * shift;
      adjustedSum2 = entry.triple.s2 - 2 * shift * entry.triple.s + n * shift * shift;
    }
    group.n += n;
    group.s += adjustedSum;
    group.s2 += adjustedSum2;
    const install = group.installs.get(entry.install);
    if (install) {
      install.n += n;
      install.s += adjustedSum;
    } else {
      group.installs.set(entry.install, { n, s: adjustedSum });
    }
  }

  const out = [];
  for (const group of groups.values()) {
    const installMeans = [];
    let weightSum = 0;
    let weightedSum = 0;
    for (const install of group.installs.values()) {
      const mean = install.s / install.n;
      installMeans.push(mean);
      weightSum += install.n;
      weightedSum += install.n * mean;
    }
    const installs = installMeans.length;
    const mean = installs >= 5
      ? huberMean(installMeans)
      : (weightSum ? weightedSum / weightSum : 0);
    // Pooled within-cell standard deviation from the adjusted triples, the
    // same ones the mean is taken over.
    const sd = group.n >= 2
      ? Math.sqrt(Math.max(0, (group.s2 - (group.s * group.s) / group.n) / (group.n - 1)))
      : 0;
    const cell = {
      metric: group.metric,
      role: group.role,
      model: group.model,
      mean,
      sd,
      n: group.n,
      installs,
    };
    if (group.source !== undefined) {
      cell.source = group.source;
    }
    out.push(cell);
  }

  // Cells below minInstalls never leave the sheet, so every cell here meets
  // it. Ties break on metric, then mean, role and model, so the document is
  // a property of its entries.
  const kept = out.filter((c) => c.installs >= minInstalls);
  kept.sort((a, b) => {
    if (a.metric !== b.metric) return a.metric < b.metric ? -1 : 1;
    if (a.mean !== b.mean) return b.mean - a.mean;
    if (a.role !== b.role) return a.role < b.role ? -1 : 1;
    if (a.model !== b.model) return a.model < b.model ? -1 : 1;
    return 0;
  });
  return kept;
}

// judgesOf answers the sorted judge ids the entries name.
export function judgesOf(entries) {
  const judges = new Set();
  for (const entry of entries) {
    judges.add(splitKey(entry.key).judge);
  }
  return [...judges].sort();
}
