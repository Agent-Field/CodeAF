import assert from 'node:assert/strict';
import { test } from 'node:test';
import { foldedTurnMenu, messageMenu } from './messageMenu.ts';
import type { TurnV2 } from '../types';

const turn: TurnV2 = {
  id: 'turn:42', user: 'question', attachments: [], steer: [], digest: 'Short digest', state: 'done',
  blocks: [
    { kind: 'update', id: 'update', text: 'Interim words', cut: false, streaming: false },
    { kind: 'answer', id: 'answer1', text: '**Full answer**\n\n```ts\nconst n = 1;\n```', streaming: false },
    { kind: 'answer', id: 'answer2', text: '- Second answer', streaming: false },
  ],
};

function action(items: ReturnType<typeof messageMenu>, id: string) {
  const entry = items.find(item => item.id === id);
  assert.ok(entry && (!entry.kind || entry.kind === 'action'));
  return entry;
}

test('bubble Copy preserves the hover text and offers no edit', async () => {
  let copied = '';
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { clipboard: { writeText: async (text: string) => { copied = text; } } } });
  const items = messageMenu('Literal **words**\nSecond line');
  assert.deepEqual(items.map(item => item.label), ['Copy']);
  action(items, 'copy').onSelect();
  assert.equal(copied, 'Literal **words**\nSecond line');
});

test('folded Copy answer joins all answer Markdown and opens the exact anchor in the background', () => {
  let copied = '';
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { clipboard: { writeText: async (text: string) => { copied = text; } } } });
  const opened: unknown[] = [];
  const items = foldedTurnMenu(turn, (...args) => opened.push(args));
  action(items, 'copy-answer').onSelect();
  assert.equal(copied, '**Full answer**\n\n```ts\nconst n = 1;\n```\n\n- Second answer');
  action(items, 'open-tab').onSelect();
  assert.deepEqual(opened, [['turn:42', true]]);
});

test('missing answers disable Copy and missing tab handlers omit Open', () => {
  const items = foldedTurnMenu({ ...turn, blocks: [] });
  assert.equal(action(items, 'copy-answer').disabled, true);
  assert.deepEqual(items.map(item => item.id), ['copy-answer']);
  assert.equal(action(messageMenu(''), 'copy').disabled, true);
});

test('clipboard refusals do not reject out of the action handler', async () => {
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { clipboard: { writeText: async () => { throw new Error('Denied'); } } } });
  action(messageMenu('words'), 'copy').onSelect();
  await new Promise(resolve => setImmediate(resolve));
});
