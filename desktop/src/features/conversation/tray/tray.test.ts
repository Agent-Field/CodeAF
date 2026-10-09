import test from 'node:test';
import assert from 'node:assert/strict';
import type { EngineQuestion } from '../../chat/engine-client.ts';
import { answerFor, bulkAnswers, canPress, canSend, decideAnswer, initialDraft, submitAnswer, summarize } from './answers.ts';
import { clockText, deadlineAt } from './clock.ts';
import { compareRows, formOf, optionLabel, optionVariant, visibleOptions } from './form.ts';
import { blocksComposer, layoutTabs, orderQuestions } from './layout.ts';

const consent = (id: number, extra: Partial<EngineQuestion> = {}): EngineQuestion => ({
  id,
  kind: 'consent',
  ask: 'permission',
  head: `Run command ${id}`,
  options: [
    { key: '1', label: 'allow once' },
    { key: '2', label: 'always', widening: true },
    { key: '3', label: 'deny', safe: true },
  ],
  scope: ['once', 'always'],
  stakes: 'costly',
  ...extra,
});

const landing: EngineQuestion = {
  id: 7,
  kind: 'landing',
  ask: 'landing',
  head: 'Your call',
  options: [
    { key: 'a', label: 'accept' },
    { key: 'n', label: 'not right', safe: true },
    { key: 's', label: 'tell it' },
  ],
};

const proposal: EngineQuestion = {
  id: 9,
  kind: 'task',
  ask: 'confirmation',
  head: 'Start a task',
  options: [
    { key: '1', label: 'start it' },
    { key: '2', label: 'no', safe: true },
  ],
  input: { kind: 'blanks', blanks: [{ label: 'Model', kind: 'choice', choices: ['fast', 'deep'] }] },
  deadline: '2026-10-09T10:00:15Z',
};

test('forms are told apart by lane and input', () => {
  assert.equal(formOf(consent(1)), 'permission');
  assert.equal(formOf(landing), 'landing');
  assert.equal(formOf(proposal), 'proposal');
  assert.equal(formOf({ id: 1, kind: 'ask', ask: 'choice', head: 'h', input: { kind: 'dial' } }), 'dial');
  assert.equal(formOf({ id: 1, kind: 'connect', ask: 'clarification', head: 'h', input: { secret: true } }), 'text');
});

test('permission: allow once is primary, deny is quiet and carries the reason', () => {
  const q = consent(1);
  const [allow, always, deny] = q.options!;
  assert.equal(optionVariant(q, allow), 'primary');
  assert.equal(optionVariant(q, always), 'secondary');
  assert.equal(optionVariant(q, deny), 'quiet');
  assert.equal(optionLabel(q, deny), 'Deny');
  const draft = { ...initialDraft(q), change: ' too risky ' };
  assert.deepEqual(answerFor(q, draft, deny), { kind: 'consent', id: 1, key: '3', picked: ['3'], change: 'too risky' });
  assert.equal(answerFor(q, draft, allow).change, undefined);
});

test('always carries the chosen scope, and is never offered for irreversible work', () => {
  const q = consent(1);
  const draft = { ...initialDraft(q), scope: 'always' };
  assert.equal(answerFor(q, draft, q.options![1]).scope, 'always');
  assert.equal(visibleOptions(consent(2, { stakes: 'irreversible' })).length, 2);
});

test('the safe option is quiet even when it comes first', () => {
  const q: EngineQuestion = {
    id: 1,
    kind: 'ask',
    ask: 'choice',
    head: 'h',
    options: [
      { key: 'x', label: 'leave it', safe: true },
      { key: 'y', label: 'rewrite' },
    ],
    pick: { key: 'y' },
  };
  assert.equal(optionVariant(q, q.options![0]), 'quiet');
  assert.equal(optionVariant(q, q.options![1]), 'primary');
});

