import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { createToasts } from '../../design/toasts.ts';
import type { AttentionItem, WorldTransport } from '../chat/world-client.ts';
import { createWorldStore } from '../chat/world-store.ts';
import {
  ACCEPT_WINDOW_MS,
  acceptFailureLine,
  acceptedSuggestionsText,
  answerSuggestion,
  createNextUp,
  suggestionAnswer,
  suggestionAnswerPath,
} from './useNextUp.ts';

const suggestion = { key: 'use-a', label: 'Use A', percent: 92, reason: 'matches the last answer' };

function item(key: string, over: Partial<AttentionItem> = {}): AttentionItem {
  return {
    key,
    session: key,
    kind: 'ask',
    id: 1,
    text: key,
    sourceFolders: [],
    answerable: true,
    stakes: 'reversible',
    suggestion,
    blocking: { turn: false, tasks: [] },
    asked: '2026-10-10T00:00:00.000Z',
    ...over,
    key,
  };
}

function frame(seq: number, items: AttentionItem[]) {
  return `data: ${JSON.stringify({ seq, type: 'attention', at: 't', payload: { items } })}\n\n`;
}

/** A world store whose stream the test feeds by hand. The same shape the window uses. */
function mockWorld() {
  const calls: { push: (text: string) => void }[] = [];
  const encoder = new TextEncoder();
  const transport: WorldTransport = async (path, init) => {
    let controller!: ReadableStreamDefaultController<Uint8Array>;
    const body = new ReadableStream<Uint8Array>({ start(c) { controller = c; } });
    calls.push({ push: (text: string) => controller.enqueue(encoder.encode(text)) });
    init.signal?.addEventListener('abort', () => { try { controller.error(new DOMException('aborted', 'AbortError')); } catch { /* already closed */ } });
    assert.equal(path.startsWith('/events?after='), true);
    return new Response(body, { status: 200, headers: { 'content-type': 'text/event-stream' } });
  };
  return { world: createWorldStore({ transport }), calls };
}

const tick = () => new Promise(resolve => setTimeout(resolve, 10));

type Posted = { session: string; key: string; id: number; kind: string };

function harness() {
  const posted: Posted[] = [];
  let fail = new Set<string>();
  const world = mockWorld();
  const channel = createToasts();
  const next = createNextUp({
    world: world.world,
    channel,
    answer: async (session, answer) => {
      posted.push({ session, key: answer.key, id: answer.id, kind: answer.kind });
      if (fail.has(session)) throw new Error('this question is no longer waiting');
    },
  });
  const stop = next.subscribe(() => {});
  return {
    ...world,
    posted,
    channel,
    next,
    stop,
    fail(session: string) { fail.add(session); },
  };
}

async function feed(box: ReturnType<typeof harness>, items: AttentionItem[], seq = 1) {
  await tick();
  box.calls[0].push(frame(seq, items));
  await tick();
}

test('the accept window is the shared 6s toast', () => {
  const tokens = JSON.parse(readFileSync(new URL('../../design/tokens.json', import.meta.url), 'utf8')) as { interaction: { toastDuration: number } };
  assert.equal(tokens.interaction.toastDuration, 6000);
  assert.equal(ACCEPT_WINDOW_MS, tokens.interaction.toastDuration);
  assert.equal(acceptedSuggestionsText(1), 'Accepted 1 suggestion');
  assert.equal(acceptedSuggestionsText(3), 'Accepted 3 suggestions');
  assert.equal(acceptFailureLine([]), '');
  assert.equal(acceptFailureLine([{ text: 'Allow 3 git actions?' }, { text: 'Skip 4 and continue?' }]), 'Could not accept: Allow 3 git actions? · Skip 4 and continue?');
});

