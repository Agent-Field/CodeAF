import test from 'node:test';
import assert from 'node:assert/strict';
import { createTabActions, type NativeForTabs } from './actions.ts';
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

test('a tab with nothing durable behind it has no link, so Copy link copies nothing and says nothing', async () => {
  let wrote = false;
  const toasts = createToasts();
  const actions = createTabActions({ native: native(), toasts, writeClipboard: async () => { wrote = true; } });
  assert.equal(actions.linkFor(tab()), undefined);
  assert.equal(await actions.copyLink(tab()), false);
  assert.equal(wrote, false);
  assert.equal(toasts.getToast(), null);
});

const SAVED = '/srv/store/projects/-srv-app/9446cc2627f3deae/transcript.jsonl';

test('a saved conversation copies its codeaf link and the shared toast says so', async () => {
  const written: string[] = [];
  const toasts = createToasts();
  const actions = createTabActions({ native: native(), toasts, writeClipboard: async text => { written.push(text); } });
  assert.equal(actions.linkFor(tab({ sessionFile: SAVED })), 'codeaf://chat/9446cc2627f3deae');
  assert.equal(await actions.copyLink(tab({ sessionFile: SAVED })), true);
  assert.deepEqual(written, ['codeaf://chat/9446cc2627f3deae']);
  assert.deepEqual(toasts.getToast()?.message, ['Copied the link to ', { strong: 'Config stack' }]);
  assert.equal(toasts.getToast()?.tone, 'info');
});

test('a clipboard that refuses is said in a danger toast, never as copied', async () => {
  const toasts = createToasts();
  const actions = createTabActions({ native: native(), toasts, writeClipboard: async () => { throw new Error('denied'); } });
  assert.equal(await actions.copyLink(tab({ sessionFile: SAVED })), false);
  assert.deepEqual(toasts.getToast()?.message, ['Could not copy the link to ', { strong: 'Config stack' }]);
  assert.equal(toasts.getToast()?.tone, 'danger');
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
