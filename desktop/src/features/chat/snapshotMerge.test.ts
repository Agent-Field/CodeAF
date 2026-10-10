import test from 'node:test';
import assert from 'node:assert/strict';
import { isTail, mergeTail, type SnapshotTail } from './snapshotMerge.ts';
import type { EngineEntry, EngineSnapshot } from './engine-client.ts';

const entry = (Text: string, extra: Partial<EngineEntry> = {}): EngineEntry => ({ Role: 'assistant', Text, ...extra });
const held = (texts: string[], seq = 1): EngineSnapshot => ({
  id: 's', sessionFile: 'f', workspace: 'w', model: 'm', persistent: true, running: false, needsPerson: false,
  entries: texts.map(t => entry(t)), tasks: [], usage: { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 }, title: 't', seq,
});
const tail = (base: EngineSnapshot, from: number, entries: EngineEntry[], total: number, over: Partial<SnapshotTail> = {}): SnapshotTail => {
  const { entries: _drop, ...rest } = base;
  return { header: { ...rest, seq: base.seq + 1, running: true, entryCount: total }, from, entries, ...over };
};

test('an appended tail keeps the held entries and adds the new ones after `from`', () => {
  const h = held(['a', 'b']);
  const merged = mergeTail(h, tail(h, 2, [entry('c')], 3));
  assert.deepEqual(merged.entries.map(e => e.Text), ['a', 'b', 'c']);
  assert.equal(merged.seq, 2);
  assert.equal(merged.running, true);
  assert.equal('entryCount' in merged, false);
});

test('a tail starting inside the held transcript replaces from that index on', () => {
  const h = held(['a', 'b', 'c']);
  assert.deepEqual(mergeTail(h, tail(h, 1, [entry('B')], 2)).entries.map(e => e.Text), ['a', 'B']);
});

test('a header-only tail replaces the header and leaves the transcript alone', () => {
  const h = held(['a', 'b']);
  const merged = mergeTail(h, tail(h, 2, [], 2));
  assert.deepEqual(merged.entries.map(e => e.Text), ['a', 'b']);
  assert.equal(merged.seq, 2);
});

test('a null entry list is a header-only tail', () => {
  const h = held(['a']);
  assert.equal(mergeTail(h, tail(h, 1, null, 1)).entries.length, 1);
});

test('reset replaces the transcript wholesale, even with a shorter one', () => {
  const h = held(['a', 'b', 'c']);
  const merged = mergeTail(h, tail(h, 0, [entry('summary')], 1, { reset: true }));
  assert.deepEqual(merged.entries.map(e => e.Text), ['summary']);
});

test('an omitted output stays unloaded and flagged', () => {
  const h = held(['a']);
  const big = entry('', { Role: 'tool', CallID: 'c1', Output: '', OutputOmitted: true, OutputBytes: 20000 });
  const [, kept] = mergeTail(h, tail(h, 1, [big], 2)).entries;
  assert.equal(kept.Output, '');
  assert.equal(kept.OutputOmitted, true);
  assert.equal(kept.OutputBytes, 20000);
});

test('a gap or a miscounted total throws rather than showing a wrong transcript', () => {
  const h = held(['a']);
  assert.throws(() => mergeTail(h, tail(h, 5, [entry('x')], 6)), /starts at 5/);
  assert.throws(() => mergeTail(h, tail(h, 1, [entry('x')], 9)), /counts 9/);
});

test('isTail tells a tail from a whole snapshot', () => {
  const h = held(['a']);
  assert.equal(isTail(tail(h, 1, [], 1)), true);
  assert.equal(isTail(h), false);
});
