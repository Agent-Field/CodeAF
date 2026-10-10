import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineQuestion } from '../../chat/engine-client.ts';
import type { TabSummary } from '../../conversation/tabSummary.ts';
import { alsoOpenIn, askOf, changedLines, headLines, questionsFor, stateOf, tailLines } from './content.ts';

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

const asking = (id: number, head: string, tasks: string[]): EngineQuestion => ({ ...consent(id, head), blocking: { turn: true, tasks } });

test('a task card owns only the questions that name its task', () => {
  const mine = asking(1, 'Run git step 1', ['t7']);
  const other = asking(2, 'Run git step 2', ['t9']);
  const conversation = summary({ mark: 'waiting', questions: [mine, other], taskState: { t7: 'Running', t9: 'Running' } });
  assert.deepEqual(questionsFor(conversation).map(q => q.id), [1, 2]);
  assert.deepEqual(questionsFor(conversation, 't7').map(q => q.id), [1]);
  assert.deepEqual(questionsFor(conversation, 'tX'), []);
  assert.equal(askOf(questionsFor(conversation, 't7'))?.text, 'Run git step 1');
  assert.equal(askOf(questionsFor(conversation, 'tX')), undefined);
});

test('a task card says Needs you only for its own question, and the conversation mark is not its state', () => {
  const conversation = summary({ mark: 'waiting', questions: [asking(2, 'x', ['t9'])], taskState: { t7: 'Running', t9: 'Running' } });
  assert.deepEqual(stateOf(conversation, 't7'), { words: 'Running' });
  assert.deepEqual(stateOf(conversation, 't9'), { lead: 'amber', words: 'Needs you' });
  assert.deepEqual(stateOf(summary({ mark: 'failed', taskState: { t7: 'Done' } }), 't7'), { words: 'Done' });
});

test('the hover line names the other place that holds the chat, and counts any more', () => {
  assert.equal(alsoOpenIn(undefined), undefined);
  assert.equal(alsoOpenIn([]), undefined);
  assert.equal(alsoOpenIn([{ name: 'Marketing' }]), 'also open in Marketing');
  assert.equal(alsoOpenIn([{ name: 'Marketing' }, { name: 'Ops' }]), 'also open in Marketing and 1 other place');
  assert.equal(alsoOpenIn([{ name: 'Marketing' }, { name: 'Ops' }, { name: 'Dev' }]), 'also open in Marketing and 2 other places');
});
