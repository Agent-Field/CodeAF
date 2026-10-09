import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { linkForPane, linkForTab, linkOf, parseDeepLink, webLinkOf } from './deepLinks.ts';
import type { Pane, Tab } from '../types.ts';

type Cases = { valid: { raw: string; canonical: string; target: unknown }[]; invalid: { raw: string; reason: string }[] };
const cases = JSON.parse(readFileSync(new URL('./link-cases.json', import.meta.url), 'utf8')) as Cases;

const SESSION = '/home/someone/.codeaf/v3/projects/-home-someone-app/9446cc2627f3deae/transcript.jsonl';
const pane = (over: Partial<Pane> = {}): Pane => ({ id: 'p', kind: 'conversation', title: 'Fix the parser', draft: '', ...over });
const tab = (over: Partial<Tab> = {}): Tab => ({ ...pane(), pinned: false, ...over });

test('every valid corpus link parses to its target and its one canonical spelling', () => {
  for (const { raw, canonical, target } of cases.valid) {
    const parsed = parseDeepLink(raw);
    assert.deepEqual(parsed, { ok: true, target, canonical }, raw);
    assert.equal(linkOf(target as never), canonical, raw);
  }
});

test('every invalid corpus link is refused for its stated reason', () => {
  for (const { raw, reason } of cases.invalid) assert.deepEqual(parseDeepLink(raw), { ok: false, reason }, JSON.stringify(raw));
});

test('a link longer than the limit and anything that is not a string are refused', () => {
  assert.deepEqual(parseDeepLink(`codeaf://file/9446cc2627f3deae?path=${'a/'.repeat(4100)}b`), { ok: false, reason: 'malformed' });
  assert.deepEqual(parseDeepLink(undefined), { ok: false, reason: 'malformed' });
  assert.deepEqual(parseDeepLink({ kind: 'chat' }), { ok: false, reason: 'malformed' });
});

test('a saved conversation copies its chat id, never its transcript path', () => {
  const link = linkForPane(pane({ sessionFile: SESSION }));
  assert.equal(link, 'codeaf://chat/9446cc2627f3deae');
  assert.ok(!link!.includes('home'));
});

test('a conversation that was never sent, a new tab, settings, history and the Inbox have no link', () => {
  for (const kind of ['conversation', 'newtab', 'settings', 'history', 'inbox'] as const) assert.equal(linkForPane(pane({ kind })), undefined, kind);
  assert.equal(linkForTab(tab({ kind: 'inbox', sessionFile: SESSION })), undefined);
});

test('a task tab and a conversation showing a task copy the task; the expanded tasks view copies the chat', () => {
  assert.equal(linkForPane(pane({ kind: 'task', sessionFile: SESSION, route: { taskId: 'plan:3.1', back: [''], forward: [] } })), 'codeaf://chat/9446cc2627f3deae/task/plan%3A3.1');
  assert.equal(linkForPane(pane({ sessionFile: SESSION, route: { taskId: 't-1', back: [''], forward: [] } })), 'codeaf://chat/9446cc2627f3deae/task/t-1');
  assert.equal(linkForPane(pane({ sessionFile: SESSION, route: { taskId: '#tasks', back: [''], forward: [] } })), 'codeaf://chat/9446cc2627f3deae');
});

test('file and diff tabs copy a workspace-relative path; an absolute or climbing path is not a link', () => {
  assert.equal(linkForPane(pane({ kind: 'file', sessionFile: SESSION, file: { path: 'src/a b.ts', view: 'file' } })), 'codeaf://file/9446cc2627f3deae?path=src/a%20b.ts');
  assert.equal(linkForPane(pane({ kind: 'diff', sessionFile: SESSION, file: { path: 'go.mod', view: 'changes' } })), 'codeaf://diff/9446cc2627f3deae?path=go.mod');
  assert.equal(linkForPane(pane({ kind: 'file', sessionFile: SESSION, file: { path: '/etc/passwd' } })), undefined);
  assert.equal(linkForPane(pane({ kind: 'file', sessionFile: SESSION, file: { path: '../up' } })), undefined);
  // A file opened from the new-tab field without a conversation has nothing to read it through.
  assert.equal(linkForPane(pane({ kind: 'file', path: 'src/a.ts' })), undefined);
});

test('a terminal copies its engine terminal id, never the bridge session id', () => {
  const link = linkForPane(pane({ kind: 'terminal', sessionFile: SESSION, target: { terminalId: '0f1e2d3c4b5a6978', sessionId: 'bridge-secret-ish' } }));
  assert.equal(link, 'codeaf://terminal/9446cc2627f3deae/0f1e2d3c4b5a6978');
  assert.ok(!link!.includes('bridge'));
  assert.equal(linkForPane(pane({ kind: 'terminal', sessionFile: SESSION })), undefined);
});

test('a web tab copies its own normalized address and never one with credentials or another scheme', () => {
  assert.equal(linkForPane(pane({ kind: 'web', target: { url: 'https://Example.com/a b?q=1#top' } })), 'https://example.com/a%20b?q=1#top');
  assert.equal(webLinkOf('http://localhost:5173/'), 'http://localhost:5173/');
  assert.equal(webLinkOf('https://user:pw@example.com/'), undefined);
  assert.equal(webLinkOf('javascript:alert(1)'), undefined);
  assert.equal(webLinkOf('file:///etc/passwd'), undefined);
  assert.equal(webLinkOf(undefined), undefined);
});

test('a split copies the focused pane', () => {
  const split = tab({ split: { layout: '1x2', focus: 1, panes: [pane({ id: 'a' }), pane({ id: 'b', kind: 'web', target: { url: 'https://example.com/' } })] } });
  assert.equal(linkForTab(split), 'https://example.com/');
});

test('every link a pane copies parses back to the same link', () => {
  const panes = [
    pane({ sessionFile: SESSION }),
    pane({ kind: 'task', sessionFile: SESSION, route: { taskId: 'a b/c?', back: [], forward: [] } }),
    pane({ kind: 'file', sessionFile: SESSION, file: { path: 'dir/ünïcode & more?.md' } }),
    pane({ kind: 'terminal', sessionFile: SESSION, target: { terminalId: 'abc' } }),
  ];
  for (const p of panes) {
    const link = linkForPane(p);
    if (!link) continue; // a task id with a slash is not a canonical id, so it has no link
    const parsed = parseDeepLink(link);
    assert.ok(parsed.ok, link);
    assert.equal(parsed.ok && parsed.canonical, link);
  }
});
