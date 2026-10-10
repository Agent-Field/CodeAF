import assert from 'node:assert/strict';
import test from 'node:test';
import { dragRelease, mayTearOff, tabMoveToWindow, tearOffAt, type DragRelease } from './tearOff.ts';
import type { Tab } from '../model.ts';

const box = { width: 800, height: 600 };
const tab = (over: Partial<Tab> = {}): Tab => ({ id: 't', kind: 'conversation', title: 'Config stack', draft: 'words', pinned: false, ...over });
const end = (over: Partial<DragRelease> = {}): DragRelease => ({ dropEffect: 'none', clientX: -4, clientY: 20, screenX: 30, screenY: 80, ...over });

test('a refused drop outside the window is the pointer, including a display to the left', () => {
  assert.deepEqual(tearOffAt(end(), box), { x: 30, y: 80 });
  assert.deepEqual(tearOffAt(end({ clientX: 10, clientY: -1, screenX: -20, screenY: 400 }), box), { x: -20, y: 400 });
  assert.deepEqual(tearOffAt(end({ clientX: 800, clientY: 10, screenX: 900, screenY: 10 }), box), { x: 900, y: 10 });
  assert.deepEqual(tearOffAt(end({ clientX: 10, clientY: 600, screenX: 10, screenY: 700 }), box), { x: 10, y: 700 });
});

test('a drop somebody accepted, or a release still inside the window, opens nothing', () => {
  for (const dropEffect of ['move', 'copy', 'link']) assert.equal(tearOffAt(end({ dropEffect, clientX: -1 }), box), undefined);
  assert.equal(tearOffAt(end({ clientX: 0, clientY: 0 }), box), undefined);
  assert.equal(tearOffAt(end({ clientX: 799, clientY: 599 }), box), undefined);
  assert.equal(tearOffAt(end({ clientX: Number.NaN }), box), undefined);
  assert.equal(tearOffAt(end({ screenX: Number.POSITIVE_INFINITY }), box), undefined);
  assert.equal(tearOffAt(end(), { width: 0, height: 600 }), undefined);
});

test('a missing transfer counts as a refused drop', () => {
  assert.deepEqual(dragRelease({ dataTransfer: null, clientX: -1, clientY: 2, screenX: 3, screenY: 4 }), { dropEffect: 'none', clientX: -1, clientY: 2, screenX: 3, screenY: 4 });
});

test('pinned tabs and Home never tear off, and a tab that cannot move does not either', () => {
  assert.equal(mayTearOff(tab(), true), true);
  assert.equal(mayTearOff(tab({ pinned: true }), true), false);
  assert.equal(mayTearOff(tab(), false), false);
  assert.equal(mayTearOff(tab({ kind: 'home', pinned: true }), false), false);
});

test('tabMoveToWindow calls the window door once with that tab and does not stop the work itself', () => {
  const calls: string[] = [];
  tabMoveToWindow(tab(), moved => { calls.push(moved.id); return Promise.resolve(true); });
  assert.deepEqual(calls, ['t']);
  let called = false;
  tabMoveToWindow(tab({ pinned: true }), () => { called = true; });
  assert.equal(called, true, 'the guard lives with the drag, not inside the door');
});
