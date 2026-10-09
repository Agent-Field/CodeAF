import test from 'node:test';
import assert from 'node:assert/strict';
import { linkSentences, resolveLink, type LinkEngine } from './openLink.ts';
import type { Pane, Tab } from '../types.ts';

const CHAT = '9446cc2627f3deae';
const SESSION = `/srv/store/projects/-srv-app/${CHAT}/transcript.jsonl`;
const tab = (over: Partial<Tab> = {}): Tab => ({ id: 't', kind: 'conversation', title: 'Fix the parser', draft: '', pinned: false, ...over });
const pane = (over: Partial<Pane> = {}): Pane => ({ id: 'p', kind: 'conversation', title: 'x', draft: '', ...over });
class Gone extends Error { status = 404; unreachable = false; }
class Down extends Error { status = 0; unreachable = true; }

type Calls = string[];
function engine(calls: Calls, over: Partial<LinkEngine> = {}): LinkEngine {
  return {
    async chat(id) { calls.push(`chat ${id}`); if (id !== CHAT) throw new Gone('no such conversation'); return { sessionFile: SESSION, title: 'Fix the parser', archived: false }; },
    async unarchive(id) { calls.push(`unarchive ${id}`); },
    async task(file, id) { calls.push(`task ${id}`); if (id !== 't-1') throw new Gone('task not found'); return { title: 'Write the tests' }; },
    async terminal(file, id) { calls.push(`terminal ${id}`); if (id !== 'abc') throw new Gone('terminal not found'); return { title: 'zsh' }; },
    ...over,
  };
}

test('a link to a conversation that already has a tab focuses it and asks the engine nothing', async () => {
  const calls: Calls = [];
  const tabs = [tab({ id: 'a' }), tab({ id: 'b', sessionFile: SESSION, route: { taskId: 't-9', back: [''], forward: [] } })];
  assert.deepEqual(await resolveLink(`codeaf://chat/${CHAT}`, () => tabs, engine(calls)), { kind: 'focus', id: 'b', link: `codeaf://chat/${CHAT}` });
  assert.deepEqual(calls, []);
});

test('a pane inside a split is focused rather than opened again', async () => {
  const split = tab({ id: 's', split: { layout: '1x2', focus: 0, panes: [pane({ id: 'left' }), pane({ id: 'right', kind: 'terminal', sessionFile: SESSION, target: { terminalId: 'abc' } })] } });
  const outcome = await resolveLink(`codeaf://terminal/${CHAT}/abc`, () => [split], engine([]));
  assert.deepEqual(outcome, { kind: 'focus', id: 'right', link: `codeaf://terminal/${CHAT}/abc` });
});

test('a conversation with no tab opens ONE tab on the transcript the engine names, with no message sent', async () => {
  const calls: Calls = [];
  const outcome = await resolveLink(`codeaf://chat/${CHAT}`, () => [tab()], engine(calls));
  assert.equal(outcome.kind, 'open');
  if (outcome.kind !== 'open') return;
  assert.equal(outcome.tab.kind, 'conversation');
  assert.equal(outcome.tab.sessionFile, SESSION);
  assert.equal(outcome.tab.title, 'Fix the parser');
  assert.equal(outcome.tab.draft, '');
  assert.deepEqual(calls, [`chat ${CHAT}`]);
});

test('an archived conversation is brought back, as Continue does', async () => {
  const calls: Calls = [];
  const e = engine(calls, { async chat() { calls.push('chat'); return { sessionFile: SESSION, title: '', archived: true }; } });
  const outcome = await resolveLink(`codeaf://chat/${CHAT}`, () => [], e);
  assert.equal(outcome.kind, 'open');
  assert.ok(calls.includes(`unarchive ${CHAT}`));
});

test('a task link opens the task view on that task', async () => {
  const outcome = await resolveLink(`codeaf://chat/${CHAT}/task/t-1`, () => [], engine([]));
  assert.equal(outcome.kind, 'open');
  if (outcome.kind !== 'open') return;
  assert.equal(outcome.tab.kind, 'task');
  assert.equal(outcome.tab.title, 'Write the tests');
  assert.deepEqual(outcome.tab.route, { taskId: 't-1', back: [''], forward: [] });
});

test('a file link opens a file tab that reads through the conversation; an existing one is focused', async () => {
  const outcome = await resolveLink(`codeaf://diff/${CHAT}?path=src/a.ts`, () => [], engine([]));
  assert.equal(outcome.kind, 'open');
  if (outcome.kind !== 'open') return;
  assert.equal(outcome.tab.kind, 'diff');
  assert.deepEqual(outcome.tab.file, { path: 'src/a.ts', view: 'changes' });
  assert.equal(outcome.tab.sessionFile, SESSION);
  const again = await resolveLink(`codeaf://diff/${CHAT}?path=src/a.ts`, () => [outcome.tab], engine([]));
  assert.equal(again.kind, 'focus');
});

test('a live terminal opens on its own id and no terminal is started', async () => {
  const calls: Calls = [];
  const outcome = await resolveLink(`codeaf://terminal/${CHAT}/abc`, () => [], engine(calls));
  assert.equal(outcome.kind, 'open');
  if (outcome.kind !== 'open') return;
  assert.equal(outcome.tab.target?.terminalId, 'abc');
  assert.equal(outcome.tab.target?.sessionId, undefined);
  assert.deepEqual(calls, [`chat ${CHAT}`, 'terminal abc']);
});

test('an expired terminal, a missing task and an unknown conversation are said, and nothing opens', async () => {
  assert.deepEqual(await resolveLink(`codeaf://terminal/${CHAT}/old`, () => [], engine([])), { kind: 'refused', sentence: linkSentences.terminalGone, link: `codeaf://terminal/${CHAT}/old` });
  assert.deepEqual(await resolveLink(`codeaf://chat/${CHAT}/task/nope`, () => [], engine([])), { kind: 'refused', sentence: linkSentences.taskGone, link: `codeaf://chat/${CHAT}/task/nope` });
  assert.deepEqual(await resolveLink('codeaf://chat/0000000000000000', () => [], engine([])), { kind: 'refused', sentence: linkSentences.chatGone, link: 'codeaf://chat/0000000000000000' });
});

test('an engine that cannot be reached is said as that, not as a missing conversation', async () => {
  const outcome = await resolveLink(`codeaf://chat/${CHAT}`, () => [], engine([], { async chat() { throw new Down('codeaf engine is not running'); } }));
  assert.deepEqual(outcome, { kind: 'refused', sentence: linkSentences.unreachable, link: `codeaf://chat/${CHAT}` });
});

test('a malformed, unknown or climbing link asks the engine nothing', async () => {
  const calls: Calls = [];
  for (const raw of ['codeaf://chat/%E0%A4%A', 'codeaf://settings', `codeaf://file/${CHAT}?path=../../etc/passwd`, 'javascript:alert(1)']) {
    const outcome = await resolveLink(raw, () => [], engine(calls));
    assert.equal(outcome.kind, 'refused', raw);
  }
  assert.deepEqual(calls, []);
});

test('a tab that appears while the engine answers is focused, not doubled', async () => {
  let tabs: Tab[] = [];
  const e = engine([], { async chat() { tabs = [tab({ id: 'late', sessionFile: SESSION })]; return { sessionFile: SESSION, title: '', archived: false }; } });
  assert.deepEqual(await resolveLink(`codeaf://chat/${CHAT}`, () => tabs, e), { kind: 'focus', id: 'late', link: `codeaf://chat/${CHAT}` });
});