test('the queue follows the mock world and leaves out the conversation on screen', async () => {
  const box = harness();
  await feed(box, [
    item('here-q', { session: 'here', text: 'Allow git?' }),
    item('away', { session: 'away', text: 'Use the lexer?', suggestion: { ...suggestion, key: 'lexer' } }),
    item('costly', { session: 'costly', stakes: 'costly', text: 'Spend the budget?' }),
    item('irrev', { session: 'irrev', stakes: 'irreversible', text: 'Delete the branch?' }),
  ]);
  const here = box.next.view('here');
  assert.deepEqual(here.queue.items.map(row => row.key), ['away', 'costly', 'irrev']);
  assert.deepEqual(here.queue.acceptable.map(row => row.key), ['away']);
  assert.equal(here.failureLine, '');
  assert.equal(here.pendingAccept, false);
  box.stop();
});

test('skip is per window and never calls the engine', async () => {
  const posted: string[] = [];
  const world = mockWorld();
  const a = createNextUp({ world: world.world, channel: createToasts(), answer: async session => { posted.push(session); } });
  const b = createNextUp({ world: world.world, channel: createToasts(), answer: async session => { posted.push(session); } });
  const stopA = a.subscribe(() => {});
  const stopB = b.subscribe(() => {});
  await tick();
  world.calls[0].push(frame(1, [
    item('a', { asked: '2026-10-10T00:00:00.000Z' }),
    item('b', { asked: '2026-10-10T00:01:00.000Z' }),
    item('c', { asked: '2026-10-10T00:02:00.000Z' }),
  ]));
  await tick();
  a.skip('');
  a.skip('');
  assert.deepEqual(a.view('').queue.items.map(row => row.key), ['c', 'a', 'b']);
  assert.deepEqual(a.view('').skipped, ['a', 'b']);
  assert.equal(a.view('').queue.count, 3);
  assert.deepEqual(b.view('').queue.items.map(row => row.key), ['a', 'b', 'c']);
  assert.deepEqual(b.view('').skipped, []);
  assert.deepEqual(posted, []);
  stopA();
  stopB();
});

test('accept answers each suggestion only after the toast settles', async () => {
  const box = harness();
  await feed(box, [
    item('keep', { session: 'keep', id: 4, kind: 'consent', text: 'Allow 3 git actions?', suggestion: { ...suggestion, key: 'allow' } }),
    item('other', { session: 'other', id: 9, kind: 'choice', text: 'Use the shared lexer?', suggestion: { ...suggestion, key: 'lexer' } }),
    item('here', { session: 'here', text: 'In this chat?' }),
    item('costly', { session: 'costly', stakes: 'costly', text: 'Spend it?' }),
    item('bare', { session: 'bare', suggestion: undefined, text: 'No suggestion' }),
  ]);
  box.next.accept('here');
  const toast = box.channel.getToast();
  assert.equal(toast?.message[0], 'Accepted 2 suggestions');
  assert.equal(toast?.durationMs, ACCEPT_WINDOW_MS);
  assert.equal(typeof toast?.undo, 'function');
  assert.equal(box.next.view('here').pendingAccept, true);
  assert.deepEqual(box.posted, []);
  box.channel.dismiss(toast!.id);
  await tick();
  assert.deepEqual(box.posted, [
    { session: 'keep', key: 'allow', id: 4, kind: 'consent' },
    { session: 'other', key: 'lexer', id: 9, kind: 'choice' },
  ]);
  assert.equal(box.next.view('here').pendingAccept, false);
  assert.equal(box.next.view('here').failureLine, '');
  assert.equal(box.channel.getToasts().length, 0);
  box.stop();
});

test('undo within the window cancels every answer and does not call the engine', async () => {
  const box = harness();
  await feed(box, [
    item('one', { session: 'one', text: 'Allow git?' }),
    item('two', { session: 'two', text: 'Use the lexer?', suggestion: { ...suggestion, key: 'lexer' } }),
  ]);
  box.next.accept('');
  const toast = box.channel.getToast();
  assert.ok(toast);
  await box.channel.undo(toast.id);
  assert.equal(box.channel.getToast(), null);
  assert.equal(box.next.view('').pendingAccept, false);
  box.channel.dismiss(toast.id);
  await tick();
  assert.deepEqual(box.posted, []);
  box.stop();
});

