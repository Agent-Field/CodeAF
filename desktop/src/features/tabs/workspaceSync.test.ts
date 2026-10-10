import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createWorkspaceSync, saveDelayMs, v1ImportedKey, v1StorageKey, viewStorageKey,
  type WindowView, type WorkspaceNotices, type WorkspaceTransport,
} from './workspaceSync.ts';

type Tab = { id: string; title: string };
type State = { tabs: Tab[]; activeId?: string; split: Record<string, number>; scroll: Record<string, number> };

const tab = (id: string): Tab => ({ id, title: id });
const state = (ids: string[], over: Partial<State> = {}): State => ({
  tabs: ids.map(tab), activeId: ids[0], split: {}, scroll: {}, ...over,
});

function deserialize(document: unknown, view: WindowView): State {
  const tabs = document && typeof document === 'object' && Array.isArray((document as { tabs?: unknown }).tabs)
    ? (document as { tabs: Tab[] }).tabs : [];
  const ids = new Set(tabs.map(item => item.id));
  return {
    tabs,
    activeId: view.activeId && ids.has(view.activeId) ? view.activeId : tabs[0]?.id,
    split: Object.fromEntries(Object.entries(view.split).filter(([id]) => ids.has(id))),
    scroll: Object.fromEntries(Object.entries(view.scroll).filter(([id]) => ids.has(id))),
  };
}

const serialize = (value: State) => value;

function memoryStorage(seed: Record<string, string> = {}): Storage {
  const map = new Map(Object.entries(seed));
  return {
    get length() { return map.size; },
    clear() { map.clear(); },
    getItem: key => map.has(key) ? map.get(key)! : null,
    setItem: (key, value) => { map.set(key, String(value)); },
    removeItem: key => { map.delete(key); },
    key: index => [...map.keys()][index] ?? null,
  };
}

type PutBody = { revision: number; writer: string; workspace: { tabs?: Tab[]; activeId?: string; split?: unknown; scroll?: unknown } };
type Call = { method: string; match?: string; body?: PutBody; status: number };

function bridge() {
  const docs = new Map<string, { revision: number; writer?: string; workspace: unknown }>();
  const notices = new Map<string, { key: string; revision: number; writer?: string }>();
  const listeners = new Set<() => void>();
  const calls: Call[] = [];
  let quiet = false;
  const api: WorkspaceNotices = {
    subscribe(fn) { listeners.add(fn); return () => { listeners.delete(fn); }; },
    lastWorkspace: key => notices.get(key),
  };
  const transport: WorkspaceTransport = async (path, init) => {
    const key = decodeURIComponent(path.replace('/workspaces/', ''));
    const current = docs.get(key) ?? { revision: 0, workspace: null as unknown };
    if (init.method === 'GET') {
      calls.push({ method: 'GET', status: 200 });
      return { status: 200, body: { key, revision: current.revision, writer: current.writer, workspace: current.workspace } };
    }
    const payload = JSON.parse(init.body ?? '{}') as PutBody;
    const match = init.headers?.['If-Match'];
    if (match !== String(payload.revision)) {
      calls.push({ method: 'PUT', match, body: payload, status: 400 });
      return { status: 400, body: { error: 'If-Match and revision diverged' } };
    }
    if (Number(match) !== current.revision) {
      calls.push({ method: 'PUT', match, body: payload, status: 412 });
      return { status: 412, body: { code: 'conflict', current: { key, revision: current.revision, writer: current.writer, workspace: current.workspace } } };
    }
    const next = { revision: current.revision + 1, writer: payload.writer, workspace: payload.workspace };
    docs.set(key, next);
    calls.push({ method: 'PUT', match, body: payload, status: 200 });
    if (!quiet) {
      notices.set(key, { key, revision: next.revision, writer: next.writer });
      for (const listener of [...listeners]) listener();
    }
    return { status: 200, body: { key, ...next } };
  };
  return {
    transport, notices: api, docs, calls,
    set quiet(value: boolean) { quiet = value; },
  };
}

function useClock() {
  let n = 0;
  const timers = new Map<number, { fn: () => unknown; ms: number }>();
  return {
    clock: {
      set(fn: () => unknown, ms: number) { const id = ++n; timers.set(id, { fn, ms }); return id; },
      clear(id: unknown) { timers.delete(id as number); },
    },
    delays: () => [...timers.values()].map(timer => timer.ms),
    async advance() {
      const batch = [...timers.values()];
      timers.clear();
      await Promise.all(batch.map(timer => Promise.resolve(timer.fn())));
    },
  };
}