test('landing: tell it needs words and sends key s with the change', () => {
  const tell = landing.options![2];
  const draft = initialDraft(landing);
  assert.equal(canPress(landing, draft, tell), false);
  assert.equal(optionLabel(landing, tell), 'Tell it…');
  const typed = { ...draft, change: 'use the older API' };
  assert.equal(canPress(landing, typed, tell), true);
  assert.deepEqual(answerFor(landing, typed, tell), { kind: 'landing', id: 7, key: 's', picked: ['s'], change: 'use the older API' });
});

test('fuel: add more needs words', () => {
  const fuel: EngineQuestion = { id: 1, kind: 'fuel', ask: 'choice', head: 'h', options: [{ key: '1', label: 'add more' }, { key: '3', label: 'stop', safe: true }] };
  assert.equal(canPress(fuel, initialDraft(fuel), fuel.options![0]), false);
  assert.equal(canPress(fuel, initialDraft(fuel), fuel.options![1]), true);
});

test('proposal: start sends key 1 with the model blank; empty blanks are left out', () => {
  const start = proposal.options![0];
  const draft = initialDraft(proposal);
  assert.equal(draft.blanks.Model, 'fast');
  assert.deepEqual(answerFor(proposal, draft, start).blanks, { Model: 'fast' });
  assert.equal(answerFor(proposal, draft, start).key, '1');
  assert.equal(answerFor(proposal, { ...draft, blanks: { Model: '' } }, start).blanks, undefined);
  assert.equal(answerFor(proposal, draft, proposal.options![1]).blanks, undefined);
});

test('proposal clock says it starts; a suggested-answer clock says so', () => {
  const now = Date.parse('2026-10-09T10:00:03Z');
  assert.equal(clockText(proposal, now), 'Starts in 12s');
  const auto: EngineQuestion = { id: 2, kind: 'ask', ask: 'choice', head: 'h', pick: { key: '1' }, asked: '2026-10-09T10:00:00Z', policy: { kind: 'recommend-then-auto', after: 15e9 } };
  assert.equal(clockText(auto, now), 'Takes the suggested answer in 12s');
  assert.equal(deadlineAt({ ...auto, stakes: 'irreversible' }), null);
  assert.equal(clockText(consent(1), now), null);
});

test('secret text goes in change; pairs, dial, checklist, blanks use their own fields', () => {
  const secret: EngineQuestion = { id: 1, kind: 'connect', ask: 'clarification', head: 'h', input: { kind: 'text', secret: true } };
  const sent = submitAnswer(secret, { ...initialDraft(secret), change: 'sk-123' });
  assert.equal(sent.change, 'sk-123');
  assert.equal(summarize(secret, sent), 'Sent');
  const dial: EngineQuestion = { id: 2, kind: 'ask', ask: 'choice', head: 'h', input: { kind: 'dial', dial: { min: 0, max: 10, default: 4 } } };
  assert.equal(submitAnswer(dial, initialDraft(dial)).dial, 4);
  const pairs: EngineQuestion = { id: 3, kind: 'ask', ask: 'choice', head: 'h', input: { kind: 'pairs', blanks: [{ label: 'Speed or cost', choices: ['Speed', 'Cost'] }] } };
  assert.deepEqual(submitAnswer(pairs, initialDraft(pairs)).blanks, { 'Speed or cost': 'either' });
  const list: EngineQuestion = { id: 4, kind: 'ask', ask: 'choice', head: 'h', input: { kind: 'checklist' }, options: [{ key: 'a', label: 'A' }, { key: 'b', label: 'B' }] };
  assert.equal(canSend(list, initialDraft(list)), false);
  const done = submitAnswer(list, { ...initialDraft(list), picked: ['b', 'a'] });
  assert.deepEqual([done.key, done.picked], ['b', ['b', 'a']]);
});

test('you decide takes the pick as the asker, except when irreversible', () => {
  const q: EngineQuestion = { ...landing, pick: { key: 'a' } };
  assert.deepEqual(decideAnswer(q), { kind: 'landing', id: 7, key: 'a', picked: ['a'], decidedBy: 'asker' });
  assert.equal(decideAnswer({ ...q, stakes: 'irreversible' }), null);
  assert.equal(decideAnswer(landing), null);
});

