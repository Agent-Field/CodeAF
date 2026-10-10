import test from 'node:test';
import assert from 'node:assert/strict';
import {
  CLOSED_STILL_RUNNING, OPEN_IDLE_MS, PLACE_NUMBER_MAX, breadcrumb, countsText, effectiveTint, leastUsedTint,
  placeNumbers, primaryParentLabel, railSections, rollupWords,
  type ChatActivity, type PlaceRecord, type PlaceStore, type WindowVisit,
} from './selectors.ts';

const now = '2026-10-10T12:00:00.000Z';
const ago = (ms: number) => new Date(Date.parse(now) - ms).toISOString();

function place(id: string, name: string, parents: readonly string[] = [], tint: PlaceRecord['tint'] = '', archived = false): PlaceRecord {
  return { id, name, parents, tint, archived };
}

function store(places: readonly PlaceRecord[], memberships: PlaceStore['memberships'] = [], pinned: readonly string[] = []): PlaceStore {
  return { places, memberships, pinned };
}

function chats(n: number, placeId: string): PlaceStore['memberships'] {
  return Array.from({ length: n }, (_, i) => ({ chatId: `${placeId}-c${i}`, placeId }));
}

test('effective tint: a top-level choice, inheritance, an override, and graphite when nothing was chosen', () => {
  const places = [
    place('top', 'codeaf', [], 'tide'),
    place('child', 'Software', ['top']),
    place('grand', 'Config parser', ['child']),
    place('bare', 'Notes'),
  ];
  const graph = store(places);
  assert.equal(effectiveTint('top', graph), 'tide');
  assert.equal(effectiveTint('child', graph), 'tide');
  assert.equal(effectiveTint('grand', graph), 'tide');
  assert.equal(effectiveTint('bare', graph), 'graphite');
  assert.equal(effectiveTint('missing', graph), undefined);

  places[1] = place('child', 'Software', ['top'], 'sage');
  assert.equal(effectiveTint('child', store(places)), 'sage');
  assert.equal(effectiveTint('grand', store(places)), 'sage');
  places[1] = place('child', 'Software', ['top']);
  assert.equal(effectiveTint('grand', store(places)), 'tide');
});

test('several parents keep the first parent’s tint and never blend', () => {
  const tide = place('tide', 'codeaf', [], 'tide');
  const rose = place('rose', 'Marketing', [], 'rose');
  const child = place('release', 'Release', ['tide', 'rose']);
  const grand = place('notes', 'Notes', ['release']);
  assert.equal(effectiveTint('release', store([tide, rose, child, grand])), 'tide');
  assert.equal(effectiveTint('notes', store([tide, rose, child, grand])), 'tide');

  const flipped = place('release', 'Release', ['rose', 'tide']);
  const under = place('notes', 'Notes', ['release']);
  assert.equal(effectiveTint('release', store([tide, rose, flipped, under])), 'rose');
  assert.equal(effectiveTint('notes', store([tide, rose, flipped, under])), 'rose');
  assert.equal(effectiveTint('release', store([tide, rose, place('release', 'Release', ['rose', 'tide'], 'sage'), under])), 'sage');

  const loop = store([place('a', 'A', ['b']), place('b', 'B', ['a'])]);
  assert.equal(effectiveTint('a', loop), 'graphite');
  assert.equal(effectiveTint('b', store([place('a', 'A', ['b']), place('b', 'B', ['a'], 'rose')])), 'rose');
});

test('a new top-level place gets the least-used hue, children and graphite do not spend one', () => {
  assert.equal(leastUsedTint(store([])), 'tide');
  const tops = ['tide', 'iris', 'rose', 'sand', 'sage'].map(tint => place(tint, tint, [], tint as PlaceRecord['tint']));
  assert.equal(leastUsedTint(store(tops)), 'tide');
  assert.equal(leastUsedTint(store([...tops, place('again', 'Again', [], 'tide')])), 'iris');
  assert.equal(leastUsedTint(store([place('gone', 'Gone', [], 'tide', true), place('kid', 'Kid', ['gone'], 'rose')])), 'tide');
  assert.equal(leastUsedTint(store([place('bare', 'Bare')])), 'tide');
});

test('the primary parent is the first parent’s name, and the breadcrumb walks that chain outermost first', () => {
  const graph = store([
    place('codeaf', 'codeaf', [], 'tide'),
    place('software', 'Software', ['codeaf']),
    place('config', 'Config parser', ['software', 'docs']),
    place('docs', 'Docs', [], 'sand'),
  ]);
  assert.equal(primaryParentLabel('config', graph), 'Software');
  assert.equal(primaryParentLabel('codeaf', graph), undefined);
  assert.equal(primaryParentLabel('missing', graph), undefined);
  assert.deepEqual(breadcrumb('config', graph).map(crumb => crumb.name), ['codeaf', 'Software']);
  assert.deepEqual(breadcrumb('codeaf', graph), []);
  assert.deepEqual(breadcrumb('missing', graph), []);
});

