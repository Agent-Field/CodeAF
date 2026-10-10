import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { ToolStep, WorkBlock, WorkStep } from '../types.ts';
import { callCopyText, stepCopyText, workLogText } from './copyLog.ts';

const call = (over: Partial<ToolStep>): ToolStep => ({ id: 'c', tool: 'bash', hint: '', args: '', output: '', state: 'done', ...over });
const bash = call({ id: 'b', args: JSON.stringify({ command: 'ls -la' }), output: 'a.ts\nb.ts\n' });
const grep = call({ id: 'g', tool: 'grep', args: JSON.stringify({ pattern: 'foo', path: 'src' }), output: 'src/x.ts:1:foo' });
const omitted = call({ id: 'o', callId: 'o1', tool: 'web_fetch', hint: 'https://x.dev', args: JSON.stringify({ url: 'https://x.dev' }), outputOmitted: true });
const step = (title: string, calls: ToolStep[]): WorkStep => ({ id: title, title, titleSource: 'composed', category: 'run', calls, state: 'done' });
const block = (steps: WorkStep[]): WorkBlock => ({ kind: 'work', id: 'w', steps, notes: [], live: false, summary: { steps: steps.length, calls: 0, failed: 0 } });

test('a bash call copies its command, never its output', async () => {
  assert.equal(await callCopyText(bash), 'ls -la');
});

test('another call copies its output, falling back to what the row says', async () => {
  assert.equal(await callCopyText(grep), 'src/x.ts:1:foo');
  assert.notEqual(await callCopyText({ ...grep, output: '' }), '');
});

test('an omitted output is fetched once; a failed fetch copies the row text instead', async () => {
  const asked: string[] = [];
  const text = await callCopyText(omitted, async (id) => { asked.push(id); return { output: 'page body', full: true }; });
  assert.equal(text, 'page body');
  assert.deepEqual(asked, ['o1']);
  assert.notEqual(await callCopyText(omitted, async () => { throw new Error('gone'); }), '');
});

test('a step copies each call in order, parted by a blank line', async () => {
  assert.equal(await stepCopyText(step('Look', [bash, grep])), 'ls -la\n\nsrc/x.ts:1:foo');
});

test('the log is each title, then each call and its output, steps in order', async () => {
  const log = await workLogText(block([step('List files', [bash]), step('Search', [grep])]));
  const lines = log.split('\n');
  assert.equal(lines[0], 'List files');
  assert.equal(lines[1], '$ ls -la');
  assert.deepEqual(lines.slice(2, 5), ['a.ts', 'b.ts', '']);
  assert.ok(log.indexOf('Search') > log.indexOf('b.ts'));
  assert.ok(log.endsWith('src/x.ts:1:foo'));
});