const ids = (workspace: unknown) => {
  if (!workspace || typeof workspace !== 'object' || !Array.isArray((workspace as { tabs?: unknown }).tabs)) return [];
  return (workspace as { tabs: Tab[] }).tabs.map(item => item.id);
};

async function until(ready: () => boolean) {
  for (let i = 0; i < 40; i++) {
    if (ready()) return;
    await new Promise<void>(resolve => setImmediate(resolve));
  }
  throw new Error('timed out');
}

const puts = (calls: Call[], writer?: string) => calls.filter(call => call.method === 'PUT' && (!writer || call.body?.writer === writer));

test('two syncs on one fake bridge converge', async () => {
  const box = bridge();
  const left = useClock();
  const right = useClock();
  const sessionA = memoryStorage();
  const sessionB = memoryStorage();
  const fromA: State[] = [];
  const fromB: State[] = [];
  const a = createWorkspaceSync({
    key: 'now', writer: 'win-a', windowLabel: 'main', session: sessionA, local: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: left.clock, serialize, deserialize,
  });
  const b = createWorkspaceSync({
    key: 'now', writer: 'win-b', windowLabel: 'w-2', session: sessionB, local: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: right.clock, serialize, deserialize,
  });
  a.onRemote(state => fromA.push(state));
  b.onRemote(state => fromB.push(state));
  await a.load();
  await b.load();
  a.save(state(['a'], { activeId: 'a', scroll: { a: 12 } }));
  assert.deepEqual(left.delays(), [saveDelayMs]);
  assert.equal(puts(box.calls).length, 0);
  await left.advance();
  await until(() => fromB.length === 1);
  assert.deepEqual(fromB.at(-1)!.tabs.map(item => item.id), ['a']);
  assert.equal(fromA.length, 0);
  assert.equal(JSON.parse(sessionA.getItem(viewStorageKey('main', 'now'))!).activeId, 'a');
  assert.equal(JSON.parse(sessionA.getItem(viewStorageKey('main', 'now'))!).scroll.a, 12);
  const first = puts(box.calls, 'win-a').at(-1)!;
  assert.equal(first.match, '0');
  assert.equal(first.body!.workspace.activeId, undefined);
  assert.equal(first.body!.workspace.scroll, undefined);
  b.save(state(['a', 'b'], { activeId: 'b' }));
  await right.advance();
  await until(() => fromA.length === 1);
  assert.deepEqual(fromA.at(-1)!.tabs.map(item => item.id), ['a', 'b']);
  assert.equal(fromA.at(-1)!.activeId, 'a');
  assert.equal(fromB.length, 1);
  assert.deepEqual(ids(box.docs.get('now')!.workspace), ['a', 'b']);
  const before = puts(box.calls).length;
  a.save(state(['a', 'b'], { activeId: 'b', scroll: { a: 40 } }));
  await left.advance();
  assert.equal(puts(box.calls).length, before);
  assert.equal(JSON.parse(sessionA.getItem(viewStorageKey('main', 'now'))!).activeId, 'b');
  assert.equal(JSON.parse(sessionB.getItem(viewStorageKey('w-2', 'now'))!).activeId, 'b');
  a.close();
  b.close();
});

test('a stale write rebases without losing the other window\'s new tab', async () => {
  const box = bridge();
  const left = useClock();
  const right = useClock();
  const sessionB = memoryStorage();
  const seen: State[] = [];
  const a = createWorkspaceSync({
    key: 'now', writer: 'win-a', windowLabel: 'main', session: memoryStorage(), local: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: left.clock, serialize, deserialize,
  });
  const b = createWorkspaceSync({
    key: 'now', writer: 'win-b', windowLabel: 'w-2', session: sessionB, local: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: right.clock, serialize, deserialize,
  });
  b.onRemote(next => seen.push(next));
  await a.load();
  await b.load();
  a.save(state(['a']));
  await left.advance();
  await until(() => seen.length === 1);
  box.quiet = true;
  a.save(state(['a', 'b']));
  await left.advance();
  assert.deepEqual(ids(box.docs.get('now')!.workspace), ['a', 'b']);
  box.quiet = false;
  b.save(state(['a', 'c'], { activeId: 'c', split: { a: 1, gone: 2 }, scroll: { a: 8, gone: 3 } }));
  await right.advance();
  assert.deepEqual(ids(box.docs.get('now')!.workspace), ['a', 'b', 'c']);
  const mine = puts(box.calls, 'win-b');
  assert.deepEqual(mine.map(call => call.status), [412, 200]);
  assert.deepEqual(mine.map(call => call.match), ['1', '2']);
  assert.equal(mine[1].body!.workspace.activeId, undefined);
  assert.equal(mine[1].body!.workspace.split, undefined);
  assert.equal(mine[1].body!.workspace.scroll, undefined);
  const view = JSON.parse(sessionB.getItem(viewStorageKey('w-2', 'now'))!);
  assert.equal(view.activeId, 'c');
  assert.deepEqual(view.split, { a: 1 });
  assert.deepEqual(view.scroll, { a: 8 });
  assert.deepEqual(seen.at(-1)!.tabs.map(item => item.id), ['a', 'b', 'c']);
  assert.equal(seen.at(-1)!.activeId, 'c');
  a.close();
  b.close();
});

