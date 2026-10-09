import test from 'node:test';
import assert from 'node:assert/strict';
import { bindingsKey } from './bindings.ts';
import { bindingFor, legacyTerminalView, terminalTargetOf, terminalView } from './target.ts';
import { handoffFromPane, handoffProblem, paneFromHandoff } from '../../design/nativeControls.ts';
import type { Pane } from '../tabs/types.ts';

// A minimal localStorage, so the legacy binding store can be read without a browser.
const store = new Map<string, string>();
Object.assign(globalThis, { localStorage: { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v), removeItem: (k: string) => void store.delete(k) } });
const legacy = (all: Record<string, unknown>) => store.set(bindingsKey, JSON.stringify(all));

const FILE = '/home/me/.codeaf/v3/projects/x/abc/session.jsonl';
const shell = (over: Partial<Pane> = {}): Pane => ({ id: 'p1', kind: 'terminal', title: 'zsh', draft: '', ...over });

test('a pane names its terminal only when it has both the conversation and the terminal id', () => {
  assert.deepEqual(terminalTargetOf({ sessionFile: FILE, target: { terminalId: 't1' } }), { sessionFile: FILE, terminalId: 't1' });
  assert.equal(terminalTargetOf({ sessionFile: FILE }), undefined);
  assert.equal(terminalTargetOf({ target: { terminalId: 't1' } }), undefined);
});

test('the durable view never carries the bridge session id', () => {
  assert.deepEqual(terminalView({ sessionFile: FILE, terminalId: 't1' }), { sessionFile: FILE, target: { terminalId: 't1' } });
});

test('a copy of the pane under a new id shows the same terminal without any binding saved for the new id', () => {
  store.clear();
  legacy({ p1: { sessionFile: 'old.jsonl', terminalId: 'old' } });
  const original = shell({ ...terminalView({ sessionFile: FILE, terminalId: 't1' }) });
  const copy = { ...original, id: 'p2' };
  assert.deepEqual(bindingFor(copy), { sessionFile: FILE, terminalId: 't1' });
});

test('the pane own target wins over a stale binding saved under the same id', () => {
  store.clear();
  legacy({ p1: { sessionFile: 'old.jsonl', terminalId: 'old' } });
  assert.equal(bindingFor(shell({ ...terminalView({ sessionFile: FILE, terminalId: 't1' }) }))?.terminalId, 't1');
});

test('a pane saved before the durable target hydrates from its binding, once', () => {
  store.clear();
  legacy({ p1: { sessionFile: FILE, terminalId: 't7' }, p3: { sessionFile: FILE, refused: '16 terminals are already running; close one first' } });
  assert.deepEqual(legacyTerminalView(shell()), { sessionFile: FILE, target: { terminalId: 't7' } });
  assert.equal(legacyTerminalView(shell({ ...terminalView({ sessionFile: FILE, terminalId: 't7' }) })), undefined);
  assert.equal(legacyTerminalView(shell({ id: 'p3' })), undefined, 'a refusal has no terminal to name');
  assert.equal(legacyTerminalView(shell({ id: 'p9' })), undefined);
});

test('a half-named target is not trusted: it falls back to the binding, or to none', () => {
  store.clear();
  assert.equal(bindingFor(shell({ target: { terminalId: 't1' } })), undefined);
});

test('a handoff of a terminal carries its durable target, and the receiving window gets the same shell under a new id', () => {
  store.clear();
  const tab = handoffFromPane(shell({ ...terminalView({ sessionFile: FILE, terminalId: 't1' }), target: { terminalId: 't1', sessionId: 'opaque-bridge-id' } }));
  assert.deepEqual(tab, { kind: 'terminal', title: 'zsh', sessionFile: FILE, target: { terminalId: 't1' } });
  assert.equal(handoffProblem(tab), undefined);
  const received = paneFromHandoff(tab, 'p-new');
  assert.equal(received.id, 'p-new');
  assert.deepEqual(bindingFor(received), { sessionFile: FILE, terminalId: 't1' });
});

test('a terminal not yet hydrated is captured from its legacy binding when it moves', () => {
  store.clear();
  legacy({ p1: { sessionFile: FILE, terminalId: 't7' } });
  const tab = handoffFromPane(shell());
  assert.deepEqual(tab, { kind: 'terminal', title: 'zsh', sessionFile: FILE, target: { terminalId: 't7' } });
  assert.deepEqual(bindingFor(paneFromHandoff(tab, 'p-new')), { sessionFile: FILE, terminalId: 't7' });
});

test('a move that fails leaves the source tab in place: nothing is removed without a claim', async () => {
  const { createNativeControls } = await import('../../design/nativeControls.ts');
  const calls: string[] = [];
  const bridge = {
    desktop: true,
    async invoke(command: string) { calls.push(command); throw new Error('That window is gone'); },
    async listen(_event: string, _handler: unknown) { return () => {}; },
    location: { pathname: '/', search: '' },
    openBrowserTab: () => {},
  };
  const native = createNativeControls(bridge as never);
  const removed: string[] = [];
  await native.onHandoffClaimed(id => removed.push(id));
  await assert.rejects(native.moveTabToWindow('w-2', shell({ ...terminalView({ sessionFile: FILE, terminalId: 't1' }) })), /gone/);
  assert.deepEqual(calls, ['window_move_tab']);
  assert.deepEqual(removed, []);
});
