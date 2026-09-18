// sheet.test.js checks the running totals, the CRDT join and the aggregation.

import test from 'node:test';
import assert from 'node:assert/strict';

import { aggregate, cellKey, fold, join, judgesOf } from '../src/sheet.js';

const KEY = cellKey({
  role: 'worker',
  model: 'z-ai/glm-5.3-flash',
  judge: 'anthropic/claude-opus-5',
  door: 'task',
  size: 'M',
});

function entry(install, day, key, mean, n = 4) {
  return { install, day, key, triple: { n, s: mean * n, s2: mean * mean * n } };
}

test('cellKey keeps the five dimensions in order', () => {
  assert.equal(KEY, 'worker|z-ai/glm-5.3-flash|anthropic/claude-opus-5|task|M');
});

test('fold adds one score', () => {
  assert.deepEqual(fold(null, 50), { n: 1, s: 50, s2: 2500 });
  assert.deepEqual(fold({ n: 1, s: 50, s2: 2500 }, 60), { n: 2, s: 110, s2: 6100 });
});

test('join is commutative, associative, idempotent and takes the larger total', () => {
  const a = { n: 2, s: 100, s2: 5200 };
  const b = { n: 3, s: 160, s2: 8600 };
  const c = { n: 1, s: 10, s2: 100 };
  assert.deepEqual(join(a, b), join(b, a));
  assert.deepEqual(join(join(a, b), c), join(a, join(b, c)));
  assert.deepEqual(join(a, a), a);
  assert.deepEqual(join(a, b), b);
  assert.deepEqual(join(a, null), a);
});

test('aggregate takes n, installs and the n-weighted mean below five installs', () => {
  const entries = [
    entry('i1', '2026-09-17', KEY, 50),
    entry('i2', '2026-09-17', KEY, 60),
    entry('i3', '2026-09-17', KEY, 70),
  ];
  const cells = aggregate(entries, { minInstalls: 3 });
  assert.equal(cells.length, 1);
  assert.equal(cells[0].role, 'worker');
  assert.equal(cells[0].model, 'z-ai/glm-5.3-flash');
  assert.equal(cells[0].installs, 3);
  assert.equal(cells[0].n, 12);
  assert.equal(cells[0].mean, 60);
});

test('aggregate pools the within-cell standard deviation from the triples', () => {
  // Two installs, each four scores: the pooled sd is over all eight scores.
  const entries = [
    { install: 'i1', day: '2026-09-17', key: KEY, triple: { n: 4, s: 200, s2: 10000 } }, // 50 ×4
    { install: 'i2', day: '2026-09-17', key: KEY, triple: { n: 4, s: 240, s2: 14400 } }, // 60 ×4
  ];
  const cells = aggregate(entries, { minInstalls: 2 });
  // sqrt((24400 - 440²/8) / 7) = sqrt((24400 - 24200)/7) = sqrt(200/7).
  assert.ok(Math.abs(cells[0].sd - Math.sqrt(200 / 7)) < 1e-9);
  assert.equal(cells[0].sd, Math.sqrt(200 / 7));
});

test('aggregate keeps the Huber mean near six installs when one is absurd', () => {
  const entries = [48, 50, 52, 49, 51, 50, 500].map((mean, i) =>
    entry(`i${i}`, '2026-09-17', KEY, mean));
  const cells = aggregate(entries, { minInstalls: 3 });
  assert.equal(cells[0].installs, 7);
  assert.ok(Math.abs(cells[0].mean - 50) < 1, `mean ${cells[0].mean} should sit near 50`);
});

test('judge severity is removed: two judges offset by +10 agree on each model', () => {
  const key = (model, judge) => cellKey({
    role: 'worker', model, judge, door: 'task', size: 'M',
  });
  const ja = 'anthropic/claude-opus-5';
  const jb = 'deepseek/deepseek-v4.1-flash';
  const entries = [
    entry('a', '2026-09-17', key('z-ai/glm-5.3', ja), 50),
    entry('b', '2026-09-17', key('z-ai/glm-5.3', jb), 60), // +10 severity
    entry('a', '2026-09-17', key('z-ai/glm-5.3-flash', ja), 70),
    entry('b', '2026-09-17', key('z-ai/glm-5.3-flash', jb), 80), // +10 severity
  ];
  const cells = aggregate(entries, { minInstalls: 2 });
  const byModel = new Map(cells.map((c) => [c.model, c]));
  // Both judges land on one mean per model: the +10 offset is gone.
  const base = byModel.get('z-ai/glm-5.3').mean;
  const flash = byModel.get('z-ai/glm-5.3-flash').mean;
  assert.ok(Math.abs(base - 55) < 1e-9, `base ${base}`);
  assert.ok(Math.abs(flash - 75) < 1e-9, `flash ${flash}`);
  assert.ok(Math.abs((flash - base) - 20) < 1e-9);
});

