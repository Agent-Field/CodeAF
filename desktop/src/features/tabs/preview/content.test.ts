import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineQuestion } from '../../chat/engine-client.ts';
import type { TabSummary } from '../../conversation/tabSummary.ts';
import { askOf, changedLines, headLines, stateOf, tailLines } from './content.ts';

const consent = (id: number, head: string): EngineQuestion => ({ id, kind: 'consent', ask: 'permission', head, options: [{ key: 'y', label: 'allow' }, { key: 'n', label: 'deny', safe: true }] });
const summary = (over: Partial<TabSummary> = {}): TabSummary => ({ title: '', firstLine: '', digest: '', ...over });

test('terminal tail keeps the last three lines without escapes and marks the newest', () => {
  const lines = tailLines('\u001b[32mone\u001b[0m\ntwo\n\nthree\nspin 10%\rspin 100%\nfour\n');
  assert.deepEqual(lines, [{ text: 'three' }, { text: 'spin 100%' }, { text: 'four', tone: 'ink' }]);
  assert.deepEqual(tailLines(''), []);
});

test('file head skips blank lines', () => {
  assert.deepEqual(headLines('\npackage parse\n\nimport "x"\nvar a\nvar b').map(l => l.text), ['package parse', 'import "x"', 'var a']);
});

test('diff head lists the first changed lines, removed before added as written', () => {
  const diff = { hunks: [{ header: '@@', oldStart: 1, oldLines: 2, newStart: 1, newLines: 2, lines: [
    { kind: 'context' as const, old: 1, new: 1, text: 'keep' },
    { kind: 'del' as const, old: 2, text: ' return Token{Kind: Comma}' },
    { kind: 'add' as const, new: 2, text: ' if l.peekClose() {' },
    { kind: 'add' as const, new: 3, text: ' third' },
  ] }] };
  assert.deepEqual(changedLines(diff), [{ text: '− return Token{Kind: Comma}', tone: 'del' }, { text: '+ if l.peekClose() {', tone: 'add' }]);
});

test('a set of permissions reads as one batch and may be allowed together', () => {
  assert.equal(askOf(undefined), undefined);
  assert.equal(askOf([]), undefined);
  assert.deepEqual(askOf([consent(1, 'a'), consent(2, 'b'), consent(3, 'c')]), { text: 'Allow 3 actions?', permissions: true, count: 3 });
  assert.deepEqual(askOf([consent(1, 'Run `go test`')]), { text: 'Run `go test`', permissions: true, count: 1 });
});

test('a choice is never offered as Allow all', () => {
  const choice: EngineQuestion = { id: 5, kind: 'choice', ask: 'choice', head: 'Which branch?', options: [{ key: 'a', label: 'main' }] };
  assert.equal(askOf([choice])?.permissions, false);
  assert.equal(askOf([consent(1, 'x'), choice])?.permissions, false);
  assert.equal(askOf([choice, consent(1, 'x')])?.text, 'Which branch? and 1 more');
});

test('card state: needs you and failed lead with a dot, running counts tasks, a quiet tab says nothing', () => {
  assert.deepEqual(stateOf(summary({ mark: 'waiting' })), { lead: 'amber', words: 'Needs you' });
  assert.deepEqual(stateOf(summary({ mark: 'failed' })), { lead: 'danger', words: 'Failed' });
  assert.deepEqual(stateOf(summary({ mark: 'working', running: 4 })), { dot: 'accent', words: '4 running' });
  assert.deepEqual(stateOf(summary({ mark: 'working' })), { dot: 'accent', words: 'Working' });
  assert.deepEqual(stateOf(summary()), {});
  assert.deepEqual(stateOf(undefined), {});
  assert.deepEqual(stateOf(summary({ taskState: { t1: 'Done' } }), 't1'), { words: 'Done' });
});
