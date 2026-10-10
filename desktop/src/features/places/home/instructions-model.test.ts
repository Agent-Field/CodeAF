import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { homeViewFromDigest } from '../shell/selectors.ts';
import { dropHighlight, droppedSource, instructionWrite, instructionsVisible, withNote } from './instructions-model.ts';

// The same bytes place-actions puts on a dragged chat or place. Imported from there, this file pulls the desktop client.
const chatDragType = 'application/x-codeaf-chat';
const placeDragType = 'application/x-codeaf-place';

const fixture = (name: string): any => JSON.parse(readFileSync(new URL(`../fixtures/${name}.json`, import.meta.url), 'utf8'));

test('an empty instructions string stays off the Home until Write instructions opens it', () => {
  assert.equal(instructionsVisible(undefined, false), false);
  assert.equal(instructionsVisible('  ', false), false);
  assert.equal(instructionsVisible('', true), true);
  assert.equal(instructionsVisible('Keep the parser strict.', false), true);
  assert.equal(homeViewFromDigest(fixture('home-place')).instructions, undefined);
  const digest = fixture('home-place');
  digest.place.instructions = 'Keep the parser strict.';
  assert.equal(homeViewFromDigest(digest).instructions, 'Keep the parser strict.');
});

test('blur writes only a real change, and a note becomes its own paragraph', () => {
  assert.equal(instructionWrite('Keep it.', 'Keep it.'), undefined);
  assert.equal(instructionWrite('', '   '), undefined);
  assert.equal(instructionWrite('Keep it.', ''), '');
  assert.equal(instructionWrite('', 'Use the brand voice.'), 'Use the brand voice.');
  assert.equal(withNote('Use the brand voice.', '  '), undefined);
  assert.equal(withNote('', 'Ship on Tuesday'), 'Ship on Tuesday');
  assert.equal(withNote('Use the brand voice.', 'Ship on Tuesday'), 'Use the brand voice.\n\nShip on Tuesday');
});

test('a dropped file or link is a source, and a dragged chat or place is not', () => {
  assert.equal(droppedSource({ types: ['Files'], files: [{ name: 'voice.md', path: '/work/voice.md' }], text: '' })?.ref, '/work/voice.md');
  assert.deepEqual(droppedSource({ types: ['text/uri-list'], files: [], text: 'https://example.com/brand' }), { kind: 'url', ref: 'https://example.com/brand' });
  assert.equal(droppedSource({ types: [chatDragType], files: [], text: 'https://example.com/brand' }), undefined);
  assert.equal(droppedSource({ types: [placeDragType, 'Files'], files: [{ name: 'x' }], text: '' }), undefined);
  assert.equal(dropHighlight(['Files']), true);
  assert.equal(dropHighlight(['text/plain']), false);
  assert.equal(dropHighlight([chatDragType, 'Files']), false);
});
