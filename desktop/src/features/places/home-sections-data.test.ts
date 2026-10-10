import { test } from 'node:test';
import assert from 'node:assert/strict';
import { homeDecisions, homeStatus } from './home-sections-data.ts';

test('Home maps only the real action, reason, display name and recorded confidence', () => {
  assert.deepEqual(homeDecisions({ decisions: [{ id: 'd', action: 'Read the notes', at: '2026-10-10', by: 'opaque-id', because: 'Allowed twice', percent: 97 }] }), [
    { id: 'd', title: 'Read the notes', at: '2026-10-10', detail: 'Allowed twice', why: { by: undefined, because: 'Allowed twice', sure: '97%' } },
  ]);
  assert.deepEqual(homeDecisions({ decisions: [] }), []);
});

test('Home rejects malformed reads and does not turn missing status into learning', () => {
  for (const value of [null, {}, { mode: 'imagined' }, { mode: 'deciding', agreedWeek: -1 }, { mode: 'learning', learning: { agreed: 21, of: 20 } }]) {
    assert.throws(() => homeStatus(value));
  }
  for (const value of [null, { decisions: [null] }, { decisions: [{ id: 'd', at: 'today' }] }]) assert.throws(() => homeDecisions(value));
  assert.deepEqual(homeStatus({ mode: 'none' }), { mode: 'none' });
});
