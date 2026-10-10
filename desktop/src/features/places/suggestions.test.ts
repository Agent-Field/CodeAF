import assert from 'node:assert/strict';
import test from 'node:test';
import {
  browserSnoozes, clusterMinimum, idleAfterDays, idleLine, idleListCap, idlePlaces, idleText, readSnoozes, snoozeDays,
  suppliedSuggestions, viewForSupplied, withSnooze, writeSnooze, type IdlePlace, type SnoozeStore,
} from './suggestions.ts';

const day = 86_400_000;
const now = new Date('2026-10-10T12:00:00.000Z');
const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();
const place = (id: string, name: string, msAgo: number, extra: Partial<IdlePlace> = {}): IdlePlace => ({ id, name, createdAt: ago(msAgo), ...extra });

function memory(): SnoozeStore & { raw: Map<string, string> } {
  const raw = new Map<string, string>();
  return { raw, getItem: key => raw.get(key) ?? null, setItem: (key, value) => { raw.set(key, value); } };
}

test('the idle figures are the design’s 60 and 30, and a new place needs 5 chats', () => {
  assert.equal(idleAfterDays, 60);
  assert.equal(snoozeDays, 30);
  assert.equal(clusterMinimum, 5);
  assert.equal(idleListCap, 20);
});

test('exactly 60 days is due and one millisecond earlier is not', () => {
  const due = idlePlaces([place('pl_1', 'Launch week', 60 * day)], now);
  assert.equal(due.length, 1);
  assert.equal(due[0].days, 60);
  assert.equal(idleText(due[0]), '“Launch week” hasn’t been touched in 60 days');
  assert.deepEqual(idlePlaces([place('pl_1', 'Launch week', 60 * day - 1)], now), []);
});

test('the clock is the one passed in, not the wall clock', () => {
  const early = new Date('2026-01-01T00:00:00.000Z');
  const later = new Date(early.getTime() + 60 * day);
  const old = { id: 'pl_old', name: 'Old notes', createdAt: early.toISOString() };
  assert.equal(idlePlaces([old], new Date(later.getTime() - 1)).length, 0);
  assert.equal(idlePlaces([old], later)[0].days, 60);
  assert.equal(idlePlaces([old], new Date('invalid')).length, 0);
});

test('Not now hides a place for 30 days and returns it on that boundary', () => {
  const idle = [place('pl_1', 'Launch week', 75 * day)];
  const snoozed = withSnooze([], 'pl_1', now);
  assert.equal(snoozed[0].until - snoozed[0].at, 30 * day);
  assert.equal(idlePlaces(idle, now, snoozed).length, 0);
  assert.equal(idlePlaces(idle, new Date(now.getTime() + 30 * day - 1), snoozed).length, 0);
  const back = idlePlaces(idle, new Date(now.getTime() + 30 * day), snoozed);
  assert.equal(back.length, 1);
  assert.equal(back[0].days, 105);
});

test('a saved Not now lasts 30 days on the injected clock and a damaged record hides nothing', () => {
  const store = memory();
  writeSnooze(store, 'pl_1', now);
  const idle = [place('pl_1', 'Launch week', 80 * day)];
  assert.equal(idlePlaces(idle, now, readSnoozes(store, now)).length, 0);
  const before = new Date(now.getTime() + 30 * day - 1);
  assert.equal(idlePlaces(idle, before, readSnoozes(store, before)).length, 0);
  const at = new Date(now.getTime() + 30 * day);
  assert.equal(idlePlaces(idle, at, readSnoozes(store, at)).length, 1);
  store.setItem([...store.raw.keys()][0], '{');
  assert.deepEqual(readSnoozes(store, now), []);
  store.setItem([...store.raw.keys()][0], JSON.stringify({ version: 2, snoozes: [] }));
  assert.deepEqual(readSnoozes(store, now), []);
  store.setItem([...store.raw.keys()][0], JSON.stringify({ version: 1, snoozes: [{ id: 'a', at: 1, until: 2 }, { id: 'a', at: 1, until: 2 }] }));
  assert.deepEqual(readSnoozes(store, now), []);
});

test('pinned, archived, busy and unknown ages are never offered', () => {
  const old = 90 * day;
  assert.equal(idlePlaces([place('p', 'Pinned', old, { pinned: true })], now).length, 0);
  assert.equal(idlePlaces([place('p', 'Archived', old, { archived: true })], now).length, 0);
  assert.equal(idlePlaces([place('p', 'Busy', old, { busy: true })], now).length, 0);
  assert.equal(idlePlaces([{ id: 'p', name: 'Undated' }], now).length, 0);
  assert.equal(idlePlaces([place('p', 'Zero', 0, { createdAt: '0001-01-01T00:00:00Z' })], now).length, 0);
  assert.equal(idlePlaces([{ id: 'p', name: '   ', createdAt: ago(old) }], now).length, 0);
});

