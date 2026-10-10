import test from 'node:test';
import assert from 'node:assert/strict';
import { knowsList, sourceWords, stillTrueDue, type KnowsLine } from './model.ts';

const now = new Date('2026-10-10T12:00:00Z');
const DAY = 86_400_000;
const ago = (days: number) => new Date(now.getTime() - days * DAY).toISOString();
const line = (id: string, extra: Partial<KnowsLine> = {}): KnowsLine => ({ id, placeId: 'marketing', text: id, source: { kind: 'you-wrote' }, ...extra });

test('Home shows the top 3 by last use, then recency; All retains every remaining line', () => {
  const lines = [line('new', { createdAt: ago(0) }), line('older-use', { lastUsedAt: ago(2) }),
    line('tie-old', { lastUsedAt: ago(1), createdAt: ago(5) }), line('tie-new', { lastUsedAt: ago(1), createdAt: ago(3) })];
  const before = structuredClone(lines);
  const view = knowsList(lines, now);
  assert.deepEqual(view.home.map(row => row.line.id), ['tie-new', 'tie-old', 'older-use']);
  assert.deepEqual(view.all.map(row => row.line.id), ['tie-new', 'tie-old', 'older-use', 'new']);
  assert.equal(view.allLabel, 'All');
  assert.deepEqual(lines, before);
});

test('empty and at most three lines omit All; unknown dates preserve stored ties', () => {
  assert.deepEqual(knowsList([], now), { home: [], all: [], allLabel: '' });
  const view = knowsList([line('a'), line('b', { createdAt: 'invalid' }), line('c')], now);
  assert.equal(view.allLabel, '');
  assert.deepEqual(view.home.map(row => row.line.id), ['a', 'b', 'c']);
  assert.ok(view.home.every(row => row.prompt === '' && !row.struck));
});

test('recency falls back to source time; valid last use outranks a never-used line', () => {
  const view = knowsList([line('unknown'), line('source', { source: { kind: 'you-wrote', at: ago(2) } }),
    line('created', { createdAt: ago(1), lastUsedAt: 'invalid' }), line('used', { lastUsedAt: ago(90) })], now);
  assert.deepEqual(view.all.map(row => row.line.id), ['used', 'created', 'source', 'unknown']);
});

test('replaced lines are struck until exactly 7 days, then absent from Home and All', () => {
  const kept = line('kept', { replacedBy: 'new', replacedAt: ago(7 - 1 / DAY) });
  const expired = line('expired', { replacedBy: 'new', replacedAt: ago(7) });
  const view = knowsList([kept, expired, line('new')], now);
  assert.deepEqual(view.all.map(row => row.line.id), ['kept', 'new']);
  assert.equal(view.home[0].struck, true);
  assert.equal(view.home[0].prompt, '');
  assert.equal(view.home[1].struck, false);
  assert.equal(knowsList([expired, line('a'), line('b'), line('c')], now).allLabel, '');
});

test('You said in Launch post · Tue', () => {
  assert.equal(sourceWords(line('said', { source: { kind: 'said-in-chat', chatId: 'launch', at: '2026-10-06T12:00:00Z' } }), { launch: 'Launch post' }), 'You said in Launch post · Tue');
});

test('Learned from 3 of your answers', () => {
  assert.equal(sourceWords(line('learned', { source: { kind: 'learned', answers: 3 } })), 'Learned from 3 of your answers');
});

test('brand-voice.md · you added', () => {
  for (const path of ['brand-voice.md', '/docs/brand-voice.md', 'C:\\docs\\brand-voice.md']) {
    assert.equal(sourceWords(line('file', { source: { kind: 'file', path } })), 'brand-voice.md · you added');
  }
});

test('Replaced Tue · kept for 7 days overrides original provenance', () => {
  assert.equal(sourceWords(line('old', { replacedBy: 'new', replacedAt: '2026-10-06T12:00:00Z' })), 'Replaced Tue · kept for 7 days');
});

test('unknown provenance details draw nothing rather than ids, zero counts or invented dates', () => {
  for (const source of [{ kind: 'said-in-chat', chatId: 'unknown' }, { kind: 'learned', answers: 0 },
    { kind: 'learned', answers: -1 }, { kind: 'learned', answers: 1.5 }, { kind: 'file' }] as KnowsLine['source'][]) {
    assert.equal(sourceWords(line('unknown', { source })), '');
  }
  assert.equal(sourceWords(line('replaced', { replacedBy: 'new', replacedAt: 'invalid' })), '');
  assert.equal(sourceWords(line('said', { source: { kind: 'said-in-chat', chatId: 'launch', at: 'invalid' } }), { launch: 'Launch post' }), 'You said in Launch post');
  assert.equal(sourceWords(line('written')), 'You wrote');
});

test('still true? appears at 60 unused days, using last use or creation/source time', () => {
  assert.equal(knowsList([line('due', { lastUsedAt: ago(60) })], now).home[0].prompt, 'still true?');
  assert.equal(stillTrueDue(line('not-yet', { lastUsedAt: ago(60 - 1 / DAY) }), now), false);
  assert.equal(stillTrueDue(line('used', { createdAt: ago(100), lastUsedAt: ago(1) }), now), false);
  assert.equal(stillTrueDue(line('never-used', { createdAt: ago(60) }), now), true);
  assert.equal(stillTrueDue(line('source', { source: { kind: 'you-wrote', at: ago(60) } }), now), true);
  assert.equal(stillTrueDue(line('zero', { createdAt: '0001-01-01T00:00:00Z' }), now), false);
  assert.equal(stillTrueDue(line('future', { lastUsedAt: ago(-1) }), now), false);
});

test('replacement and engine-recorded ask cooldown suppress still true?', () => {
  assert.equal(stillTrueDue(line('replaced', { lastUsedAt: ago(100), replacedBy: 'new' }), now), false);
  assert.equal(stillTrueDue(line('asked', { lastUsedAt: ago(100), askedStillTrueAt: ago(60 - 1 / DAY) }), now), false);
  assert.equal(stillTrueDue(line('due-again', { lastUsedAt: ago(100), askedStillTrueAt: ago(60) }), now), true);
  assert.equal(stillTrueDue(line('invalid-clock', { createdAt: ago(100) }), new Date('invalid')), false);
});
