import test from 'node:test';
import assert from 'node:assert/strict';
import { createToasts } from './toasts.ts';

test('a new toast replaces the current one and settles it', () => {
  const t = createToasts();
  const log: string[] = [];
  const first = t.show({ message: ['one'], onSettled: () => log.push('one settled') });
  const second = t.show({ message: ['two'] });
  assert.notEqual(first, second);
  assert.deepEqual(log, ['one settled']);
  assert.equal(t.getToast()?.id, second);
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
  t.dismiss(old); t.undo(old); t.act(old, 0);
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
