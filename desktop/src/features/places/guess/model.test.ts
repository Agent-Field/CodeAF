import { canFileChoice, filingOptions } from './filing.ts';
import test from 'node:test';
import assert from 'node:assert/strict';
import { alsoIn, readGuess, readLenses, steerMessage, NOT_UNDERSTOOD } from './model.ts';
import { createGuessController } from './controller.ts';

const answer = { place: 'place-a', name: 'Desk app', confidence: 80, others: ['place-b'] };
function fixture() {
  let now = 0, next = 0;
  const jobs = new Map<number, { at: number; fn: () => void }>();
  const time = { now: () => now, later: (fn: () => void, ms: number) => { jobs.set(++next, { at: now + ms, fn }); return next; }, cancel: (id: unknown) => { jobs.delete(id as number); } };
  const advance = (ms: number) => {
    const end = now + ms;
    while (true) {
      const due = [...jobs.entries()].filter(([, job]) => job.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
      if (!due) break;
      now = due[1].at; jobs.delete(due[0]); due[1].fn();
    }
    now = end;
  };
  return { time, advance };
}
const draft = (text: string) => ({ draft: text, workspace: '', chatId: '' });
const flush = async () => { await Promise.resolve(); await Promise.resolve(); };

test('300 ms debounce sends only the latest draft and empty drafts send nothing', async () => {
  const { time, advance } = fixture(); const asked: string[] = []; const shown: unknown[] = [];
  const controller = createGuessController(async ask => { asked.push(ask.draft); return answer; }, value => shown.push(value), time);
  controller.update(draft('a')); advance(299); assert.deepEqual(asked, []);
  controller.update(draft('ab')); advance(299); assert.deepEqual(asked, []);
  advance(1); await flush(); assert.deepEqual(asked, ['ab']); assert.deepEqual(shown.at(-1), answer);
  controller.update(draft('')); advance(500); assert.deepEqual(asked, ['ab']); assert.equal(shown.at(-1), undefined);
});
test('an edit immediately hides the pill and an obsolete response cannot return', async () => {
  const { time, advance } = fixture(); const shown: unknown[] = []; let resolve!: (answer: unknown) => void;
  const controller = createGuessController(() => new Promise(done => { resolve = done; }), value => shown.push(value), time);
  controller.update(draft('first')); advance(300); controller.update(draft('second')); resolve(answer); await flush();
  assert.equal(shown.at(-1), undefined); controller.dispose(); advance(500); assert.equal(shown.at(-1), undefined);
});
test('a 20 ms budget breach hides the pill even when a transport ignores cancellation', async () => {
  const { time, advance } = fixture(); const shown: unknown[] = []; let resolve!: (answer: unknown) => void; let signal!: AbortSignal;
  const controller = createGuessController((_ask, abort) => { signal = abort; return new Promise(done => { resolve = done; }); }, value => shown.push(value), time);
  controller.update(draft('first')); advance(300); advance(21); assert.ok(signal.aborted); resolve(answer); await flush(); assert.equal(shown.at(-1), undefined);
});
test('valid, absent, malformed and failed guesses obey the emptiness law', async () => {
  assert.deepEqual(readGuess(answer), answer);
  for (const value of [null, {}, { ...answer, place: '' }, { ...answer, confidence: 101 }, { ...answer, confidence: '80' }, { ...answer, others: ['a', 'b', 'c'] }]) assert.equal(readGuess(value), undefined);
  const { time, advance } = fixture(); let shown: unknown = answer;
  const controller = createGuessController(async () => { throw new Error('offline'); }, value => { shown = value; }, time);
  controller.update(draft('first')); advance(300); await flush(); assert.equal(shown, undefined);
});
test('views use engine chat ids and a refused steer uses the exact reply', () => {
  const lenses = [{ name: 'by month', axis: 'month', value: '2026-10', chats: ['chat-a'] }];
  assert.deepEqual(readLenses({ lenses }), lenses); assert.throws(() => readLenses({ lenses: [{ ...lenses[0], chats: [1] }] }));
  assert.equal(steerMessage({ understood: false }), NOT_UNDERSTOOD); assert.equal(steerMessage({ understood: true }), ''); assert.throws(() => steerMessage({}));
});
test('only a real shortcut membership shows also in; primary and unknown rows show nothing', () => {
  assert.equal(alsoIn({ link: true, places: [{ name: 'Desk app', link: false }] }, 'other'), 'also in Desk app');
  assert.equal(alsoIn({ link: true }, 'other'), 'also in'); assert.equal(alsoIn({}), undefined); assert.equal(alsoIn({ link: false }), undefined);
});

test('a chosen place cannot file while typing, sending, streaming or interrupted', () => {
  assert.equal(canFileChoice(undefined), false);
  assert.equal(canFileChoice({ running: false, entries: [] }), false);
  assert.equal(canFileChoice({ running: true, entries: [{ Role: 'assistant', Answer: true }] }), false);
  assert.equal(canFileChoice({ running: false, entries: [{ Role: 'assistant', Answer: true, Interrupted: true }] }), false);
  assert.equal(canFileChoice({ running: false, entries: [{ Role: 'assistant', Answer: true }] }), true);
  assert.deepEqual(filingOptions([{ id: 'shortcut', link: true }, { id: 'old' }], 'chosen'), { addedBy: 'you', moveFrom: 'old' });
  assert.deepEqual(filingOptions([{ id: 'chosen' }], 'chosen'), { addedBy: 'you' });
  assert.deepEqual(filingOptions([], 'chosen'), { addedBy: 'you' });
});