test('v1 import happens once', async () => {
  const box = bridge();
  const local = memoryStorage({ [v1StorageKey]: JSON.stringify(state(['v1'], { activeId: 'v1', scroll: { v1: 4 } })) });
  const session = memoryStorage();
  const first = createWorkspaceSync({
    key: 'now', writer: 'win-a', windowLabel: 'main', local, session, transport: box.transport, notices: box.notices,
    clock: useClock().clock, serialize, deserialize,
  });
  const loaded = await first.load();
  assert.deepEqual(loaded.tabs.map(item => item.id), ['v1']);
  assert.equal(loaded.activeId, 'v1');
  assert.equal(loaded.scroll.v1, 4);
  assert.equal(local.getItem(v1ImportedKey), '1');
  assert.match(local.getItem(v1StorageKey) ?? '', /"id":"v1"/);
  assert.equal(puts(box.calls).length, 1);
  assert.equal(puts(box.calls)[0].match, '0');
  assert.equal(puts(box.calls)[0].body!.workspace.activeId, undefined);
  box.docs.set('now', { revision: 0, workspace: null });
  const second = createWorkspaceSync({
    key: 'now', writer: 'win-b', windowLabel: 'w-2', local, session: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: useClock().clock, serialize, deserialize,
  });
  const again = await second.load();
  assert.deepEqual(again.tabs, []);
  assert.equal(puts(box.calls).length, 1);
  assert.match(local.getItem(v1StorageKey) ?? '', /"id":"v1"/);
  first.close();
  second.close();
});

test('own echo is ignored', async () => {
  const box = bridge();
  const clock = useClock();
  const mine: State[] = [];
  const theirs: State[] = [];
  const a = createWorkspaceSync({
    key: 'now', writer: 'win-a', windowLabel: 'main', session: memoryStorage(), local: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: clock.clock, serialize, deserialize,
  });
  const b = createWorkspaceSync({
    key: 'now', writer: 'win-b', windowLabel: 'w-2', session: memoryStorage(), local: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: useClock().clock, serialize, deserialize,
  });
  a.onRemote(next => mine.push(next));
  b.onRemote(next => theirs.push(next));
  await a.load();
  await b.load();
  a.save(state(['a']));
  await clock.advance();
  await until(() => theirs.length === 1);
  assert.equal(mine.length, 0);
  assert.deepEqual(theirs[0].tabs.map(item => item.id), ['a']);
  a.close();
  b.close();
});

test('offline edits stay in memory and flush on reconnect', async () => {
  const box = bridge();
  const clock = useClock();
  let online = true;
  let wake = () => {};
  const sync = createWorkspaceSync({
    key: 'now', writer: 'win-a', windowLabel: 'main', session: memoryStorage(), local: memoryStorage(),
    transport: box.transport, notices: box.notices, clock: clock.clock, serialize, deserialize,
    online: () => online,
    listenOnline: retry => { wake = retry; return () => { wake = () => {}; }; },
  });
  await sync.load();
  online = false;
  sync.save(state(['a'], { activeId: 'a' }));
  await clock.advance();
  assert.equal(puts(box.calls).length, 0);
  assert.deepEqual((await sync.load()).tabs.map(item => item.id), ['a']);
  online = true;
  wake();
  await until(() => puts(box.calls).some(call => call.status === 200));
  assert.deepEqual(ids(box.docs.get('now')!.workspace), ['a']);
  sync.close();
});
