import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createPlaceNavigation } from './navigation.ts';
import type { PlaceKey } from '../../design/nativeControls.ts';
import type { PlacesGraph, Mutation } from './wire.ts';

const A = 'pl_0000000000000001';
const B = 'pl_0000000000000002';
function rig(desktop = false) {
  const graph: PlacesGraph = JSON.parse(readFileSync(new URL('./fixtures/graph.json', import.meta.url), 'utf8'));
  const a = graph.places.find(place => place.id === A)!;
  const b = graph.places[1];
  b.id = B;
  b.parents = [A];
  a.pinned = false;
  b.pinned = false;
  graph.rail.open = [a, b];
  let current: Exclude<PlaceKey, 'root'> = 'now';
  let focuses = 0;
  let roots = 0;
  const closed = new Set<string>();
  const calls: string[] = [];
  const warnings: unknown[] = [];
  const work = { running: true, stopCalls: 0 };
  const tabs = new Map([['now', [{ id: 'loose', draft: 'Now draft' }]], [A, [{ id: 'working', draft: 'Keep this', work }]], [B, [{ id: 'other', draft: 'Other draft' }]]]);
  let refuse = '';
  const client = {
    graph: async () => graph,
    visit: async (id: string) => {
      calls.push(`touch:${id}`);
      if (refuse === 'touch') throw new Error('Engine refused');
    },
    railOp: async (ask: { op: string; place?: string }) => {
      calls.push(`${ask.op}:${ask.place}`);
      if (ask.op === refuse) throw new Error('Engine refused');
      if (ask.op === 'close') graph.rail.open = graph.rail.open.filter(place => place.id !== ask.place);
      if (ask.op === 'visit') graph.rail.open = [graph.places.find(place => place.id === ask.place)!, ...graph.rail.open.filter(place => place.id !== ask.place)];
      return { revision: graph.revision, receipts: [], noop: true, undo: [] } as Mutation;
    },
  };
  const nav = createPlaceNavigation({
    client, windows: { desktop, openPlaceWindow: async key => { calls.push(`window:${key}`); return { moved: false }; } },
    graph: () => graph, current: () => current, setPlace: key => { current = key; },
    focusHome: () => { focuses++; }, openRoot: () => { roots++; },
    setClosed: (key, value) => { if (value) closed.add(key); else closed.delete(key); },
    refresh: async () => {}, warn: error => warnings.push(error),
  });
  return { nav, graph, calls, closed, warnings, work, tabs, client, current: () => current, focuses: () => focuses, roots: () => roots, refuse: (op: string) => { refuse = op; } };
}

test('Go to visits the Open MRU and focuses Home on every arrival without stopping work', async () => {
  const r = rig();
  const saved = r.tabs.get(A);
  await r.nav.goTo(A);
  await r.nav.goTo(B);
  await r.nav.goTo(A);
  assert.equal(r.current(), A);
  assert.equal(r.graph.rail.open[0].id, A);
  assert.equal(r.focuses(), 3);
  assert.equal(r.tabs.get(A), saved);
  assert.deepEqual(r.calls.filter(call => call.startsWith('touch:')), [`touch:${A}`, `touch:${B}`, `touch:${A}`]);
  assert.deepEqual(r.work, { running: true, stopCalls: 0 });
});

test('close leaves Now, keeps saved tabs for restore and revisits the rail on reopening', async () => {
  const r = rig();
  await r.nav.goTo(A);
  const saved = r.tabs.get(A);
  await r.nav.closePlace(A);
  assert.equal(r.current(), 'now');
  assert.ok(r.closed.has(A));
  assert.ok(!r.graph.rail.open.some(place => place.id === A));
  assert.equal(r.tabs.get(A), saved);
  await r.nav.goTo(A);
  assert.equal(r.tabs.get(r.current()), saved);
  assert.equal(r.closed.has(A), false);
  assert.equal(r.work.running, true);
});