test('a second accept replaces the pending batch, and the first is not posted', async () => {
  const box = harness();
  await feed(box, [item('first', { session: 'first', text: 'First?', suggestion: { ...suggestion, key: 'first-key' } })]);
  box.next.accept('');
  assert.equal(box.channel.getToast()?.message[0], 'Accepted 1 suggestion');
  await feed(box, [item('second', { session: 'second', id: 8, text: 'Second?', suggestion: { ...suggestion, key: 'second-key' } })], 2);
  box.next.accept('');
  const toast = box.channel.getToast();
  assert.equal(toast?.message[0], 'Accepted 1 suggestion');
  assert.equal(box.channel.getToasts().length, 1);
  box.channel.dismiss(toast!.id);
  await tick();
  assert.deepEqual(box.posted, [{ session: 'second', key: 'second-key', id: 8, kind: 'ask' }]);
  box.stop();
});

test('after commit, failures are one muted line and successes are not listed', async () => {
  const box = harness();
  box.fail('bad');
  await feed(box, [
    item('good', { session: 'good', text: 'Allow 3 git actions?' }),
    item('bad', { session: 'bad', text: 'Skip 4 and continue?' }),
    item('also', { session: 'also-bad', text: 'Keep the old schema?' }),
  ]);
  box.fail('also-bad');
  box.next.accept('');
  box.channel.dismiss(box.channel.getToast()!.id);
  await tick();
  const line = 'Could not accept: Skip 4 and continue? · Keep the old schema?';
  assert.equal(box.next.view('').failureLine, line);
  const toast = box.channel.getToast();
  assert.equal(toast?.message[0], line);
  assert.equal(toast?.tone, 'info');
  assert.equal(toast?.undo, undefined);
  assert.equal(box.channel.getToasts().length, 1);
  assert.deepEqual(box.posted.map(row => row.session), ['good', 'bad', 'also-bad']);
  box.stop();
});

test('accept with nothing acceptable shows no toast and posts nothing', async () => {
  const box = harness();
  await feed(box, [item('irrev', { stakes: 'irreversible', text: 'Delete the branch?' })]);
  box.next.accept('');
  assert.equal(box.channel.getToast(), null);
  await tick();
  assert.deepEqual(box.posted, []);
  box.stop();
});

test('a suggestion answer is posted to the session answer endpoint', async () => {
  const body = suggestionAnswer(item('q', { session: 'sess/1', id: 3, kind: 'consent', suggestion: { ...suggestion, key: 'allow' } }));
  assert.deepEqual(body, { kind: 'consent', id: 3, key: 'allow', picked: ['allow'], decidedBy: 'person' });
  assert.equal(suggestionAnswerPath('sess/1'), '/sessions/sess%2F1/answer');
  const seen: { path: string; body: string; method: string }[] = [];
  const post = async (path: string, init: { method: 'POST'; body: string }) => {
    seen.push({ path, body: init.body, method: init.method });
    return new Response(JSON.stringify({ accepted: true }), { status: 200, headers: { 'content-type': 'application/json' } });
  };
  await answerSuggestion('sess/1', body!, post);
  assert.deepEqual(seen, [{ path: '/sessions/sess%2F1/answer', method: 'POST', body: JSON.stringify(body) }]);
  const refused = async () => new Response(JSON.stringify({ error: 'this question is no longer waiting' }), { status: 409, headers: { 'content-type': 'application/json' } });
  await assert.rejects(answerSuggestion('sess/1', body!, refused), /this question is no longer waiting/);
  const blank = async () => new Response(JSON.stringify({ accepted: false }), { status: 200, headers: { 'content-type': 'application/json' } });
  await assert.rejects(answerSuggestion('sess/1', body!, blank), /did not accept/);
});
