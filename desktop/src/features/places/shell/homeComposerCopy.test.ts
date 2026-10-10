import assert from 'node:assert/strict';
import test from 'node:test';
import { homeComposerPlaceholder, homePlaceIsEmpty } from './homeComposerCopy.ts';

const bare = { chats: [], children: [], attention: [], sources: undefined };

test('an empty place asks for the first chat, and a place that already holds something does not', () => {
  assert.equal(homeComposerPlaceholder('Launch site', true), 'Start the first chat in Launch site');
  assert.equal(homeComposerPlaceholder('codeaf', false), 'Start something in codeaf');
  assert.equal(homePlaceIsEmpty(bare), true);
  assert.equal(homePlaceIsEmpty({ ...bare, chats: [{ id: 'c', title: 'Launch copy' }] }), false);
  assert.equal(homePlaceIsEmpty({ ...bare, children: [{ id: 'p', name: 'Marketing', tint: 'rose', places: 0, chats: 0 }] }), false);
  assert.equal(homePlaceIsEmpty({ ...bare, attention: [{ id: 'a', title: 'Port', status: 'waiting' }] }), false);
  assert.equal(homePlaceIsEmpty({ ...bare, sources: [{ id: 's', kind: 'folder', label: 'brand' }] }), false);
});
