import test from 'node:test';
import assert from 'node:assert/strict';
import type { HistoryItem, SearchResult } from '../../../history/types.ts';
import { fromHistoryLimit, historyDetail, matchesOf, rankConversations } from './historyRows.ts';
import { buildSections, flatRows, type RowInput } from './rows.ts';

const item = (id: string, title: string, over: Partial<HistoryItem> = {}): HistoryItem => ({ id, sessionFile: `/s/${id}`, title, at: '2026-10-06T14:02:00Z', messages: 4, tasks: 0, tasksRunning: 0, files: [], fileCount: 0, decisions: 0, state: 'idle', open: false, archived: false, ...over });
const result = (over: Partial<SearchResult> = {}): SearchResult => ({ query: 'lexer fix', decisions: [], discussed: [], files: [], tasks: [], counts: { decisions: 0, discussed: 0, files: 0, tasks: 0 }, ...over });
const input = (over: Partial<RowInput> = {}): RowInput => ({ query: 'lexer fix', tabs: [], closed: [], files: [], terminal: false, terminalShortcut: '⌃`', fileShortcut: '⌘O', seeAllShortcut: '⌘↵', ...over });

test('the best match leads, then discussed order, then conversations only a decision or task found; each once', () => {
  const found = rankConversations(result({
    best: { item: item('a', 'Fix it in the lexer'), answer: 'We chose the lexer.', terms: ['lexer'] },
    discussed: [{ id: 'b', title: 'B', snippet: 'one\ntwo', at: '' }, { id: 'a', title: 'A', snippet: 'dup', at: '' }],
    decisions: [{ id: 'c', title: 'Rule out JSON5', context: '', at: '' }],
    tasks: [{ id: 'd', taskId: 't', conversationTitle: 'D', title: 'T', snippet: '', at: '' }],
    counts: { decisions: 1, discussed: 14, files: 0, tasks: 1 },
  }));
  assert.deepEqual(found.candidates.map(c => c.id), ['a', 'b', 'c', 'd']);
  assert.equal(found.total, 14);
});

test('a row says what the engine said: a decision first, else the answer or snippet, whitespace folded', () => {
  const { candidates } = rankConversations(result({
    best: { item: item('a', 'A'), answer: 'The   answer.', terms: [] },
    discussed: [{ id: 'b', title: 'B', snippet: 'a\n snippet', at: '' }, { id: 'c', title: 'C', snippet: 'ignored', at: '' }],
    decisions: [{ id: 'c', title: 'keep strict mode', context: '', at: '' }],
  }));
  assert.deepEqual(candidates.map(c => c.line), ['The answer.', 'a snippet', 'decided: keep strict mode']);
});

test('the total is never below the conversations actually returned', () => {
  assert.equal(rankConversations(result({ discussed: [{ id: 'a', title: '', snippet: 's', at: '' }, { id: 'b', title: '', snippet: 's', at: '' }] })).total, 2);
});

test('only the first conversations the section shows are kept, and one that could not be read leaves no row', () => {
  const candidates = [{ id: 'a', line: 'x', item: item('a', 'A') }, { id: 'b', line: 'y' }, { id: 'c', line: 'z' }];
  const rows = matchesOf('q', candidates, new Map([['c', item('c', 'C')]]), 9).rows;
  assert.deepEqual(rows.map(r => r.id), ['a', 'c']);
  assert.equal(rows.length <= fromHistoryLimit, true);
});

test('at most three conversations are listed, even when more are found', () => {
  const candidates = ['a', 'b', 'c', 'd', 'e'].map(id => ({ id, line: '', item: item(id, id.toUpperCase()) }));
  assert.deepEqual(matchesOf('q', candidates, new Map(), 9).rows.map(r => r.id), ['a', 'b', 'c']);
});

test('detail is the engine line, then archived, and nothing when there is neither', () => {
  assert.equal(historyDetail({ id: 'a', line: 'decided: x', item: item('a', 'A', { archived: true }) }), '· decided: x · archived');
  assert.equal(historyDetail({ id: 'a', line: '', item: item('a', 'A', { archived: true }) }), '· archived');
  assert.equal(historyDetail({ id: 'a', line: '', item: item('a', 'A') }), undefined);
});

test('From history follows the ask row, ends with See all N, and only appears for the query it answers', () => {
  const history = matchesOf('lexer fix', [{ id: 'a', line: 'decided: x', item: item('a', 'Fix it in the lexer') }], new Map(), 14);
  const sections = buildSections(input({ history }));
  assert.deepEqual(sections.map(s => s.title), [undefined, 'From history', 'Start']);
  const rows = flatRows(sections);
  assert.deepEqual(rows.map(r => r.id), ['ask', 'history:a', 'seeall', 'openfile']);
  assert.equal(rows[1].conversation?.sessionFile, '/s/a');
  assert.deepEqual([rows[2].label, rows[2].hint], ['See all 14 in History', '⌘↵']);
  assert.deepEqual(buildSections(input({ history, query: 'lexer fi' })).map(s => s.title), [undefined, 'Start']);
  assert.deepEqual(buildSections(input({ history, query: '' })).map(s => s.title), ['Start']);
});

test('no matches draws no section', () => {
  assert.deepEqual(buildSections(input({ history: matchesOf('lexer fix', [], new Map(), 0) })).map(s => s.title), [undefined, 'Start']);
});