test('compare table needs shared axes on every option', () => {
  const dims = (speed: string, cost?: string): Record<string, string> => (cost ? { speed, cost } : { speed });
  const q: EngineQuestion = { id: 1, kind: 'ask', ask: 'choice', head: 'h', options: [{ key: '1', label: 'a', dimensions: dims('fast', '$1') }, { key: '2', label: 'b', dimensions: dims('slow') }] };
  assert.deepEqual(compareRows(q), [['', 'a', 'b'], ['speed', 'fast', 'slow']]);
  q.options![1].dimensions = { other: 'x' };
  assert.equal(compareRows(q), null);
});

test('tabs: deeper first, then oldest; withdrawn and folded leave', () => {
  const a = consent(1, { asked: '2026-10-09T10:00:02Z' });
  const b = consent(2, { asked: '2026-10-09T10:00:01Z' });
  const c = consent(3, { clarificationDepth: 1, asked: '2026-10-09T10:00:09Z' });
  const gone = consent(4, { withdrawn: { reason: 'x' } });
  assert.deepEqual(orderQuestions([a, b, c, gone]).map((q) => q.id), [3, 2, 1]);
  const layout = layoutTabs([a, b, c, gone], new Set(), new Set(['consent:2']));
  assert.deepEqual(layout.tabs.map((t) => t.id), ['consent:3', 'consent:1']);
  assert.deepEqual(layout.folded.map((q) => q.id), [2]);
});

test('a batch of permissions collapses; one by one adds a review tab; irreversible stays out', () => {
  const set = [consent(1, { batch: 'step:1' }), consent(2, { batch: 'step:1' }), consent(3, { batch: 'step:1' })];
  assert.deepEqual(layoutTabs(set, new Set(), new Set()).tabs.map((t) => t.kind), ['bulk']);
  assert.deepEqual(layoutTabs(set, new Set(['step:1']), new Set()).tabs.map((t) => t.kind), ['question', 'question', 'question', 'review']);
  const mixed = [consent(1, { batch: 'b' }), { ...landing, batch: 'b' }];
  assert.deepEqual(layoutTabs(mixed, new Set(), new Set()).tabs.map((t) => t.kind), ['question', 'question', 'review']);
  const risky = [consent(1, { batch: 'b' }), consent(2, { batch: 'b', stakes: 'irreversible' })];
  assert.deepEqual(layoutTabs(risky, new Set(), new Set()).tabs.map((t) => t.kind), ['question', 'question']);
  assert.equal(bulkAnswers(set, 'deny').map((a) => a.key).join(), '3,3,3');
  assert.equal(bulkAnswers(set, 'allow').map((a) => a.key).join(), '1,1,1');
});

test('only a turn-blocking question blocks the composer', () => {
  assert.equal(blocksComposer([landing, consent(1)]), false);
  assert.equal(blocksComposer([landing, consent(1, { blocking: { turn: true } })]), true);
  assert.equal(blocksComposer([consent(1, { blocking: { turn: true }, withdrawn: { reason: 'x' } })]), false);
});

test('option labels keep the engine\'s case for paths, file names and lone words; a plain phrase starts with a capital', () => {
  const q: EngineQuestion = {
    id: 3,
    kind: 'ask',
    ask: 'choice',
    head: 'Which name should the new file have?',
    options: [
      { key: '1', label: 'src/c.txt' },
      { key: '2', label: 'notes.md' },
      { key: '3', label: 'json' },
      { key: '4', label: 'not now', safe: true },
    ],
  };
  assert.deepEqual(q.options!.map((option) => optionLabel(q, option)), ['src/c.txt', 'notes.md', 'json', 'Not now']);
  assert.equal(summarize(q, { kind: 'ask', id: 3, key: '1', picked: ['1'] }), 'src/c.txt');
});
