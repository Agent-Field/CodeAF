import test from 'node:test';
import assert from 'node:assert/strict';
import { createToastQueue, type ToastClock } from './toastQueue.ts';

function fixture() {
  let time = 0;
  let next = 0;
  const timers = new Map<number, { at: number; run: () => void }>();
  const clock: ToastClock = {
    now: () => time,
    setTimeout(run, ms) { const id = ++next; timers.set(id, { at: time + ms, run }); return id; },
    clearTimeout(id) { timers.delete(id as number); },
  };
  const queue = createToastQueue(undefined, clock);
  const tick = (ms: number) => {
    const end = time + ms;
    while (true) {
      const first = [...timers].sort((a, b) => a[1].at - b[1].at)[0];
      if (!first || first[1].at > end) break;
      time = first[1].at; timers.delete(first[0]); first[1].run();
    }
    time = end;
  };
  return { queue, tick };
}

test('newest replaces the single toast and receives its full default six seconds', () => {
  const { queue, tick } = fixture();
  const first = queue.show({ lead: 'dot', message: 'First' });
  tick(5000);
  const second = queue.show({ lead: 'archive', message: 'Second' });
  queue.dismiss(first);
  tick(5999);
  assert.equal(queue.channel.getToast()?.id, second);
  assert.equal(queue.channel.getToasts().length, 1);
  tick(1);
  assert.equal(queue.channel.getToast(), null);
  queue.dispose();
});

test('hover and focus independently pause; resume expires only the remaining time', () => {
  const { queue, tick } = fixture();
  const id = queue.show({ lead: 'dot', message: 'Held' });
  tick(2000);
  queue.hold(id, 'hover', true);
  queue.hold(id, 'focus', true);
  tick(10000);
  queue.hold(id, 'hover', false);
  tick(10000);
  assert.equal(queue.channel.getToast()?.id, id);
  queue.hold(id, 'focus', false);
  tick(3999);
  assert.equal(queue.channel.getToast()?.id, id);
  tick(1);
  assert.equal(queue.channel.getToast(), null);
  queue.dispose();
});

test('delete may last ten seconds and stale holds cannot pause its replacement', () => {
  const { queue, tick } = fixture();
  const old = queue.show({ lead: 'dot', message: 'Old' });
  queue.hold(old, 'focus', true);
  const id = queue.show({ lead: 'dot', message: 'Deleted', durationMs: 10000 });
  queue.hold(old, 'focus', true);
  tick(9999); assert.equal(queue.channel.getToast()?.id, id);
  tick(1); assert.equal(queue.channel.getToast(), null);
  queue.dispose();
});

test('actions and Undo use the same channel and settle only their current toast', async () => {
  const { queue } = fixture();
  let actions = 0;
  const id = queue.show({ lead: 'dot', message: 'Action', actions: [{ label: 'Review', kind: 'ghost', onAction: () => { actions++; } }] });
  await queue.channel.act(id, 0);
  assert.equal(actions, 1);
  assert.equal(queue.channel.getToast(), null);
  const undo = queue.show({ lead: 'dot', message: 'Undoable', undo: () => { actions--; } });
  await queue.channel.undo(undo);
  assert.equal(actions, 0);
  assert.equal(queue.channel.getToast(), null);
  queue.dispose();
});

test('zero duration remains until explicitly dismissed', () => {
  const { queue, tick } = fixture();
  const id = queue.show({ lead: 'dot', message: 'Persistent', durationMs: 0 });
  tick(100000); assert.equal(queue.channel.getToast()?.id, id);
  queue.dismiss(id); assert.equal(queue.channel.getToast(), null);
  queue.dispose();
});

test('legacy callers share the timer; a refused action can restart while still held', () => {
  const { queue, tick } = fixture();
  const id = queue.channel.show({ message: ['Existing feature'] });
  tick(5000);
  queue.hold(id, 'action', true);
  tick(20000);
  queue.restart(id);
  tick(10000);
  assert.equal(queue.channel.getToast()?.id, id);
  queue.hold(id, 'action', false);
  tick(5999); assert.equal(queue.channel.getToast()?.id, id);
  tick(1); assert.equal(queue.channel.getToast(), null);
  queue.dispose();
});
