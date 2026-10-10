import test from 'node:test';
import assert from 'node:assert/strict';
import { relativeTime } from '../../../conversation/tabSummary.ts';
import type { Tab } from '../../types.ts';
import { askLabel, buildSections, flatRows, splitMatch, tabDigit, titleFromText, type RowInput } from './rows.ts';

const tab = (id: string, title: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title, draft: '', pinned: false, ...over });
const input = (over: Partial<RowInput> = {}): RowInput => ({ query: '', tabs: [], closed: [], files: [], terminal: false, terminalShortcut: '⌃`', fileShortcut: '⌘O', ...over });

test('with nothing typed the field offers only the start rows', () => {
  const sections = buildSections(input());
  assert.deepEqual(sections.map(s => s.title), ['Start']);
  assert.deepEqual(flatRows(sections).map(r => r.id), ['openfile']);
});

test('the terminal row is absent until a terminal is backed', () => {
  assert.deepEqual(flatRows(buildSections(input({ terminal: true }))).map(r => r.id), ['terminal', 'openfile']);
});

test('typed text puts the conversation row first, then Start, then Matching in file, tab, closed order', () => {
  const sections = buildSections(input({
    query: 'fix',
    files: [{ path: 'a/fixtures.go', name: 'fixtures.go', dir: 'a' }],
    tabs: [{ tab: tab('t1', 'Port fix to v1 branch'), shortcut: '⌘3', waiting: true }, { tab: tab('t2', 'Unrelated'), shortcut: '⌘2', waiting: false }],
    closed: [tab('c1', 'Fix it in the lexer'), tab('c2', 'Release', {}), tab('c3', 'fix field', { kind: 'newtab' })],
  }));
  assert.deepEqual(sections.map(s => s.title), [undefined, 'Start', 'Matching']);
  assert.deepEqual(flatRows(sections).map(r => r.id), ['ask', 'openfile', 'file:a/fixtures.go', 'tab:t1', 'closed:c1']);
  const tabRow = flatRows(sections).find(r => r.id === 'tab:t1')!;
  assert.deepEqual([tabRow.hint, tabRow.dot, tabRow.detail], ['⌘3', true, 'open tab']);
});

test('a recently closed row says how long ago, and a save with no close time says closed', () => {
  const now = Date.parse('2026-10-10T16:00:00Z');
  const hour = now - 3_600_000;
  const rows = flatRows(buildSections(input({
    query: 'fix',
    now,
    closed: [{ ...tab('c1', 'Fix it in the lexer'), closedAt: hour }, tab('old', 'fix later'), { ...tab('bad', 'fix me'), closedAt: Number.NaN }],
  })));
  assert.equal(rows.find(row => row.id === 'closed:c1')!.detail, `closed ${relativeTime(hour, now)}`);
  assert.equal(rows.find(row => row.id === 'closed:old')!.detail, 'closed');
  assert.equal(rows.find(row => row.id === 'closed:bad')!.detail, 'closed');
});

test('no matches means no Matching section', () => {
  assert.deepEqual(buildSections(input({ query: 'zzz', tabs: [{ tab: tab('t', 'abc'), waiting: false }] })).map(s => s.title), [undefined, 'Start']);
});

test('the conversation row quotes the first line, clipped', () => {
  assert.equal(askLabel('fix'), 'Ask “fix” in a new conversation');
  assert.match(askLabel('x'.repeat(100)), /^Ask “x{39}…” in a new conversation$/);
  assert.equal(askLabel('first\nsecond'), 'Ask “first” in a new conversation');
  assert.equal(titleFromText('  hello\nworld'), 'hello');
});

test('the match is split case-insensitively on the first occurrence', () => {
  assert.deepEqual(splitMatch('Port Fix to v1', 'fix'), ['Port ', 'Fix', ' to v1']);
  assert.deepEqual(splitMatch('Nothing', 'zz'), ['Nothing', '', '']);
});

test('tab digits follow the key rule: 1 to 8, then 9 for the last', () => {
  assert.deepEqual([tabDigit(0, 3), tabDigit(7, 12), tabDigit(8, 12), tabDigit(11, 12)], [1, 8, undefined, 9]);
  assert.equal(tabDigit(8, 9), 9);
});

test('an address puts a web row ahead of the conversation row; ordinary words do not', () => {
  const rows = flatRows(buildSections(input({ query: 'pkg.go.dev/encoding/json' })));
  assert.deepEqual(rows.slice(0, 3).map(r => r.id), ['web', 'ask', 'openfile']);
  assert.equal(rows[0].label, 'Open pkg.go.dev/encoding/json in a web tab');
  assert.equal(rows[0].target, 'https://pkg.go.dev/encoding/json');
  assert.equal(rows[1].hint, undefined);
  assert.deepEqual(flatRows(buildSections(input({ query: 'why is the lexer slow' }))).map(r => r.id).slice(0, 2), ['ask', 'openfile']);
  assert.equal(flatRows(buildSections(input({ query: 'ftp://example.com/x' })))[0].id, 'ask');
  assert.equal(flatRows(buildSections(input({ query: 'a.b c.d' })))[0].id, 'ask');
});
