import test from 'node:test';
import assert from 'node:assert/strict';
import { createToasts } from './toasts.ts';

test('one toast is shown; a newer one replaces it and settles the one that left', () => {
  const t = createToasts();
  const log: string[] = [];
  const first = t.show({ message: ['one'], onSettled: () => log.push('one settled') });
  const second = t.show({ message: ['two'] });
  assert.notEqual(first, second);
  assert.deepEqual(log, ['one settled']);
  assert.deepEqual(t.getToasts().map(toast => toast.id), [second]);
  assert.equal(t.getToast()?.id, second);
});

test('showing a key that is already up replaces that toast in its place and settles it', () => {
  const t = createToasts(2);
  const log: string[] = [];
  t.show({ message: ['a'], key: 'k', onSettled: () => log.push('a settled') });
  const other = t.show({ message: ['b'] });
  const again = t.show({ message: ['c'], key: 'k' });
  assert.deepEqual(log, ['a settled']);
  assert.deepEqual(t.getToasts().map(toast => toast.id), [again, other]);
});

test('an asynchronous Undo keeps the toast until it resolves, and a refusal keeps it standing', async () => {
  const t = createToasts();
  let release!: () => void;
  const id = t.show({ message: ['x'], undo: () => new Promise<void>(resolve => { release = resolve; }) });
  const done = t.undo(id);
  assert.equal(t.getToast()?.id, id);
  release();
  await done;
  assert.equal(t.getToast(), null);
  const refused = t.show({ message: ['y'], undo: async () => { throw new Error('moved since'); } });
  await assert.rejects(t.undo(refused), /moved since/);
  assert.equal(t.getToast()?.id, refused);
});

test('undo runs the undo, removes the toast and does not settle it', () => {
  const t = createToasts();
  const log: string[] = [];
  const id = t.show({ message: ['x'], undo: () => log.push('undo'), onSettled: () => log.push('settled') });
  t.undo(id);
  assert.deepEqual(log, ['undo']);
  assert.equal(t.getToast(), null);
});

test('an action runs, removes the toast and then settles it', () => {
  const t = createToasts();
  const log: string[] = [];
  const id = t.show({ message: ['x'], actions: [{ label: 'Stop it', onSelect: () => log.push('stop') }], onSettled: () => log.push('settled') });
  t.act(id, 0);
  assert.deepEqual(log, ['stop', 'settled']);
  assert.equal(t.getToast(), null);
});

test('a stale id changes nothing, and tone defaults to info', () => {
  const t = createToasts();
  const old = t.show({ message: ['a'] });
  const now = t.show({ message: ['b'], tone: 'danger' });
  t.dismiss(old);
  t.dismiss(old); void t.undo(old); void t.act(old, 0);
  assert.deepEqual(t.getToasts().map(toast => toast.id), [now]);
  assert.equal(t.getToast()?.id, now);
  assert.equal(createToasts().show({ message: [] }), 1);
  assert.equal(t.getToast()?.tone, 'danger');
});

test('listeners hear every change and can leave', () => {
  const t = createToasts();
  let heard = 0;
  const off = t.subscribe(() => heard++);
  const id = t.show({ message: ['a'] });
  t.dismiss(id);
  off();
  t.show({ message: ['b'] });
  assert.equal(heard, 2);
});
