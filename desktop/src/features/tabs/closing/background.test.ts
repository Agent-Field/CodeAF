import test from 'node:test';
import assert from 'node:assert/strict';
import type { WorldRow, AttentionItem } from '../../chat/world-client.ts';
import type { Tab } from '../model.ts';
import { buildBackgroundWork, failedListLimit, recentFailureMs, staleNotice } from './background.ts';

const NOW = Date.parse('2026-10-09T12:00:00Z');
const agoMs = (ms: number) => new Date(NOW - ms).toISOString();
const tab = (id: string, chat?: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...(chat ? { sessionFile: `/home/u/.codeaf/projects/p/${chat}/session.jsonl` } : {}), ...over });
const row = (session: string, over: Partial<WorldRow> = {}): WorldRow => ({ session, title: `T ${session}`, project: 'p', sourceFolders: [], state: 'idle', live: false, open: false, running: false, needsYou: false, failed: 0, tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 }, ...over });
const ask = (session: string): AttentionItem => ({ key: `${session}:question:1`, session, kind: 'question', text: 'Which branch?', sourceFolders: [], answerable: false });
const build = (over: Partial<Parameters<typeof buildBackgroundWork>[0]> = {}) => buildBackgroundWork({ tabs: [], closed: [], summaries: {}, since: {}, stopping: new Set(), world: { status: 'live', rows: [], items: [] }, seen: {}, now: NOW, ...over });

test('nothing known yields empty sections and no notice', () => {
  const out = build();
  assert.deepEqual([out.running, out.needsYou, out.failed, out.notice], [[], [], [], undefined]);
});

test('work running in a conversation with no tab here is listed from the feed, with no tab to open', () => {
  const out = build({ world: { status: 'live', rows: [row('c1', { running: true })], items: [] } });
  assert.deepEqual(out.running.map(i => [i.id, i.chatId, i.tabId, i.state]), [['chat:c1', 'c1', undefined, 'running']]);
});

test('a conversation open or closed here is described by its tab, never twice', () => {
  const world = { status: 'live' as const, rows: [row('c1', { running: true }), row('c2', { running: true })], items: [ask('c1')] };
  const out = build({ world, tabs: [tab('open', 'c1')], closed: [tab('gone', 'c2')] });
  // c1 is an open tab and c2 a closed one the feed has news of before any read: only the closed one is background work.
  assert.deepEqual(out.running.map(i => [i.id, i.tabId, i.state]), [['gone', 'gone', 'running']]);
  assert.deepEqual(out.needsYou, []);
});

test('a closed tab stopping leaves at once even though the feed still says running', () => {
  const out = build({ world: { status: 'live', rows: [row('c2', { running: true })], items: [] }, closed: [tab('gone', 'c2')], stopping: new Set(['gone']) });
  assert.deepEqual(out.running.map(i => i.id), ['chat:c2']);
});

test('a question in a conversation no tab shows is listed under Needs you', () => {
  const out = build({ world: { status: 'live', rows: [row('c9', { title: 'Release' })], items: [ask('c9')] } });
  assert.deepEqual(out.needsYou.map(i => [i.title, i.text, i.chatId]), [['Release', 'Which branch?', 'c9']]);
});

test('failures: unseen and recent only, newest first, capped, archived ones skipped', () => {
  const rows = [
    row('new', { failed: 2, at: agoMs(1000) }),
    row('old', { failed: 1, at: agoMs(recentFailureMs + 1000) }),
    row('seen', { failed: 1, at: agoMs(2000) }),
    row('gone', { failed: 1, at: agoMs(3000), archived: true }),
    row('mid', { failed: 3, at: agoMs(5000) }),
  ];
  const out = build({ world: { status: 'live', rows, items: [] }, seen: { seen: 1, mid: 2 } });
  assert.deepEqual(out.failed.map(i => [i.chatId, i.failed]), [['new', 2], ['mid', 3]]);
  const many = Array.from({ length: failedListLimit + 3 }, (_, n) => row(`c${n}`, { failed: 1, at: agoMs(1000 + n) }));
  assert.equal(build({ world: { status: 'live', rows: many, items: [] } }).failed.length, failedListLimit);
});

test('a failure carries its tab when this window has one, so a click can go there', () => {
  const out = build({ world: { status: 'live', rows: [row('c1', { failed: 1, at: agoMs(10) })], items: [] }, closed: [tab('gone', 'c1')] });
  assert.equal(out.failed[0].tabId, 'gone');
});

test('an unreachable feed marks what it contributed as stale and says so once; an unreachable feed that never answered says nothing', () => {
  const stale = build({ world: { status: 'unavailable', rows: [row('c1', { running: true })], items: [] } });
  assert.equal(stale.notice, staleNotice);
  assert.equal(stale.running[0].stale, true);
  assert.equal(build({ world: { status: 'unavailable', rows: [], items: [] } }).notice, undefined);
  assert.equal(build({ world: { status: 'live', rows: [row('c1', { running: true })], items: [] } }).running[0].stale, undefined);
});

test('failures: the engine\'s shared mark decides, a pending mark hides only the failure it names, and a seen failure hides nothing else', () => {
  const failure = { task: '2', at: '2026-10-02T10:00:00.25Z' };
  const rows = [
    row('shared', { failed: 2, unseenFailed: 0, failure, at: agoMs(1000) }),
    row('fresh', { failed: 2, unseenFailed: 1, failure, at: agoMs(2000) }),
    row('pend', { failed: 1, unseenFailed: 1, failure, at: agoMs(3000) }),
    row('later', { failed: 2, unseenFailed: 1, failure: { task: '3', at: '2026-10-05T00:00:00Z' }, at: agoMs(4000) }),
    row('busy', { failed: 1, unseenFailed: 0, running: true, live: true, at: agoMs(5000) }),
  ];
  const out = build({ world: { status: 'live', rows, items: [] }, seen: { shared: 0 }, pending: { pend: failure.at, later: failure.at } });
  assert.deepEqual(out.failed.map(i => [i.chatId, i.failed, i.failure?.at]), [['fresh', 1, failure.at], ['later', 1, '2026-10-05T00:00:00Z']]);
  assert.deepEqual(out.running.map(i => i.chatId), ['busy'], 'a seen failure does not hide running work');
});
