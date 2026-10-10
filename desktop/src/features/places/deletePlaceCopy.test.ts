import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { placeDeleteQuestion } from './deletePlaceCopy.ts';

const tokens = JSON.parse(readFileSync(new URL('../../design/tokens.json', import.meta.url), 'utf8')) as {
  interaction: { placeDeleteUndoMs: number; toastDuration: number; historyDeleteToastMs: number };
};

const impact = (children: number, unplaced: string[], chatsHere = unplaced.length) => ({ children, chatsHere, wouldBeUnplaced: unplaced });

test('the confirm names unplaced chats and places that move up, and never a chat delete', () => {
  assert.equal(
    placeDeleteQuestion('Reading', impact(3, ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n'])),
    'Delete “Reading”? 14 chats become unplaced · 3 places move up. No chat is deleted.',
  );
  assert.equal(
    placeDeleteQuestion('Reading', impact(2, ['a', 'b'], 3)),
    'Delete “Reading”? 2 chats become unplaced · 2 places move up. No chat is deleted.',
  );
});

test('one chat and one place stay singular', () => {
  assert.equal(
    placeDeleteQuestion('Studio', impact(1, ['a'])),
    'Delete “Studio”? 1 chat becomes unplaced · 1 place moves up. No chat is deleted.',
  );
});

test('a zero count is omitted, and chats that keep another place are not a third count', () => {
  assert.equal(placeDeleteQuestion('Software', impact(1, [], 4)), 'Delete “Software”? 1 place moves up. No chat is deleted.');
  assert.equal(placeDeleteQuestion('Notes', impact(0, ['a', 'b'])), 'Delete “Notes”? 2 chats become unplaced. No chat is deleted.');
  assert.equal(placeDeleteQuestion('Empty', impact(0, [], 0)), 'Delete “Empty”? No chat is deleted.');
});

test('Undo after deleting a place waits 10 seconds, the same span as a History delete and longer than an ordinary toast', () => {
  assert.equal(tokens.interaction.placeDeleteUndoMs, 10_000);
  assert.equal(tokens.interaction.placeDeleteUndoMs, tokens.interaction.historyDeleteToastMs);
  assert.ok(tokens.interaction.placeDeleteUndoMs > tokens.interaction.toastDuration);
});