test('aggregate publishes only the cells that meet the minInstalls floor', () => {
  const thin = cellKey({ role: 'worker', model: 'a/one', judge: 'j/x', door: 'task', size: 'S' });
  const fat = cellKey({ role: 'worker', model: 'b/two', judge: 'j/y', door: 'do', size: 'L' });
  const entries = [
    entry('i1', '2026-09-17', thin, 90),
    entry('i1', '2026-09-17', fat, 10),
    entry('i2', '2026-09-17', fat, 10),
    entry('i3', '2026-09-17', fat, 10),
  ];
  const cells = aggregate(entries, { minInstalls: 3 });
  // One install's mean for a/one is below the floor: the cell never leaves
  // the relay, so the document it feeds holds exactly b/two's cell.
  assert.equal(cells.length, 1);
  assert.equal(cells[0].model, 'b/two');
});

test('aggregate returns no cells when every one is below the floor', () => {
  const thin = cellKey({ role: 'worker', model: 'a/one', judge: 'j/x', door: 'task', size: 'S' });
  const entries = [
    entry('i1', '2026-09-17', thin, 90),
    entry('i2', '2026-09-17', thin, 90),
  ];
  const cells = aggregate(entries, { minInstalls: 3 });
  // The document then renders with an empty cells array, still signed and
  // published — a below-floor cell must not leave the relay either way.
  assert.deepEqual(cells, []);
});

test('aggregate publishes judges sorted and orders cells by mean, role then model', () => {
  // Both judges score both models, so severity removal keeps the 80-point
  // gap and the order is the means', not a tie-break's.
  const slow = cellKey({ role: 'worker', model: 'a/one', judge: 'j/x', door: 'task', size: 'S' });
  const fast = cellKey({ role: 'worker', model: 'b/two', judge: 'j/y', door: 'do', size: 'L' });
  const entries = [
    entry('i1', '2026-09-17', slow, 10),
    entry('i1', '2026-09-17', slow.replace('j/x', 'j/y'), 20),
    entry('i2', '2026-09-17', slow, 10),
    entry('i2', '2026-09-17', slow.replace('j/x', 'j/y'), 20),
    entry('i3', '2026-09-17', slow, 10),
    entry('i3', '2026-09-17', slow.replace('j/x', 'j/y'), 20),
    entry('i1', '2026-09-17', fast, 90),
    entry('i1', '2026-09-17', fast.replace('j/y', 'j/x'), 100),
    entry('i2', '2026-09-17', fast, 90),
    entry('i2', '2026-09-17', fast.replace('j/y', 'j/x'), 100),
    entry('i3', '2026-09-17', fast, 90),
    entry('i3', '2026-09-17', fast.replace('j/y', 'j/x'), 100),
  ];
  const cells = aggregate(entries, { minInstalls: 3 });
  const judges = judgesOf(entries);
  assert.deepEqual(judges, ['j/x', 'j/y']);
  assert.equal(cells[0].model, 'b/two'); // higher mean first
  assert.equal(cells[1].model, 'a/one');
});

const GRADED = cellKey({
  metric: 'acceptable',
  role: 'worker',
  model: 'z-ai/glm-5.3-flash',
  judge: 'codeaf/grader',
  door: 'task',
  size: 'M',
});

test('cellKey leads with the metric for anything but role_quality, so stored keys never move', () => {
  assert.equal(GRADED, 'acceptable|worker|z-ai/glm-5.3-flash|codeaf/grader|task|M');
  assert.equal(cellKey({ metric: 'role_quality', role: 'worker', model: 'a/b', judge: 'c/d', door: 'task', size: 'S' }), 'worker|a/b|c/d|task|S');
});

const SEVERITY_KEY = (model, judge) => cellKey({
  role: 'worker', model, judge, door: 'task', size: 'M',
});

// raw builds one entry from the raw scores themselves, not from a mean: the
// severity tests need exact triples.
function raw(install, key, scores) {
  const s = scores.reduce((a, b) => a + b, 0);
  const s2 = scores.reduce((a, b) => a + b * b, 0);
  return { install, day: '2026-09-17', key, triple: { n: scores.length, s, s2 } };
}

test('a severity past the rubric cannot publish a mean above 100', () => {
  // Judge one scores a cell at [90, 90, 90, 90, 98, 98, 100], split 2+2+1+1+1
  // over five installs, and a second cell at 5.85 against judge two at 100 —
  // both cells clear a three-install floor. The fit puts judge one far below
  // centre, and the shift lifts its cell past the rubric's top: today the
  // published mean is 117.52, above every raw score, with the sd 4.6802…
  // pooled from the unadjusted rows. Bounded and clamped, the cell must
  // publish inside the range its raw scores already held.
  const ja = SEVERITY_KEY('m/a', 'j/one');
  const jb = SEVERITY_KEY('m/b', 'j/one');
  const kb = SEVERITY_KEY('m/b', 'j/two');
  const entries = [
    raw('i1', ja, [90, 90]),
    raw('i2', ja, [90, 90]),
    raw('i3', ja, [98]),
    raw('i4', ja, [98]),
    raw('i5', ja, [100]),
    raw('i1', jb, [5.85]),
    raw('i2', jb, [5.85]),
    raw('i3', jb, [5.85]),
    raw('i1', kb, [100]),
    raw('i2', kb, [100]),
    raw('i3', kb, [100]),
  ];
  const cells = aggregate(entries, { minInstalls: 3 });
  const cell = cells.find((c) => c.model === 'm/a');
  assert.equal(cell.n, 7);
  assert.equal(cell.installs, 5);
  assert.ok(cell.mean <= 100, `mean ${cell.mean} must not pass the rubric's top`);
  assert.ok(cell.mean >= 90, `mean ${cell.mean} must not fall under the raw minimum`);
});

