import test from 'node:test';
import assert from 'node:assert/strict';
import { decidedBy, fullStamp, groupLabel, highlight, historyIdOf, idleTabs, indexOfRow, withWorldState, layout, metaLine, queryTerms, resultStamp, rowHeight, rowLead, rowLine, rowStamp, rowTrail, stepRow, stickyGroup, windowOf, type Metrics } from './model.ts';
import type { HistoryItem } from './types.ts';

// Friday 9 October 2026, 15:00 local time.
const now = new Date(2026, 9, 9, 15, 0).getTime();
const at = (month: number, day: number, hour = 12, minute = 0) => new Date(2026, month, day, hour, minute).toISOString();
const metrics: Metrics = { group: 33, row: 61, rowFiles: 87, overscan: 2 };
const item = (id: string, over: Partial<HistoryItem> = {}): HistoryItem => ({ id, sessionFile: `/s/${id}/transcript.jsonl`, title: id, at: at(9, 9), messages: 4, tasks: 0, tasksRunning: 0, files: [], fileCount: 0, decisions: 0, state: 'idle', open: false, archived: false, ...over });

test('groupLabel: Today, Yesterday, a weekday of this week, Last week, then months', () => {
  assert.equal(groupLabel(Date.parse(at(9, 9, 8)), now), 'Today');
  assert.equal(groupLabel(Date.parse(at(9, 8)), now), 'Yesterday');
  assert.equal(groupLabel(Date.parse(at(9, 6)), now), 'Tuesday');
  assert.equal(groupLabel(Date.parse(at(9, 1)), now), 'Last week');
  assert.equal(groupLabel(Date.parse(at(8, 30)), now), 'Last week');
  assert.equal(groupLabel(Date.parse(at(8, 3)), now), 'September');
  assert.equal(groupLabel(new Date(2025, 11, 20).getTime(), now), 'December 2025');
});

test('groupLabel counts calendar days, so late night to early morning is yesterday', () => {
  const late = new Date(2026, 9, 8, 23, 50).getTime();
  assert.equal(groupLabel(late, new Date(2026, 9, 9, 0, 10).getTime()), 'Yesterday');
});

test('stamps: a time inside a day group, a weekday under Last week, a date under a month', () => {
  assert.equal(rowStamp(Date.parse(at(9, 6, 14, 2)), now), '14:02');
  assert.equal(rowStamp(Date.parse(at(9, 1)), now), 'Thu');
  assert.equal(rowStamp(Date.parse(at(8, 3)), now), 'Sep 3');
  assert.equal(resultStamp(Date.parse(at(9, 9, 8)), now), 'Today');
  assert.equal(resultStamp(Date.parse(at(9, 6)), now), 'Tue');
  assert.equal(resultStamp(Date.parse(at(8, 3)), now), 'Sep 3');
  assert.equal(fullStamp(Date.parse(at(9, 6, 14, 2)), now), 'Tuesday 14:02');
});

test('a row says something, never a count or a duration', () => {
  assert.equal(rowLine(item('a', { line: 'Ruled out JSON5, because it also allows comments' })), 'Ruled out JSON5, because it also allows comments');
  assert.equal(rowLine(item('b')), '');
  assert.equal(rowLine(item('c', { open: true, tasksRunning: 4, state: 'working', line: 'x' })), 'Open · 4 tasks running');
  assert.equal(rowLine(item('d', { open: true, tasksRunning: 1 })), 'Open · 1 task running');
  assert.equal(rowLine(item('e', { open: true, state: 'needs-you', reason: 'Tag v2.4.1?' })), 'Open · waiting on you: Tag v2.4.1?');
  assert.equal(rowLine(item('f', { open: true, state: 'needs-you' })), 'Open · waiting on you');
  assert.equal(rowLine(item('g', { open: true, line: 'Discussed Load vs Open. No decision yet' })), 'Discussed Load vs Open. No decision yet');
  assert.equal(rowLine(item('h', { open: false, state: 'working', line: 'kept' })), 'kept');
});

