import test from 'node:test';
import assert from 'node:assert/strict';
import { withoutHandoff } from './handoff.ts';
import { projectTurnsV2 } from './project.ts';
import { final, snap, tool, user } from './testkit.ts';

// The two notes exactly as internal/session/readhandoff.go writes them.
const HANDED =
  "[This reading was handed to quick task 1, which finished it and returned the distilled answer below — the raw reads never entered this conversation. If you need one file's exact text to defend the answer, read that one file directly.]";
const COVERED = '[Covered by the handoff to quick task 1 — the distilled answer is on the first call of this batch.]';
const ANSWER = "What I've learned: README.md says the folder is a scratch workspace.";

test('the handed-off answer loses its note and keeps the answer', () => {
  assert.deepEqual(withoutHandoff(`${HANDED}\n\n${ANSWER}`), { output: ANSWER, covered: false });
});

test('a covered call is empty and says it was covered', () => {
  assert.deepEqual(withoutHandoff(COVERED), { output: '', covered: true });
});

test('file text that merely starts with a bracket is left alone', () => {
  for (const text of ['[section]\nkey = 1', '[This is a markdown note]', '']) {
    assert.deepEqual(withoutHandoff(text), { output: text, covered: false });
  }
});

test('a recorded read batch projects to clean calls', () => {
  const entries = [
    user('summarise the repo'),
    tool('read', 'c1', { path: '/w/README.md' }, { Hint: 'handed to quick task 1', Output: `${HANDED}\n\n${ANSWER}` }),
    tool('ls', 'c2', { path: '/w/notes' }, { Hint: 'handed to quick task 1', Output: COVERED }),
    final('done'),
  ];
  const { turns } = projectTurnsV2(snap(entries));
  const work = turns[0].blocks.find((b) => b.kind === 'work');
  assert.ok(work && work.kind === 'work');
  const [read, ls] = work.steps.flatMap((s) => s.calls);
  assert.equal(read.output, ANSWER);
  assert.equal(read.covered, undefined);
  assert.equal(ls.output, '');
  assert.equal(ls.covered, true);
});
