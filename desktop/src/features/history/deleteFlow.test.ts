import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { deleteAsk, deleteQuestion, deletedSentence, idsBetween } from './deleteFlow.ts';

const tokens = JSON.parse(readFileSync(new URL('../../design/tokens.json', import.meta.url), 'utf8')) as { interaction: { historyDeleteToastMs: number; toastDuration: number } };

const row = (id: string, archived = true) => ({ id, archived });

test('the confirm and the toast name one chat in the singular and fourteen in the plural', () => {
  assert.equal(deleteQuestion(1), 'Delete 1 chat?');
  assert.equal(deleteQuestion(14), 'Delete 14 chats?');
  assert.equal(deletedSentence(1), '1 chat deleted');
  assert.equal(deletedSentence(14), '14 chats deleted');
});

test('Undo on a delete waits 10 seconds, longer than the ordinary toast', () => {
  assert.equal(tokens.interaction.historyDeleteToastMs, 10_000);
  assert.ok(tokens.interaction.historyDeleteToastMs > tokens.interaction.toastDuration);
});

test('Delete… on one archived row confirms that row', () => {
  const items = [row('a'), row('b', false), row('c')];
  assert.deepEqual(deleteAsk('c', ['c'], items), { ids: ['c'], anchorId: 'c' });
});

test('Delete… inside a selection confirms every archived chat and replaces the first', () => {
  const items = [row('a'), row('b', false), row('c'), row('d')];
  assert.deepEqual(deleteAsk('d', ['a', 'b', 'c', 'd'], items), { ids: ['a', 'c', 'd'], anchorId: 'a' });
});

test('Delete… outside the selection confirms only the row that was asked', () => {
  const items = [row('a'), row('b'), row('c')];
  assert.deepEqual(deleteAsk('c', ['a', 'b'], items), { ids: ['c'], anchorId: 'c' });
});

test('an unarchived row is not asked', () => {
  assert.equal(deleteAsk('b', ['a', 'b'], [row('a'), row('b', false)]), undefined);
});

test('a shift-click selects the run in list order, from either end', () => {
  const items = [{ id: 'a' }, { id: 'b' }, { id: 'c' }, { id: 'd' }];
  assert.deepEqual(idsBetween(items, 'b', 'd'), ['b', 'c', 'd']);
  assert.deepEqual(idsBetween(items, 'd', 'b'), ['b', 'c', 'd']);
});
