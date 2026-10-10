import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { homeViewFromDigest, indexPlaces, placeMeta, railOrder, railSections, rollupStatus, sessionFileOf, statusWords } from './selectors.ts';

// The Go handlers write these fixtures (TestPlacesWireFixtures): the selectors read what the engine really sends.
const fixture = (name: string): any => JSON.parse(readFileSync(new URL(`../fixtures/${name}.json`, import.meta.url), 'utf8'));

test('a dot is amber for needs-you, red for a failed task, and nothing for running', () => {
  assert.equal(rollupStatus({ chats: 1, running: 1, needsYou: 0, incomplete: 0, failedTasks: 0 }), undefined);
  assert.equal(rollupStatus({ chats: 1, running: 1, needsYou: 1, incomplete: 0, failedTasks: 2 }), 'waiting');
  assert.equal(rollupStatus({ chats: 1, running: 0, needsYou: 0, incomplete: 0, failedTasks: 2 }), 'failed');
});

test('a parent’s dot names the one child it comes from', () => {
  const graph = fixture('graph');
  const index = indexPlaces(graph.places);
  assert.equal(statusWords(index.byId.get('pl_0000000000000001')!, index), '1 needs you in Config parser');
  assert.equal(statusWords(index.byId.get('pl_0000000000000005')!, index), '1 needs you in Config parser');
  assert.equal(statusWords(index.byId.get('pl_0000000000000003')!, index), undefined);
});

test('a place with children says how many are inside, otherwise its chats, and nothing at zero', () => {
  const [software, , config] = fixture('graph').places;
  assert.equal(placeMeta(software), '2 inside');
  assert.equal(placeMeta(config), '2 chats');
  assert.equal(placeMeta({ ...config, counts: { children: 0, descendants: 0, chats: 0, chatsInclusive: 0 } }), undefined);
});

test('the rail: Pinned in the engine’s order, Open without closed places unless they are still busy, and the window’s place always there', () => {
  const graph = fixture('graph');
  const open = railSections(graph, new Map());
  assert.deepEqual(open.pinned.map(p => p.name), ['Software']);
  assert.deepEqual(open.open.map(p => [p.name, p.parentName]), [['Config parser', 'Software']]);
  // Closed before its last visit: it is open again.
  assert.deepEqual(railSections(graph, new Map([['pl_0000000000000005', '2026-10-09T11:00:00Z']])).open.map(p => p.closedButBusy), [undefined]);
  // Closed after its last visit while still running: it stays, muted.
  assert.deepEqual(railSections(graph, new Map([['pl_0000000000000005', '2026-10-09T13:00:00Z']])).open.map(p => p.closedButBusy), [true]);
  const quiet = { ...graph, places: graph.places.map((p: any) => ({ ...p, statusInclusive: { ...p.statusInclusive, running: 0, needsYou: 0 } })) };
  quiet.rail = { ...graph.rail, open: graph.rail.open.map((p: any) => ({ ...p, statusInclusive: { ...p.statusInclusive, running: 0, needsYou: 0 } })) };
  assert.deepEqual(railSections(quiet, new Map([['pl_0000000000000005', '2026-10-09T13:00:00Z']])).open, []);
  // Going to a place puts it in Open even before the engine's next read.
  assert.deepEqual(railSections(graph, new Map(), 'pl_0000000000000003').open.map(p => p.name), ['Docs', 'Config parser']);
  assert.deepEqual(railOrder(open), ['pl_0000000000000001', 'pl_0000000000000005']);
});

test('the Since block carries the engine words, and blank words draw nothing (PL-133)', () => {
  const digest = fixture('home-place');
  const recap = { label: 'Since yesterday', text: ' Two fixes landed. ', since: '2026-10-09T13:00:00Z', chats: 1, items: [], unsummarised: 0 };
  assert.deepEqual(homeViewFromDigest({ ...digest, recap }).recap, { label: 'Since yesterday', text: 'Two fixes landed.' });
  assert.equal(homeViewFromDigest({ ...digest, recap: { ...recap, text: '  ' } }).recap, undefined);
  assert.equal(homeViewFromDigest({ ...digest, recap: undefined }).recap, undefined);
});

test('a place Home view carries only what the engine sent', () => {
  const view = homeViewFromDigest(fixture('home-place'));
  assert.equal(view.kind, 'place');
  assert.equal(view.title, 'Config parser');
  assert.equal(view.recap, undefined);
  assert.equal(view.contextLine, undefined);
  assert.deepEqual(view.attention.map(a => [a.title, a.status, a.detail, a.placeName]), [['Port fix to v1', 'waiting', 'Allow the v1 branch push?', undefined], ['Update fixtures', 'running', undefined, undefined]]);
  assert.ok(view.chats.every(chat => chat.status === undefined || chat.status === 'running' || chat.status === 'waiting'));
});

test('All places lists the chats in no place from Now’s digest, with the graph’s totals and paths', () => {
  const graph = fixture('graph');
  const view = homeViewFromDigest(fixture('home-root'), graph, fixture('home-now'));
  assert.equal(view.kind, 'root');
  assert.deepEqual(view.chats.map(chat => chat.id), fixture('home-now').chats.map((chat: any) => chat.id));
  assert.deepEqual(view.totals, { topLevel: 2, all: 4 });
  assert.deepEqual(view.allPlaces?.find(place => place.name === 'Config parser')?.path, ['Software']);
  assert.equal(homeViewFromDigest(fixture('home-root'), graph).chats.length, 0);
});

test('a chat row opens through the session file the engine reported, and only that', () => {
  const digest = fixture('home-place');
  digest.chats[0].sessionFile = '/h/.codeaf/v3/projects/app/sess-run/session.jsonl';
  assert.equal(sessionFileOf([undefined, digest], 'sess-run'), '/h/.codeaf/v3/projects/app/sess-run/session.jsonl');
  assert.equal(sessionFileOf([digest], 'sess-need'), undefined);
});

test('Home chat digests use the matching engine recap, with waiting reasons first and missing evidence absent', () => {
  const digest = fixture('home-place');
  digest.recap = { items: [
    { chatId: 'sess-run', line: 'Updated the strict-mode fixtures.' },
    { chatId: 'sess-need', line: 'Ported the parser.' },
    { chatId: 'other-chat', line: 'An unrelated decision.' },
  ] };
  const view = homeViewFromDigest(digest);
  assert.equal(view.chats.find(chat => chat.id === 'sess-run')?.excerpt, 'Updated the strict-mode fixtures.');
  assert.equal(view.chats.find(chat => chat.id === 'sess-need')?.excerpt, 'Allow the v1 branch push?');
  assert.equal(homeViewFromDigest({ ...digest, recap: undefined }).chats.find(chat => chat.id === 'sess-run')?.excerpt, undefined);
  const now = fixture('home-now');
  now.recap = { items: [{ chatId: now.chats[0].id, line: 'The loose chat’s recap.' }] };
  assert.equal(homeViewFromDigest(fixture('home-root'), undefined, now).chats[0].excerpt, 'The loose chat’s recap.');
});
