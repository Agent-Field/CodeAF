import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { tileDropEffect, tileDropLabel, tileDropMode, tileDropVisible, type TileDropGate } from './tileDnd.ts';

const chat = 'application/x-codeaf-chat';
const place = 'application/x-codeaf-place';
const open = (extra: Partial<TileDropGate> = {}): TileDropGate => ({ targetId: 'notes', canFile: true, ...extra });

test('the drop label is the component\'s "Add here", with or without Option', () => {
  assert.equal(tileDropLabel, 'Add here');
  assert.equal(tileDropMode({ altKey: false }), 'add');
  assert.equal(tileDropMode({ altKey: true }), 'move');
  assert.equal(tileDropEffect({ altKey: false }), 'copy');
  assert.equal(tileDropEffect({ altKey: true }), 'move');
});

test('a chat or another place may land; the place in flight may not land on itself', () => {
  assert.equal(tileDropVisible(open({ payload: { kind: 'chat', ids: ['c1'] } })), true);
  assert.equal(tileDropVisible(open({ payload: { kind: 'place', ids: ['papers'] } })), true);
  assert.equal(tileDropVisible(open({ payload: { kind: 'place', ids: ['notes'] } })), false);
  assert.equal(tileDropVisible(open({ payload: { kind: 'place', ids: ['papers', 'notes'] } })), false);
  assert.equal(tileDropVisible(open({ payload: { kind: 'chat', ids: [] } })), false);
  assert.equal(tileDropVisible(open({ payload: { kind: 'chat', ids: [' '] } })), false);
});

test('a hidden payload is judged by its type, and a known self-drop still wins', () => {
  assert.equal(tileDropVisible(open({ types: [chat] })), true);
  assert.equal(tileDropVisible(open({ types: [place] })), true);
  assert.equal(tileDropVisible(open({ types: ['text/plain', 'Files'] })), false);
  assert.equal(tileDropVisible(open({ payload: { kind: 'place', ids: ['notes'] }, types: [place] })), false);
});

test('offline, archived, and unwired tiles draw no target', () => {
  const payload = { kind: 'chat' as const, ids: ['c1'] };
  assert.equal(tileDropVisible(open({ payload, readOnly: true })), false);
  assert.equal(tileDropVisible(open({ payload, archived: true })), false);
  assert.equal(tileDropVisible(open({ payload, canFile: false })), false);
  assert.equal(tileDropVisible(open()), false);
});

test('the type names match the drag place-actions writes', () => {
  const source = readFileSync(new URL('../place-actions.ts', import.meta.url), 'utf8');
  assert.match(source, new RegExp(`export const chatDragType = '${chat}'`));
  assert.match(source, new RegExp(`export const placeDragType = '${place}'`));
  assert.match(source, /tileDropMode as dropMode/);
});
