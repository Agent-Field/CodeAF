import assert from 'node:assert/strict';
import test from 'node:test';
import { appendPlaceChange, attachLivePlaceChanges, livePlaceChange, placeChangeLine } from './placeChange.ts';
import type { TurnV2 } from './types.ts';

const turn = (blocks: TurnV2['blocks']): TurnV2 => ({ id: 't', user: 'hi', attachments: [], steer: [], blocks, state: 'done', digest: '' });

test('the live sentence names the place and the sources the event carried', () => {
  assert.equal(placeChangeLine('Now also using Release', { placeName: 'Release', sources: ['brand-voice.md'], added: true }), 'Now also using Release: brand-voice.md');
  assert.equal(placeChangeLine('No longer using Release', { placeName: 'Release', sources: [], added: false }), 'No longer using Release');
  assert.equal(placeChangeLine('', { placeName: 'Release', sources: ['a', '2 more'], added: true }), 'Now also using Release: a, 2 more');
  assert.equal(placeChangeLine('Now also using Release: brand-voice.md', { placeName: 'Release', sources: ['brand-voice.md'], added: true }), 'Now also using Release: brand-voice.md');
});

test('a placeChange event becomes one line, and a second copy of its receipt is dropped', () => {
  const event = { kind: 'placeChange', text: 'Now also using Release', placeChange: { placeName: 'Release', sources: ['brand-voice.md'], added: true, undo: 'rc_1' } };
  const note = livePlaceChange(event);
  assert.deepEqual(note, { text: 'Now also using Release: brand-voice.md', undo: 'rc_1', placeName: 'Release' });
  assert.equal(livePlaceChange({ kind: 'text', text: 'hello' }), undefined);
  assert.equal(livePlaceChange({ kind: 'placeChange', text: 'Now also using Release' }), undefined);
  assert.equal(appendPlaceChange([note!], note!).length, 1);
});

test('the recorded line replaces the live one that shares its undo receipt', () => {
  const live = [{ text: 'Now also using Release', undo: 'rc_1', placeName: 'Release' }];
  const held = attachLivePlaceChanges({
    turns: [turn([{ kind: 'placeChange', id: 'rec', text: 'Now also using Release: brand-voice.md', undoReceipts: ['rc_1'] }])],
    preface: [],
  }, live);
  assert.equal(held.turns[0].blocks.length, 1);
  const empty = attachLivePlaceChanges({ turns: [], preface: [] }, live);
  assert.equal(empty.preface[0]?.kind, 'placeChange');
  const latest = attachLivePlaceChanges({ turns: [turn([]), turn([])], preface: [] }, live);
  assert.equal(latest.turns[0].blocks.length, 0);
  assert.equal(latest.turns[1].blocks[0]?.kind, 'placeChange');
});
