import assert from 'node:assert/strict';
import test from 'node:test';
import { currentPick, pickPlace, settlePick, subscribePick } from './pickPlace.ts';

test('resolves the chosen id and clears the question', async () => {
  const asked = pickPlace({ title: 'Add to a place', exclude: ['pl_a'] });
  const request = currentPick()!;
  assert.equal(request.title, 'Add to a place');
  assert.ok(request.exclude.has('pl_a'));
  settlePick(request, 'pl_b');
  assert.equal(await asked, 'pl_b');
  assert.equal(currentPick(), undefined);
});

test('a dismissal resolves undefined', async () => {
  const asked = pickPlace({ title: 'Add to another place' });
  settlePick(currentPick()!, undefined);
  assert.equal(await asked, undefined);
});

test('a newer ask dismisses the older one, and a late answer to it is ignored', async () => {
  const first = pickPlace({ title: 'one' });
  const stale = currentPick()!;
  const second = pickPlace({ title: 'two' });
  assert.equal(await first, undefined);
  settlePick(stale, 'pl_x');
  assert.equal(currentPick()?.title, 'two');
  settlePick(currentPick()!, 'pl_y');
  assert.equal(await second, 'pl_y');
});

test('listeners hear the ask and the answer', async () => {
  let heard = 0;
  const off = subscribePick(() => { heard += 1; });
  const asked = pickPlace({ title: 't' });
  settlePick(currentPick()!, 'pl_z');
  await asked;
  off();
  assert.equal(heard, 2);
});

test('create is carried on the request', async () => {
  const asked = pickPlace({ title: 't', create: async name => `pl_${name}` });
  const request = currentPick()!;
  assert.equal(await request.create!('new'), 'pl_new');
  settlePick(request, 'pl_new');
  assert.equal(await asked, 'pl_new');
});
