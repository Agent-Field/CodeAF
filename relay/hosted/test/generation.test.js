// The generation fence: a device carrying an older opening's generation is told gone; one carrying none is let in.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fence, generationOf, newGeneration } from '../src/pair/generation.js';

const asking = (gen) => ({ headers: new Headers(gen === undefined ? {} : { 'codeaf-pair-gen': gen }) });
const verdict = (box, asked) => {
  try {
    fence(box, asked);
    return 'in';
  } catch (e) {
    return [e.code, e.status].join(' ');
  }
};

test('a generation is 32 hex characters and never repeats', () => {
  const some = Array.from({ length: 500 }, newGeneration);
  for (const g of some) assert.match(g, /^[0-9a-f]{32}$/);
  assert.equal(new Set(some).size, 500);
});

test('a request carries a generation, or none, or a malformed one that matches no box', () => {
  const g = newGeneration();
  assert.equal(generationOf(asking(g)), g);
  assert.equal(generationOf(asking()), null);
  for (const bad of ['', 'short', 'G'.repeat(32), `${g}0`]) {
    assert.equal(verdict({ gen: g }, generationOf(asking(bad))), 'gone 404', `"${bad}" is fenced`);
  }
});

test('an older generation is gone; the same one and an absent one are let in', () => {
  const [old, now] = [newGeneration(), newGeneration()];
  assert.equal(verdict({ gen: now }, old), 'gone 404');
  assert.equal(verdict({ gen: now }, now), 'in');
  assert.equal(verdict({ gen: now }, null), 'in', 'an old client sends none');
  assert.equal(verdict({}, old), 'in', 'a box opened before generations existed is not fenced');
});
