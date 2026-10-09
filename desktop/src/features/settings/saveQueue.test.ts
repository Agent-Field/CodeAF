import test from 'node:test';
import assert from 'node:assert/strict';
import { createSaveQueue } from './saveQueue.ts';

const deferred = () => { let release!: () => void; const result = new Promise<void>(resolve => { release = resolve; }); return { result, release }; };

test('one choice is saved in intent order while another choice can finish', async () => {
  const queue = createSaveQueue();
  const held = deferred();
  const started = deferred();
  const writes: string[] = [];
  const first = queue.enqueue('title', 'high', async () => { writes.push('high'); started.release(); await held.result; return 'high'; });
  const last = queue.enqueue('title', 'low', async () => { writes.push('low'); return 'low'; });
  await started.result;
  assert.equal(await queue.enqueue('recap', 'low', async () => { writes.push('recap'); return 'recap'; }), 'recap');
  assert.deepEqual(writes, ['high', 'recap']);
  held.release();
  assert.deepEqual(await Promise.all([first, last]), ['high', 'low']);
  assert.deepEqual(writes, ['high', 'recap', 'low']);
});

test('duplicate blur and Enter share one outstanding save, but a later retry is new', async () => {
  const queue = createSaveQueue();
  const held = deferred();
  let writes = 0;
  const save = () => queue.enqueue('depth', '3', async () => { writes++; await held.result; return 3; });
  const first = save();
  assert.strictEqual(save(), first);
  held.release();
  assert.equal(await first, 3);
  assert.equal(writes, 1);
  assert.equal(await save(), 3);
  assert.equal(writes, 2);
});

test('a failed save does not discard the newer intent or poison a retry', async () => {
  const queue = createSaveQueue();
  const first = queue.enqueue('title', 'high', async () => { throw new Error('offline'); });
  const last = queue.enqueue('title', 'low', async () => 'low');
  await assert.rejects(first, /offline/);
  assert.equal(await last, 'low');
  assert.equal(await queue.enqueue('title', 'high', async () => 'high'), 'high');
});
