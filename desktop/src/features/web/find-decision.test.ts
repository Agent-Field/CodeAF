import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// Locks the find-in-page research so a later edit cannot drop the API names,
// the follow-up id, or the refusal to invent a current-match index.
const decisions = readFileSync(new URL('../../../docs/DECISIONS.md', import.meta.url), 'utf8');
const questions = readFileSync(new URL('../../../docs/DESIGN-QUESTIONS.md', import.meta.url), 'utf8');
const webQuestions = readFileSync(new URL('../../../docs/WEB-QUESTIONS.md', import.meta.url), 'utf8');

function section(markdown: string, heading: string): string {
  const start = markdown.indexOf(heading);
  assert.ok(start >= 0, heading);
  const rest = markdown.slice(start + heading.length);
  const next = rest.search(/\n## /);
  return next < 0 ? rest : rest.slice(0, next);
}

test('find-in-page names the public API on macOS and Linux and a native follow-up', () => {
  const body = section(decisions, '## t-d5-tab-web-find-research');
  assert.match(body, /findString:withConfiguration:completionHandler:/);
  assert.match(body, /macos\(11\.0\)/);
  assert.match(body, /matchFound/);
  assert.match(body, /WebKitFindController/);
  assert.match(body, /webkit_web_view_get_find_controller/);
  assert.match(body, /t-d5-nat-web-find/);
  assert.match(body, /web_find \{ pane, query, forward \}/);
  assert.match(body, /\{ found, matches\? \}/);
  assert.match(body, /no `current` index/);
  assert.match(body, /_findString/);
  assert.match(body, /with_webview/);
});

test('the open ledger and the web questions row carry the same command', () => {
  const row = questions.split('\n').find((line) => line.startsWith('| TW-14 |'));
  assert.ok(row, 'TW-14 row');
  assert.match(row, /t-d5-nat-web-find/);
  assert.match(row, /web_find \{ pane, query, forward \}/);
  assert.match(row, /n of m/);
  const w9 = webQuestions.split('\n').find((line) => line.startsWith('| W9 |'));
  assert.ok(w9, 'W9 row');
  assert.match(w9, /t-d5-nat-web-find/);
  assert.match(w9, /\{ found, matches\? \}/);
});