test('counts say 4 inside, 28 chats, 6 chats · also in Software, and 47 places · 212 chats', () => {
  const reports = place('reports', 'Reports', [], 'sage');
  const children = Array.from({ length: 47 }, (_, i) => place(`r${i}`, `Report ${i}`, ['reports']));
  const reportChats = chats(212, 'reports')!;
  const reportsStore = store([reports, ...children], reportChats);
  assert.equal(countsText('reports', reportsStore, 'tree'), '47 inside');
  assert.equal(countsText('reports', reportsStore, 'tile'), '47 places · 212 chats');

  const software = place('software', 'Software', [], 'tide');
  const release = place('release', 'Release', ['marketing', 'software']);
  const releaseStore = store([software, place('marketing', 'Marketing', [], 'rose'), release], chats(6, 'release'));
  assert.equal(countsText('release', releaseStore, 'tile'), '6 chats · also in Software');
  assert.equal(countsText('release', releaseStore, 'tree'), '6 chats');

  const leaf = store([place('leaf', 'Leaf')], chats(28, 'leaf'));
  assert.equal(countsText('leaf', leaf, 'tree'), '28 chats');
  assert.equal(countsText('leaf', leaf, 'tile'), '28 chats');
  assert.equal(countsText('leaf', store([place('leaf', 'Leaf')]), 'tile'), undefined);
  assert.equal(countsText('leaf', store([place('leaf', 'Leaf')]), 'tree'), undefined);

  const top = place('top', 'Top');
  const a = place('a', 'A', ['top']);
  const b = place('b', 'B', ['top']);
  const shared = store([top, a, b], [{ chatId: 'shared', placeId: 'a' }, { chatId: 'shared', placeId: 'b' }]);
  assert.equal(countsText('top', shared, 'tile'), '2 places · 1 chat');
  assert.equal(countsText('a', shared, 'tile'), '1 chat');
});

test('a dot says 2 need you in the one child it comes from, and failed when nothing is waiting', () => {
  const software = place('software', 'Software', [], 'iris');
  const config = place('config', 'Config parser', ['software']);
  const docs = place('docs', 'Docs', ['software']);
  const graph = store([software, config, docs], [
    { chatId: 'port', placeId: 'config' },
    { chatId: 'other', placeId: 'docs' },
    { chatId: 'own', placeId: 'software' },
  ]);
  const waiting: Record<string, ChatActivity> = { port: { needsYou: 2, running: true } };
  assert.deepEqual(rollupWords('config', graph, waiting), { status: 'waiting', words: '2 need you in Config parser' });
  assert.deepEqual(rollupWords('software', graph, waiting), { status: 'waiting', words: '2 need you in Config parser' });
  assert.equal(rollupWords('docs', graph, { other: { running: true } }), undefined);

  const both: Record<string, ChatActivity> = { port: { needsYou: 1 }, other: { needsYou: 1 } };
  assert.deepEqual(rollupWords('software', graph, both), { status: 'waiting', words: '2 need you in Software' });

  const shared = store([software, config, docs], [{ chatId: 'port', placeId: 'config' }, { chatId: 'port', placeId: 'docs' }]);
  assert.deepEqual(rollupWords('software', shared, { port: { needsYou: 2 } }), { status: 'waiting', words: '2 need you in Software' });

  assert.deepEqual(rollupWords('config', graph, { port: { failed: true, needsYou: 1 } }), { status: 'waiting', words: '1 needs you in Config parser' });
  assert.deepEqual(rollupWords('config', graph, { port: { failed: true } }), { status: 'failed', words: '1 failed task in Config parser' });
  assert.deepEqual(rollupWords('software', graph, { port: { failed: true }, other: { failed: true } }), { status: 'failed', words: '2 failed tasks in Software' });
  assert.equal(rollupWords('missing', graph, waiting), undefined);
});

