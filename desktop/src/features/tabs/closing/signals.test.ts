import test from 'node:test';
import assert from 'node:assert/strict';
import type { AttentionItem, WorldRow } from '../../chat/world-client.ts';
import { recentFailureMs } from './background.ts';
import { attentionSignals, failureNoticeId, noticeFailureLimit, noticeQuestionLimit } from './signals.ts';

const NOW = Date.parse('2026-10-09T12:00:00Z');
const ago = (ms: number) => new Date(NOW - ms).toISOString();
const row = (session: string, over: Partial<WorldRow> = {}): WorldRow => ({ session, title: `T ${session}`, project: 'p', sourceFolders: [], state: 'idle', live: false, open: false, running: false, needsYou: false, failed: 0, tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 }, at: ago(60_000), ...over });
const failing = (session: string, landed: string, over: Partial<WorldRow> = {}) => row(session, { failed: 2, unseenFailed: 1, failure: { task: 't1', at: landed }, ...over });
const asking = (session: string, id: number): AttentionItem => ({ key: `${session}:consent:${id}`, session, kind: 'consent', id, text: 'Run it?', sourceFolders: [], answerable: true });
const live = (rows: WorldRow[], items: AttentionItem[] = []) => ({ status: 'live' as const, rows, items });
const ids = (list: { id: string }[]) => list.map(item => item.id);

test('a failure is named by the engine, so two windows reading the same feed post the same list', () => {
  const world = live([failing('aaaa000000000001', '2026-10-09T11:00:00Z', { at: '2026-10-09T11:00:00Z' }), failing('aaaa000000000002', '2026-10-09T11:30:00Z', { at: '2026-10-09T11:30:00Z' })]);
  // Window A keeps an old count record for one failure; window B has none. The engine's own fact decides for both.
  const a = attentionSignals(world, { aaaa000000000001: 9 }, NOW);
  const b = attentionSignals(world, {}, NOW);
  assert.deepEqual(a, b);
  assert.deepEqual(ids(a), ['failed:aaaa000000000002:2026-10-09T11:30:00Z', 'failed:aaaa000000000001:2026-10-09T11:00:00Z']);
  assert.deepEqual(a[0], { id: 'failed:aaaa000000000002:2026-10-09T11:30:00Z', kind: 'failed', chatTitle: 'T aaaa000000000002', text: 'A task failed', chatId: 'aaaa000000000002' });
});

test('a failure leaves the list only when the engine says it was seen, and a newer one is a new notification', () => {
  const before = attentionSignals(live([failing('aaaa000000000001', '2026-10-09T11:00:00Z')]), {}, NOW);
  const seen = attentionSignals(live([failing('aaaa000000000001', '2026-10-09T11:00:00Z', { unseenFailed: 0 })]), {}, NOW);
  const again = attentionSignals(live([failing('aaaa000000000001', '2026-10-09T11:40:00Z', { failed: 3 })]), {}, NOW);
  assert.equal(before.length, 1);
  assert.deepEqual(seen, []);
  assert.notEqual(again[0].id, before[0].id);
});

test('an engine without the seen mark falls back to this window and names the failure by its count', () => {
  const old = row('aaaa000000000001', { failed: 2 });
  assert.deepEqual(ids(attentionSignals(live([old]), {}, NOW)), ['failed:aaaa000000000001:#2']);
  assert.deepEqual(attentionSignals(live([old]), { aaaa000000000001: 2 }, NOW), [], 'this window saw both');
  assert.equal(failureNoticeId(old), 'failed:aaaa000000000001:#2');
});

test('old, archived and deleted failures are not announced, and nothing is announced off a feed that is not live', () => {
  const rows = [
    failing('aaaa000000000001', ago(0), { at: ago(recentFailureMs + 1) }),
    failing('aaaa000000000002', ago(0), { archived: true }),
    failing('aaaa000000000003', ago(0), { deletionPending: true }),
  ];
  assert.deepEqual(attentionSignals(live(rows), {}, NOW), []);
  assert.deepEqual(attentionSignals({ status: 'unavailable', rows: [failing('aaaa000000000004', ago(0))], items: [asking('aaaa000000000004', 1)] }, {}, NOW), []);
});

test('the list stays within what Rust accepts, and stays within what Rust accepts', () => {
  const rows = Array.from({ length: noticeFailureLimit + 10 }, (_, i) => failing(`bbbb${String(i).padStart(12, '0')}`, ago(i), { at: ago(i * 1000) }));
  const items = Array.from({ length: noticeQuestionLimit + 10 }, (_, i) => asking('aaaa000000000001', i + 1));
  const list = attentionSignals(live(rows, items), {}, NOW);
  assert.equal(list.filter(item => item.kind === 'failed').length, noticeFailureLimit);
  assert.equal(list.filter(item => item.kind === 'needsYou').length, noticeQuestionLimit);
  assert.ok(list.length <= 256);
  assert.equal(list.find(item => item.kind === 'failed')?.id.startsWith('failed:bbbb000000000000:'), true, 'newest first');
});

test('a question names its conversation and the exact question a click focuses', () => {
  const [first] = attentionSignals(live([row('aaaa000000000001', { title: 'Fix the parser' })], [asking('aaaa000000000001', 7)]), {}, NOW);
  assert.deepEqual(first, { id: 'aaaa000000000001:consent:7', kind: 'needsYou', chatTitle: 'Fix the parser', text: 'Run it?', chatId: 'aaaa000000000001', question: { kind: 'consent', id: 7 } });
});
