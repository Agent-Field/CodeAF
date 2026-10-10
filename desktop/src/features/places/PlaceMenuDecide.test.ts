import test from 'node:test';
import assert from 'node:assert/strict';
import { decideEntries, decideText, putDecide, type DecideChange } from './PlaceMenuDecide.ts';


test('nothing is drawn without the engine\'s figures or a handler', () => {
  assert.deepEqual(decideEntries('p1', undefined, () => undefined), []);
  assert.deepEqual(decideEntries('p1', { alwaysAsk: false, threshold: 90 }, undefined), []);
});

test('Always ask me is a checked toggle and keeps the threshold out of the change', () => {
  const calls: DecideChange[] = [];
  const [ask] = decideEntries('p1', { alwaysAsk: true, threshold: 80 }, (_id, change) => void calls.push(change));
  assert.ok(ask.kind !== 'separator' && ask.kind !== 'submenu' && ask.kind !== 'swatches' && ask.checked === true);
  if (ask.kind !== 'separator' && ask.kind !== 'submenu' && ask.kind !== 'swatches') ask.onSelect();
  assert.deepEqual(calls, [{ alwaysAsk: false }]);
});

test('the confidence submenu checks the current figure, keeps one the presets lack, and writes the pick', () => {
  const calls: DecideChange[] = [];
  const sub = decideEntries('p1', { alwaysAsk: false, threshold: 85 }, (_id, change) => void calls.push(change))[1];
  assert.equal(sub.kind, 'submenu');
  if (sub.kind !== 'submenu') return;
  const checked = sub.items.filter(item => item.kind !== 'separator' && item.kind !== 'submenu' && item.kind !== 'swatches' && item.checked);
  assert.deepEqual(checked.map(item => (item.kind === 'separator' || item.kind === 'swatches' ? '' : item.label)), ['85%']);
  const hundred = sub.items.find(item => item.kind !== 'separator' && item.kind !== 'swatches' && item.label === '100%');
  if (hundred && hundred.kind !== 'separator' && hundred.kind !== 'submenu' && hundred.kind !== 'swatches') hundred.onSelect();
  assert.deepEqual(calls, [{ threshold: 100 }]);
});

test('the PUT carries the effective always-ask and only a threshold the person chose', async () => {
  const bodies: Record<string, unknown>[] = [];
  const answer = { revision: 4, receipts: [{ id: 'r1', action: 'place.decide', beforeRevision: 3, afterRevision: 4 }], undo: ['r1'], noop: false };
  const client = { setDecide: async (_id: string, body: Readonly<Record<string, unknown>>) => { bodies.push({ ...body }); return answer; } };
  const mutation = await putDecide('p1', { alwaysAsk: true }, { alwaysAsk: false, threshold: 90 }, client);
  await putDecide('p1', { threshold: 70 }, { alwaysAsk: true, threshold: 90 }, client);
  assert.deepEqual(bodies, [{ alwaysAsk: true }, { alwaysAsk: true, threshold: 70 }]);
  assert.deepEqual(mutation.undo, ['r1']);
});

test('the toast says what is now true', () => {
  assert.equal(decideText('Release', { alwaysAsk: true }), '“Release” will always ask you');
  assert.equal(decideText('Release', { alwaysAsk: false }), '“Release” decides automatically again');
  assert.equal(decideText('Release', { threshold: 70 }), 'Decision confidence for “Release” is now 70%');
});
