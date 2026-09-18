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
  const cells = aggregate(entries, { minInstalls: 3 });
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
  const cells = aggregate(entries, { minInstalls: 3 });
  const byModel = new Map(cells.map((c) => [c.model, c]));
  // Both judges land on one mean per model: the +10 offset is gone.
  const base = byModel.get('z-ai/glm-5.3').mean;
  const flash = byModel.get('z-ai/glm-5.3-flash').mean;
  assert.ok(Math.abs(base - 55) < 1e-9, `base ${base}`);
  assert.ok(Math.abs(flash - 75) < 1e-9, `flash ${flash}`);
  assert.ok(Math.abs((flash - base) - 20) < 1e-9);
});

test('aggregate publishes judges sorted and sorts below-minInstalls cells last', () => {
  const thin = cellKey({ role: 'worker', model: 'a/one', judge: 'j/x', door: 'task', size: 'S' });
  const fat = cellKey({ role: 'worker', model: 'b/two', judge: 'j/y', door: 'do', size: 'L' });
  const entries = [
    entry('i1', '2026-09-17', thin, 90),
    entry('i2', '2026-09-17', thin, 90),
    entry('i1', '2026-09-17', fat, 10),
    entry('i2', '2026-09-17', fat, 10),
    entry('i3', '2026-09-17', fat, 10),
  ];
  const cells = aggregate(entries, { minInstalls: 3 });
  const judges = judgesOf(entries);
  assert.deepEqual(judges, ['j/x', 'j/y']);
  assert.equal(cells[0].model, 'b/two'); // 3 installs meets the floor, sorts first
  assert.equal(cells[1].model, 'a/one'); // 2 installs falls after
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
