import test from 'node:test';
import assert from 'node:assert/strict';
import { createTabActions, HANDOFF_CLAIM_WAIT_MS, type NativeForTabs } from './actions.ts';
import { createToasts } from '../../design/toasts.ts';
import type { Tab } from './model.ts';

const tab = (over: Partial<Tab> = {}): Tab => ({ id: 't', kind: 'conversation', title: 'Config stack', draft: '', pinned: false, ...over });
const native = (over: Partial<NativeForTabs> = {}): NativeForTabs => ({
  desktop: true,
  currentWindow: async () => ({ label: 'main', placeKey: 'now' }),
  openPlaceWindow: async () => ({ label: 'w-1', handoffId: 'h1', moved: true }),
  moveTabToWindow: async () => ({ handoffId: 'h2' }),
  ...over,
});

test('there is no canonical link today, so Copy link yields nothing and copies nothing', async () => {
  let wrote = false;
  const actions = createTabActions({ native: native(), toasts: createToasts(), writeClipboard: async () => { wrote = true; } });
  assert.equal(actions.linkFor(tab()), undefined);
  assert.equal(await actions.copyLink(tab()), false);
  assert.equal(wrote, false);
});

test('a tab can move only in the desktop app, as one pane, and never the Inbox', () => {
  const on = createTabActions({ native: native(), toasts: createToasts() });
  assert.equal(on.canMove(tab()), true);
  assert.equal(on.canMove(tab({ kind: 'inbox' })), false);
  assert.equal(on.canMove(tab({ split: { layout: '1x2', focus: 0, panes: [] } })), false);
  assert.equal(createTabActions({ native: native({ desktop: false }), toasts: createToasts() }).canMove(tab()), false);
});

test('moving to a new window opens this window\'s place with the tab as the handoff and says nothing on success', async () => {
  const calls: unknown[] = [];
  const toasts = createToasts();
  const actions = createTabActions({ native: native({ openPlaceWindow: async (place, options) => { calls.push([place, options?.pane?.id]); return { label: 'w-1', handoffId: 'h1', moved: true }; } }), toasts });
  assert.equal(await actions.moveToNewWindow(tab()), true);
  assert.deepEqual(calls, [['now', 't']]);
  assert.equal(toasts.getToast(), null);
});

test('a refused or unmoved handoff leaves the tab and says so in a danger toast', async () => {
  for (const openPlaceWindow of [async () => { throw new Error('refused'); }, async () => ({ moved: false })] as NativeForTabs['openPlaceWindow'][]) {
    const toasts = createToasts();
    const actions = createTabActions({ native: native({ openPlaceWindow }), toasts });
    assert.equal(await actions.moveToNewWindow(tab()), false);
    assert.equal(toasts.getToast()?.tone, 'danger');
    assert.deepEqual(toasts.getToast()?.message, ['Could not move ', { strong: 'Config stack' }, ' to a new window. It is still here.']);
  }
});

test('moving into an open window reports its failure the same way', async () => {
  const toasts = createToasts();
  const actions = createTabActions({ native: native({ moveTabToWindow: async () => { throw new Error('no such window'); } }), toasts });
  assert.equal(await actions.moveToWindow(tab(), 'w-9'), false);
  assert.equal(toasts.getToast()?.tone, 'danger');
});

test('an unclaimed handoff is reported once after the wait, and the tab is untouched', async () => {
  const timers: Array<[() => void, number]> = [];
  const toasts = createToasts();
  let here = true;
  const actions = createTabActions({ native: native(), toasts, stillHere: () => here, setTimer: (fn, ms) => { timers.push([fn, ms]); } });
  assert.equal(await actions.moveToNewWindow(tab()), true);
  assert.equal(timers.length, 1);
  assert.equal(timers[0][1], HANDOFF_CLAIM_WAIT_MS);
  assert.equal(HANDOFF_CLAIM_WAIT_MS > 60_000, true);
  assert.equal(toasts.getToast(), null);
  timers[0][0]();
  assert.deepEqual(toasts.getToast()?.message, [{ strong: 'Config stack' }, ' was not taken by the new window. It is still here.']);
  assert.equal(toasts.getToast()?.tone, 'danger');
  assert.equal(here, true);
});

test('a claimed or closed tab produces no timeout notice', async () => {
  for (const move of [(a: ReturnType<typeof createTabActions>) => a.moveToNewWindow(tab()), (a: ReturnType<typeof createTabActions>) => a.moveToWindow(tab(), 'w-2')]) {
    const timers: Array<() => void> = [];
    const toasts = createToasts();
    const actions = createTabActions({ native: native(), toasts, stillHere: () => false, setTimer: fn => { timers.push(fn); } });
    assert.equal(await move(actions), true);
    timers[0]();
    assert.equal(toasts.getToast(), null);
  }
});

test('no timer is set when a move fails to start', async () => {
  const timers: unknown[] = [];
  const actions = createTabActions({ native: native({ openPlaceWindow: async () => { throw new Error('x'); } }), toasts: createToasts(), stillHere: () => true, setTimer: () => { timers.push(1); } });
  assert.equal(await actions.moveToNewWindow(tab()), false);
  assert.deepEqual(timers, []);
});