test('bulk close keeps the chosen place and pinned places', async () => {
  const r = rig();
  const pinned = { ...r.graph.places[0], id: 'pl_00000000000000ff', pinned: true };
  r.graph.rail.open.push(pinned);
  await r.nav.goTo(A);
  await r.nav.closeAllOthers(A);
  assert.deepEqual(r.calls.filter(call => call.startsWith('close:')), [`close:${B}`]);
  assert.equal(r.current(), A);
});

test('up follows the primary parent, then opens root as a Home tab in the current window', async () => {
  const r = rig();
  r.graph.places.find(place => place.id === B)!.parents = [A, 'pl_0000000000000003'];
  await r.nav.goTo(B);
  await r.nav.up();
  assert.equal(r.current(), A);
  await r.nav.up();
  assert.equal(r.roots(), 1);
  assert.equal(r.current(), A);
});

test('native new-window navigation leaves this window and its saved tabs untouched', async () => {
  const r = rig(true);
  await r.nav.goTo(A);
  await r.nav.goTo(B, { newWindow: true });
  await r.nav.openInNewWindow('root');
  assert.equal(r.current(), A);
  assert.equal(r.focuses(), 1);
  assert.deepEqual(r.calls, [`touch:${A}`, `visit:${A}`, `window:${B}`, `touch:${B}`, `visit:${B}`, 'window:root']);
});

test('browser new-window actions fall back to Go to in this window', async () => {
  const r = rig();
  await r.nav.openInNewWindow(A);
  await r.nav.goTo(B, { newWindow: true });
  assert.equal(r.current(), B);
  assert.equal(r.focuses(), 2);
  assert.ok(!r.calls.some(call => call.startsWith('window:')));
});

test('Now and root do not write rail entries and cannot be closed', async () => {
  const r = rig();
  await r.nav.goTo('now');
  await r.nav.goTo('root');
  await r.nav.closePlace('now');
  await r.nav.closePlace('root');
  assert.deepEqual(r.calls, []);
  assert.equal(r.closed.size, 0);
  assert.equal(r.roots(), 1);
});

test('invalid, missing and archived destinations leave the window unchanged', async () => {
  const r = rig(true);
  await assert.rejects(r.nav.goTo('invalid'), /not a place/);
  await assert.rejects(r.nav.goTo('pl_ffffffffffffffff'), /no longer exists/);
  r.graph.places.find(place => place.id === B)!.archived = true;
  await assert.rejects(r.nav.openInNewWindow(B), /archived/);
  assert.equal(r.current(), 'now');
  assert.deepEqual(r.calls, []);
});

test('a refused close preserves the current workspace; a failed visit reports its error', async () => {
  const r = rig();
  await r.nav.goTo(A);
  r.refuse('close');
  await assert.rejects(r.nav.closePlace(A), /Engine refused/);
  assert.equal(r.current(), A);
  assert.equal(r.closed.size, 0);
  r.refuse('visit');
  await r.nav.goTo(B);
  assert.equal(r.current(), B);
  assert.equal(r.warnings.length, 1);
  r.refuse('touch');
  await r.nav.goTo(A);
  assert.equal(r.current(), A);
  assert.equal(r.warnings.length, 2);
});

test('a delayed lookup cannot replace a newer navigation', async () => {
  const r = rig();
  const a = r.graph.places.find(place => place.id === A)!;
  r.graph.places = r.graph.places.filter(place => place.id !== A);
  let resolve!: (graph: PlacesGraph) => void;
  r.client.graph = () => new Promise(done => { resolve = done; });
  const slow = r.nav.goTo(A);
  await r.nav.goTo(B);
  resolve({ ...r.graph, places: [...r.graph.places, a] });
  await slow;
  assert.equal(r.current(), B);
  assert.deepEqual(r.calls, [`touch:${B}`, `visit:${B}`]);
});