test('the rail keeps pinned order, Open is this window’s newest first, and a closed place stays only while it is busy', () => {
  const codeaf = place('codeaf', 'codeaf', [], 'tide');
  const personal = place('personal', 'Personal', [], 'sand');
  const config = place('config', 'Config parser', ['codeaf']);
  const marketing = place('marketing', 'Marketing', ['codeaf'], 'rose');
  const reading = place('reading', 'Reading', [], 'iris');
  const q3 = place('q3', 'Q3 report', ['reports'], 'sage');
  const reports = place('reports', 'Reports', [], 'sage');
  const graph = store(
    [codeaf, personal, config, marketing, reading, reports, q3],
    [{ chatId: 'port', placeId: 'config' }, { chatId: 'draft', placeId: 'q3' }],
    ['codeaf', 'personal', 'codeaf'],
  );
  const activity: Record<string, ChatActivity> = { port: { needsYou: 2, running: true }, draft: { running: true } };
  const open = railSections(graph, {
    now,
    visits: [
      { placeId: 'q3', touchedAt: ago(3 * 60_000), closed: true },
      { placeId: 'marketing', touchedAt: ago(2 * 60_000) },
      { placeId: 'config', touchedAt: ago(60_000) },
      { placeId: 'reading', touchedAt: ago(0) },
      { placeId: 'codeaf', touchedAt: ago(0) },
    ],
  }, activity);

  assert.deepEqual(open.pinned.map(row => [row.name, row.number, row.tint]), [['codeaf', 1, 'tide'], ['Personal', 2, 'sand']]);
  assert.deepEqual(open.open.map(row => row.name), ['Reading', 'Config parser', 'Marketing', 'Q3 report']);
  assert.equal(open.open.find(row => row.id === 'config')?.parentName, 'codeaf');
  assert.equal(open.open.find(row => row.id === 'config')?.statusLabel, '2 need you in Config parser');
  assert.equal(open.open.find(row => row.id === 'q3')?.closedButRunning, true);
  assert.equal(open.open.find(row => row.id === 'q3')?.closedLabel, CLOSED_STILL_RUNNING);
  assert.deepEqual(open.open.map(row => row.number), [3, 4, 5, 6]);

  const settled = railSections(graph, { now, visits: [{ placeId: 'q3', touchedAt: ago(0), closed: true }] }, { draft: { failed: true } });
  assert.deepEqual(settled.open, []);

  const idle = railSections(graph, { now, visits: [{ placeId: 'marketing', touchedAt: ago(OPEN_IDLE_MS) }] });
  assert.deepEqual(idle.open, []);
  const kept = railSections(graph, { now, visits: [{ placeId: 'config', touchedAt: ago(OPEN_IDLE_MS), closed: true }] }, activity);
  assert.equal(kept.open[0]?.closedButRunning, true);
  const pinnedStays = railSections(graph, { now, visits: [{ placeId: 'codeaf', touchedAt: ago(OPEN_IDLE_MS * 2) }] });
  assert.deepEqual(pinnedStays.pinned.map(row => row.name), ['codeaf', 'Personal']);
});

test('place numbers run through Pinned and then Open, and stop at 9', () => {
  const pinned = ['p1', 'p2'];
  const open = Array.from({ length: 12 }, (_, i) => `o${i}`);
  const numbers = placeNumbers(pinned, open);
  assert.equal(numbers.get('p1'), 1);
  assert.equal(numbers.get('p2'), 2);
  assert.equal(numbers.get('o0'), 3);
  assert.equal(numbers.get('o6'), 9);
  assert.equal(numbers.has('o7'), false);
  assert.equal(numbers.size, PLACE_NUMBER_MAX);
  assert.equal(placeNumbers([], []).size, 0);
});

test('a 200-place chain inherits tint, counts chats once, and numbers only the first nine open rows', () => {
  const places: PlaceRecord[] = [];
  const memberships: { chatId: string; placeId: string }[] = [];
  const activity: Record<string, ChatActivity> = {};
  let previous = '';
  for (let i = 0; i < 200; i++) {
    const id = `pl_${i}`;
    places.push(place(id, `P${i}`, previous ? [previous] : [], i === 0 ? 'tide' : ''));
    memberships.push({ chatId: `c${i}`, placeId: id });
    activity[`c${i}`] = { needsYou: 1 };
    previous = id;
  }
  const graph = store(places, memberships);
  assert.equal(places.length, 200);
  assert.equal(effectiveTint('pl_0', graph), 'tide');
  assert.equal(effectiveTint('pl_100', graph), 'tide');
  assert.equal(effectiveTint('pl_199', graph), 'tide');
  assert.equal(breadcrumb('pl_199', graph).length, 199);
  assert.equal(breadcrumb('pl_199', graph)[0]?.name, 'P0');
  assert.equal(countsText('pl_0', graph, 'tree'), '1 inside');
  assert.equal(countsText('pl_0', graph, 'tile'), '1 place · 200 chats');
  assert.equal(countsText('pl_199', graph, 'tile'), '1 chat');
  assert.equal(rollupWords('pl_0', graph, activity)?.words, '200 need you in P0');
  assert.equal(rollupWords('pl_199', graph, activity)?.words, '1 needs you in P199');

  const visits: WindowVisit[] = places.map((item, i) => ({ placeId: item.id, touchedAt: ago((200 - i) * 1000) }));
  const rail = railSections(graph, { now, visits }, activity);
  assert.equal(rail.pinned.length, 0);
  assert.equal(rail.open.length, 200);
  assert.equal(rail.open[0]?.id, 'pl_199');
  assert.equal(rail.open[0]?.number, 1);
  assert.equal(rail.open[8]?.number, 9);
  assert.equal(rail.open[9]?.number, undefined);
  assert.equal(leastUsedTint(graph), 'iris');
});

test('an empty store draws nothing', () => {
  const empty = store([]);
  assert.equal(effectiveTint('pl', empty), undefined);
  assert.equal(primaryParentLabel('pl', empty), undefined);
  assert.deepEqual(breadcrumb('pl', empty), []);
  assert.equal(countsText('pl', empty, 'tree'), undefined);
  assert.equal(countsText('pl', empty, 'tile'), undefined);
  assert.equal(rollupWords('pl', empty, { c: { needsYou: 2, failed: true, running: true } }), undefined);
  assert.deepEqual(railSections(empty, { now, visits: [{ placeId: 'pl', touchedAt: now }] }), { pinned: [], open: [] });
  assert.equal(placeNumbers([], []).size, 0);
});