test('trail and lead follow the live state', () => {
  assert.equal(rowTrail(item('a', { open: true }), now), 'Open');
  assert.equal(rowTrail(item('b', { at: at(9, 6, 14, 2) }), now), '14:02');
  assert.equal(rowLead(item('c', { open: true, state: 'needs-you' })), 'needs-you');
  assert.equal(rowLead(item('d', { open: true, tasksRunning: 2 })), 'working');
  assert.equal(rowLead(item('e', { open: false, tasksRunning: 2 })), 'conversation');
  assert.equal(rowLead(item('f')), 'conversation');
});

test('metaLine joins the day, the message and task counts and an archived mark', () => {
  assert.equal(metaLine(item('a', { at: at(9, 6, 14, 2), messages: 9, tasks: 2 }), now), 'Tuesday 14:02 · 9 messages · 2 tasks');
  assert.equal(metaLine(item('b', { at: at(9, 6, 14, 2), messages: 1, archived: true }), now), 'Tuesday 14:02 · 1 message · archived');
  assert.equal(decidedBy('codeaf', 'accepted'), 'codeaf, accepted');
  assert.equal(decidedBy('you'), 'you');
});

test('layout puts a heading before each new group and gives a row with files its taller height', () => {
  const items = [item('a', { at: at(9, 9, 9) }), item('b', { at: at(9, 6, 14, 2), files: [{ path: 'lexer.go', added: 1, removed: 0 }] }), item('c', { at: at(9, 6, 11, 40) })];
  const { entries, height } = layout(items, now, metrics);
  assert.deepEqual(entries.map(entry => entry.kind), ['group', 'row', 'group', 'row', 'row']);
  assert.deepEqual(entries.map(entry => entry.top), [0, 33, 94, 127, 214]);
  assert.equal(height, 275);
  assert.equal(rowHeight(items[1], metrics), 87);
});

test('windowOf draws what intersects the viewport and a margin either side', () => {
  const items = Array.from({ length: 200 }, (_, i) => item(`n${i}`, { at: at(9, 9, 10) }));
  const { entries } = layout(items, now, metrics);
  const top = windowOf(entries, 0, 300, 2);
  assert.equal(top.start, 0);
  assert.ok(top.end >= 6 && top.end <= 10, `end ${top.end}`);
  const middle = windowOf(entries, 61 * 100, 300, 2);
  assert.ok(middle.start > 90 && middle.end < 115, `${middle.start}-${middle.end}`);
  assert.deepEqual(windowOf([], 0, 300, 2), { start: 0, end: 0 });
});

test('stickyGroup names the heading above the viewport and lets the next one push it up', () => {
  const items = [item('a', { at: at(9, 9, 9) }), item('b', { at: at(9, 6, 14, 2) }), item('c', { at: at(9, 6, 11, 40) })];
  const { entries } = layout(items, now, metrics);
  // At rest the heading draws itself; it sticks only once scrolled away.
  assert.equal(stickyGroup(entries, 0), undefined);
  assert.deepEqual(stickyGroup(entries, 10), { label: 'Today', push: 0 });
  assert.equal(stickyGroup(entries, 80)?.label, 'Today');
  assert.equal(stickyGroup(entries, 80)?.push, Math.min(0, entries[2].top - 80 - 33));
  assert.equal(stickyGroup(entries, entries[2].top + 1)?.label, 'Tuesday');
  assert.equal(stickyGroup([], 0), undefined);
});

test('stepRow skips headings and clamps at the ends', () => {
  const { entries } = layout([item('a', { at: at(9, 9) }), item('b', { at: at(9, 6) })], now, metrics);
  const first = indexOfRow(entries, 'a');
  const second = indexOfRow(entries, 'b');
  assert.equal(stepRow(entries, first, 1), second);
  assert.equal(stepRow(entries, second, -1), first);
  assert.equal(stepRow(entries, second, 1), second);
  assert.equal(stepRow(entries, first, -1), first);
});

