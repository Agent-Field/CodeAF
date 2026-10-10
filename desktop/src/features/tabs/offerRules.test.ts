import test from 'node:test';
import assert from 'node:assert/strict';
import { canonicalSetKey, chatIDOf, findOffers, folderOf, looseChatIDs, mergeOffers, nextOffer, offerMemoryDays, offerStorageKey, readMemory, remember, shouldAskCanonical, tabOffersForChats, writeMemory } from './offerRules.ts';
import { setIdSource } from './helpers.ts';
import { workspaceReducer, type Tab, type WorkspaceState } from './model.ts';

let n = 0;
setIdSource(() => `g${++n}`);
const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const day = 86_400_000;

test('three loose tabs on one saved session are one offer, named by the conversation', () => {
  const tabs = [tab('c', { title: 'Benchmarks', titleSource: 'engine', sessionFile: 's1' }), tab('t1', { kind: 'task', sessionFile: 's1' }), tab('t2', { kind: 'task', sessionFile: 's1' }), tab('x', { sessionFile: 's2' })];
  assert.deepEqual(findOffers(tabs), [{ key: 'conversation:s1', basis: 'conversation', ids: ['c', 't1', 't2'], title: 'Benchmarks' }]);
});

test('a placeholder conversation title is never offered as the group name', () => {
  const tabs = [tab('c', { title: 'New conversation', sessionFile: 's' }), tab('a', { kind: 'task', sessionFile: 's' }), tab('b', { kind: 'task', sessionFile: 's' })];
  assert.equal(findOffers(tabs)[0].title, undefined);
  assert.equal(findOffers([tab('c', { title: 'Mine', sessionFile: 's' }), ...tabs.slice(1)])[0].title, undefined, 'no titleSource: the title is not a name anyone chose');
});

test('fewer than three, pinned, grouped, split and unsourced tabs make no offer', () => {
  const base = [tab('a', { sessionFile: 's' }), tab('b', { kind: 'task', sessionFile: 's' })];
  assert.deepEqual(findOffers([...base, tab('c', { kind: 'task', sessionFile: 's', pinned: true })]), []);
  assert.deepEqual(findOffers([...base, tab('c', { kind: 'task', sessionFile: 's', groupId: 'g' })]), []);
  assert.deepEqual(findOffers([...base, tab('c', { kind: 'task', sessionFile: 's', split: { layout: '1x2', focus: 0, panes: [] } })]), []);
  assert.deepEqual(findOffers([...base, tab('c', { kind: 'terminal', sessionFile: 's' })]), []);
  assert.deepEqual(findOffers([...base, tab('c', { kind: 'newtab' })]), []);
});

test('file and diff tabs in one top folder are an offer named by that folder; root, absolute and climbing paths name none', () => {
  const file = (id: string, path: string, kind: Tab['kind'] = 'file') => tab(id, { kind, file: { path } });
  const offers = findOffers([file('a', 'internal/x/a.go'), file('b', 'internal/b.go', 'diff'), file('c', 'internal/y/c.go'), file('d', 'cmd/d.go')]);
  assert.deepEqual(offers, [{ key: 'folder:internal', basis: 'folder', ids: ['a', 'b', 'c'], title: 'internal' }]);
  for (const bad of ['a.go', '/etc/passwd', '../x/a.go', 'a//b.go', 'a\\b.go', './a/b.go', undefined]) assert.equal(folderOf(bad), undefined, String(bad));
});

test('a tab is claimed by the session offer first, so folder offers need three of their own', () => {
  const f = (id: string, session?: string) => tab(id, { kind: 'file', file: { path: `internal/${id}.go` }, sessionFile: session });
  const offers = findOffers([f('a', 's'), f('b', 's'), f('c', 's'), f('d'), f('e')]);
  assert.deepEqual(offers.map(o => o.key), ['conversation:s']);
});

test('memory: decisions last thirty days, malformed storage is nothing, and the set is capped', () => {
  const store = new Map<string, string>();
  const storage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) };
  const now = 100 * day;
  writeMemory(storage, { fresh: now - 1, stale: now - offerMemoryDays * day, future: now + day });
  assert.deepEqual(Object.keys(readMemory(storage, now)), ['fresh']);
  store.set(offerStorageKey, '[1,2]'); assert.deepEqual(readMemory(storage, now), {});
  store.set(offerStorageKey, '{bad'); assert.deepEqual(readMemory(storage, now), {});
  store.set(offerStorageKey, '{"a":"x","b":null}'); assert.deepEqual(readMemory(storage, now), {});
  assert.deepEqual(readMemory(undefined, now), {});
  let m = {}; for (let i = 0; i < 250; i++) m = remember(m, `k${i}`, i + 1);
  assert.equal(Object.keys(m).length, 200);
  assert.ok('k249' in m && !('k0' in m));
});