test('a live descendant keeps the parent current, and an archived child does not', () => {
  const parent = place('parent', 'Reports', 100 * day);
  const fresh = place('child', 'Q3', 10 * day, { parents: ['parent'] });
  assert.equal(idlePlaces([parent, fresh], now).some(item => item.id === 'parent'), false);
  const exactly = place('child', 'Q3', 60 * day, { parents: ['parent'] });
  assert.equal(idlePlaces([parent, exactly], now).find(item => item.id === 'parent')?.days, 60);
  const almost = place('child', 'Q3', 60 * day - 1, { parents: ['parent'] });
  assert.equal(idlePlaces([parent, almost], now).some(item => item.id === 'parent'), false);
  const archivedChild = place('child', 'Q3', 1 * day, { parents: ['parent'], archived: true });
  assert.equal(idlePlaces([parent, archivedChild], now).find(item => item.id === 'parent')?.days, 100);
});

test('one day is singular, the oldest place is first, and the list stops at 20', () => {
  assert.equal(idleText({ name: 'A', days: 1 }), '“A” hasn’t been touched in 1 day');
  const older = place('b', 'Zed', 80 * day);
  const newer = place('a', 'Alpha', 70 * day);
  assert.deepEqual(idlePlaces([newer, older], now).map(item => item.id), ['b', 'a']);
  const tied = [place('b', 'Alpha', 70 * day), place('a', 'Alpha', 70 * day)];
  assert.deepEqual(idlePlaces(tied, now).map(item => item.id), ['a', 'b']);
  const many = Array.from({ length: 21 }, (_, index) => place(`p${String(index).padStart(2, '0')}`, `Place ${String(index).padStart(2, '0')}`, 100 * day));
  const listed = idlePlaces(many, now);
  assert.equal(listed.length, 20);
  assert.equal(listed.at(-1)?.id, 'p19');
});

test('an engine candidate is confirmed by this clock, including a local Not now', () => {
  const candidate = { id: 'pl_1', name: 'Launch week', touchedAt: ago(75 * day) };
  assert.equal(idleLine(candidate, now)?.text, '“Launch week” hasn’t been touched in 75 days');
  assert.equal(idleLine({ ...candidate, touchedAt: ago(60 * day - 1) }, now), undefined);
  assert.equal(idleLine(candidate, now, withSnooze([], 'pl_1', now)), undefined);
  assert.equal(idleLine({ id: 'pl_1', name: 'Launch week', touchedAt: 'not-a-time' }, now), undefined);
});

test('engine proposals are rendered as sent, and a short or unnamed create is absent', () => {
  assert.deepEqual(suppliedSuggestions([], now), []);
  const card = viewForSupplied({ id: 'prop_1', kind: 'create', name: 'Launch week', reason: '7 chats across Marketing and Release', chatCount: 7 }, now);
  assert.equal(card?.layout, 'card');
  if (card?.layout !== 'card') return;
  assert.equal(card.title, 'Launch week');
  assert.equal(card.detail, '7 chats across Marketing and Release');
  assert.deepEqual(card.actions.map(action => action.label), ['Create', 'Not now']);
  assert.equal(viewForSupplied({ id: 'prop_2', kind: 'create', name: 'Tiny', chatCount: 4 }, now), undefined);
  assert.equal(viewForSupplied({ id: 'prop_3', kind: 'create', name: '   ', chatCount: 7 }, now), undefined);
  const counted = viewForSupplied({ id: 'prop_4', kind: 'create', name: 'Launch week', chatCount: 7 }, now);
  assert.equal(counted?.layout === 'card' && counted.detail, '7 chats');
  const line = viewForSupplied({ id: 'prop_5', kind: 'move', placeName: 'Reading', chatCount: 5 }, now);
  assert.equal(line?.layout === 'line' && line.text, '5 of these look like they belong in Reading');
  assert.equal(viewForSupplied({ id: 'prop_1', kind: 'create', name: 'Launch week', chatCount: 7 }, now, withSnooze([], 'prop_1', now)), undefined);
  assert.equal(browserSnoozes(new Date('invalid')).length, 0);
});