test('a severity that would run past ten points is held at ten', () => {
  // Judge one scores one cell alone at 90 and a second cell at 50 against
  // judge two at 100 (twice the rows), so the unbounded fit puts judge one at
  // −25 and judge two at +25 — a severity a quarter of the rubric wide. Held
  // at ∓10, the alone cell publishes at exactly 100 (the shift fills the gap
  // to the top and no further) and the shared cell at the mean of 60 and 90.
  const x = SEVERITY_KEY('m/x', 'j/one');
  const y1 = SEVERITY_KEY('m/y', 'j/one');
  const y2 = SEVERITY_KEY('m/y', 'j/two');
  const entries = [];
  for (const i of ['i1', 'i2', 'i3']) {
    entries.push(raw(i, x, [90]));
    entries.push(raw(i, y1, [50]));
    entries.push(raw(i, y2, [100, 100]));
  }
  const cells = aggregate(entries, { minInstalls: 3 });
  const byModel = new Map(cells.map((c) => [c.model, c]));
  assert.equal(byModel.get('m/x').mean, 100);
  assert.equal(byModel.get('m/x').sd, 0);
  assert.equal(byModel.get('m/y').mean, 80);
});

test('the published sd is pooled from the adjusted scores', () => {
  // A shift that pushes a triple past the rubric clamps every adjusted
  // score to the bound it crossed, so a cell whose adjusted scores all land
  // at 100 publishes sd 0 — today the sd still describes the raw [90, 98].
  // With no shift, the same raw spread keeps today's sd: the shift of a sum
  // of squares is exact, so adjusting moves the mean and not the spread.
  const p = SEVERITY_KEY('m/p', 'j/one');
  const q1 = SEVERITY_KEY('m/q', 'j/one');
  const q2 = SEVERITY_KEY('m/q', 'j/two');
  const shifted = [
    raw('i1', p, [90, 98]),
    raw('i1', q1, [0]),
    raw('i2', q1, [0]),
    raw('i3', q1, [0]),
    raw('i1', q2, [100]),
    raw('i2', q2, [100]),
    raw('i3', q2, [100]),
  ];
  const clamped = aggregate(shifted, { minInstalls: 1 }).find((c) => c.model === 'm/p');
  assert.equal(clamped.mean, 100);
  assert.equal(clamped.sd, 0);

  const flat = SEVERITY_KEY('m/r', 'j/one');
  const unshifted = aggregate([raw('i1', flat, [90, 100])], { minInstalls: 1 })[0];
  assert.equal(unshifted.sd, Math.sqrt(50));
});

test('the Huber mean with a zero MAD answers the median', () => {
  // [97, 97, 97, 97, 20]: four installs at 97 and one at 20. The MAD is
  // zero, so the scale is zero and the estimate falls back — to the median
  // 97, the value four of five installs actually reported, not the plain
  // mean 81.6 that lets the one far-off install drag the cell under every
  // repeated value.
  const key = SEVERITY_KEY('m/s', 'j/one');
  const entries = ['i1', 'i2', 'i3', 'i4', 'i5'].map((install, i) =>
    raw(install, key, [i === 4 ? 20 : 97]));
  const cells = aggregate(entries, { minInstalls: 5 });
  assert.equal(cells[0].mean, 97);
});

test('aggregate keeps a graded cell per source beside the judged cells and removes no severity from it', () => {
  const seeded = cellKey({ metric: 'acceptable', role: 'worker', model: 'z-ai/glm-5.3-flash', judge: 'codeaf/reviewer', door: 'task', size: 'M' });
  const entries = [
    entry('i1', '2026-09-17', KEY, 50),
    entry('i1', '2026-09-17', GRADED, 100, 2),
    entry('i2', '2026-09-17', GRADED, 0, 2),
    entry('i1', '2026-09-17', seeded, 100, 3),
  ];
  const cells = aggregate(entries, { minInstalls: 1 });
  const graded = cells.filter((c) => c.metric === 'acceptable');
  assert.equal(graded.length, 2);
  const byGrader = graded.find((c) => c.source === 'codeaf/grader');
  assert.equal(byGrader.installs, 2);
  assert.equal(byGrader.n, 4);
  assert.equal(byGrader.mean, 50);
  const bySeed = graded.find((c) => c.source === 'codeaf/reviewer');
  assert.equal(bySeed.mean, 100);
  const judged = cells.find((c) => c.metric === 'role_quality');
  assert.equal(judged.source, undefined);
  assert.equal(judged.mean, 50);
  assert.deepEqual(judgesOf(entries), ['anthropic/claude-opus-5', 'codeaf/grader', 'codeaf/reviewer']);
});