test('nextOffer skips decided sets and sets already shown this launch', () => {
  const offers = [{ key: 'a', basis: 'folder' as const, ids: [] }, { key: 'b', basis: 'folder' as const, ids: [] }, { key: 'c', basis: 'folder' as const, ids: [] }];
  assert.equal(nextOffer(offers, { a: 1 }, new Set(['b']))?.key, 'c');
  assert.equal(nextOffer(offers, { a: 1, c: 2 }, new Set(['b'])), undefined);
});

test('canonical chat ids come from saved records, never opaque bridge tokens or titles', () => {
  const id = 'aaaa000000000001';
  const path = tab('path', { title: 'Drip irrigation', sessionFile: '/tmp/-bucket/aaaa000000000001/transcript.jsonl' });
  const fromSummary = tab('sum', { title: 'Ignore this title' });
  const fromTarget = tab('tgt', { target: { sessionId: 'bbbb000000000002' } });
  const pinned = tab('pin', { pinned: true, target: { sessionId: 'cccc000000000003' } });
  const bad = tab('bad', { target: { sessionId: 'not-a-session-id' } });
  assert.equal(chatIDOf(path), id);
  assert.equal(chatIDOf(fromTarget), undefined, 'even a hex bridge token is not a history record');
  assert.equal(chatIDOf(path, { chatId: 'dddd000000000004' }), id, 'saved pane identity beats a stale host summary');
  assert.deepEqual(looseChatIDs([path, fromSummary, fromTarget, pinned, bad], { sum: { chatId: id } }), [id]);
  assert.equal(shouldAskCanonical([id, 'bbbb000000000002'], new Set()), false);
  const three = [id, 'bbbb000000000002', 'cccc000000000003'];
  const key = canonicalSetKey(three);
  assert.equal(canonicalSetKey([...three].reverse()), key);
  assert.equal(shouldAskCanonical(three, new Set()), true);
  assert.equal(shouldAskCanonical(three, new Set([key])), false);
});

test('an engine offer maps onto loose conversation tabs and does not take a tab a saved session already claimed', () => {
  const summaries = { a: { chatId: 'aaaa000000000001' }, b: { chatId: 'bbbb000000000002' }, c: { chatId: 'cccc000000000003' } };
  const tabs = [tab('a'), tab('b'), tab('c'), tab('t1', { kind: 'task', sessionFile: 's' }), tab('t2', { kind: 'task', sessionFile: 's' }), tab('t3', { kind: 'task', sessionFile: 's' })];
  const engine = [{ key: 'topic:drip', basis: 'topic' as const, ids: ['aaaa000000000001', 'bbbb000000000002', 'cccc000000000003', '9999999999999999'], title: 'Drip irrigation' }];
  const mapped = tabOffersForChats(tabs, summaries, engine);
  assert.deepEqual(mapped, [{ key: 'topic:drip', basis: 'topic', ids: ['a', 'b', 'c'], title: 'Drip irrigation' }]);
  const local = findOffers(tabs);
  assert.equal(local[0].key, 'conversation:s');
  assert.deepEqual(mergeOffers(local, mapped).map(offer => offer.key), ['conversation:s', 'topic:drip']);
  const claimed = [tab('a', { sessionFile: 's' }), tab('b', { kind: 'task', sessionFile: 's' }), tab('c', { kind: 'task', sessionFile: 's' })];
  assert.deepEqual(mergeOffers(findOffers(claimed), tabOffersForChats(claimed, summaries, engine)), findOffers(claimed));
});

test('accepting hands ids to the reducer\'s own group action: one group, one run, named, nothing else moved', () => {
  const tabs = [tab('x'), tab('c', { title: 'Benchmarks', titleSource: 'engine', sessionFile: 's' }), tab('y'), tab('t1', { kind: 'task', sessionFile: 's' }), tab('t2', { kind: 'task', sessionFile: 's' })];
  const s: WorkspaceState = { tabs, groups: [], activeId: 'x', closed: [], nextNumber: 6, recentIds: [] };
  const offer = findOffers(s.tabs)[0];
  const after = workspaceReducer(s, { type: 'group', id: offer.ids[0], ids: offer.ids.slice(1), title: offer.title });
  assert.equal(after.groups.length, 1);
  assert.equal(after.groups[0].title, 'Benchmarks');
  assert.deepEqual(after.tabs.map(t => t.id), ['x', 'c', 't1', 't2', 'y']);
  assert.deepEqual(findOffers(after.tabs), [], 'grouped tabs are never offered again');
});
