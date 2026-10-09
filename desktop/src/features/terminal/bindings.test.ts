import test from 'node:test';
import assert from 'node:assert/strict';
import { parseBindings } from './bindings.ts';

test('a valid binding keeps its session, terminal and refusal', () => {
  assert.deepEqual(parseBindings({ a: { sessionFile: 's.jsonl', terminalId: 't1' }, b: { refused: '16 terminals are already running; close one first' } }), {
    a: { sessionFile: 's.jsonl', terminalId: 't1' },
    b: { refused: '16 terminals are already running; close one first' },
  });
});
test('an entry that names neither a terminal nor a refusal is dropped and the others stay', () => {
  assert.deepEqual(parseBindings({ a: { sessionFile: 's.jsonl' }, b: 'x', c: null, d: { terminalId: '' }, e: { sessionFile: 's.jsonl', terminalId: 't9', extra: 1 } }), { e: { sessionFile: 's.jsonl', terminalId: 't9' } });
});
test('anything that is not a record parses as empty', () => {
  for (const value of [null, undefined, 4, 'x', []]) assert.deepEqual(parseBindings(value), {});
});