test('highlight wraps every hit, longest term first, and ignores empty terms', () => {
  assert.deepEqual(highlight('Fix it in the lexer', ['lexer']), [{ text: 'Fix it in the ', mark: false }, { text: 'lexer', mark: true }]);
  assert.deepEqual(highlight('Lexer and lexer.go', ['lexer', 'lexer.go']).filter(s => s.mark).map(s => s.text), ['Lexer', 'lexer.go']);
  assert.deepEqual(highlight('plain', ['', ' ']), [{ text: 'plain', mark: false }]);
  assert.deepEqual(highlight('a (b) c', ['(b)']).filter(s => s.mark).map(s => s.text), ['(b)']);
  assert.deepEqual(highlight('We decided in the lexer', ['decide', 'lexer']).filter(s => s.mark).map(s => s.text), ['lexer']);
});

test('queryTerms keeps the content words of a question', () => {
  assert.deepEqual(queryTerms('what did we decide about the lexer'), ['decide', 'lexer']);
  assert.deepEqual(queryTerms('lexer.go'), ['lexer.go']);
});

test('historyIdOf is the folder of a transcript, or the stem of a flat journal', () => {
  assert.equal(historyIdOf('/home/a/.codeaf/places/x/sess-1/transcript.jsonl'), 'sess-1');
  assert.equal(historyIdOf('/home/a/.codeaf/sessions/old-2.jsonl'), 'old-2');
  assert.equal(historyIdOf('mock-session-1.jsonl'), 'mock-session-1');
});

test('idleTabs: 12 hours idle, unless pinned, active, held (running or waiting) or without a conversation', () => {
  const hours = (n: number) => now - n * 3_600_000;
  const tab = (id: string, over: Partial<{ kind: string; pinned: boolean; hasSession: boolean }> = {}) => ({ id, kind: 'conversation', pinned: false, hasSession: true, ...over });
  const activity = {
    old: { at: hours(13), hold: false }, fresh: { at: hours(11), hold: false }, exact: { at: hours(12), hold: false },
    held: { at: hours(30), hold: true }, pinned: { at: hours(30), hold: false }, active: { at: hours(30), hold: false },
    empty: { at: hours(30), hold: false }, web: { at: hours(30), hold: false },
  };
  const tabs = [tab('old'), tab('fresh'), tab('exact'), tab('held'), tab('pinned', { pinned: true }), tab('active'), tab('empty', { hasSession: false }), tab('web', { kind: 'web' }), tab('unseen')];
  assert.deepEqual(idleTabs(tabs, 'active', activity, now), ['old', 'exact']);
});

test('a world needs-you record lifts a closed conversation\'s row to the amber glyph', () => {
  const rows = [item('a'), item('b'), item('c')];
  const merged = withWorldState(rows, [{ chatId: 'b', running: false, needsYou: 1, tasksRunning: 0 }]);
  assert.deepEqual(merged.map(one => one.id), ['a', 'b', 'c'], 'order is by time alone');
  assert.equal(merged[1].state, 'needs-you'); assert.equal(merged[1].open, true);
  assert.equal(rowLead(merged[1]), 'needs-you');
  assert.equal(merged[0], rows[0]);
});

test('a world running row marks a closed-but-running conversation working, matched by its transcript', () => {
  const merged = withWorldState([item('a')], [{ chatId: 'x', sessionFile: '/s/a/transcript.jsonl', running: true, needsYou: 0, tasksRunning: 2 }]);
  assert.equal(rowLead(merged[0]), 'working'); assert.equal(merged[0].tasksRunning, 2);
});

test('an idle world row or no world rows leaves the list untouched', () => {
  const rows = [item('a')];
  assert.equal(withWorldState(rows, []), rows);
  assert.equal(withWorldState(rows, [{ chatId: 'a', running: false, needsYou: 0, tasksRunning: 0 }]), rows);
});
