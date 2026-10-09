import test from 'node:test';
import assert from 'node:assert/strict';
import type { ToolStep } from '../types.ts';
import { categoryOf, composedTitle, narrationTitle } from './titles.ts';

const call = (tool: string, args: object = {}): ToolStep => ({
  id: tool,
  tool,
  hint: '',
  args: JSON.stringify(args),
  output: '',
  state: 'done',
});

test('reads share a directory in the title', () => {
  const calls = ['a.go', 'b.go', 'c.go'].map((f) => call('read', { path: `internal/x/${f}` }));
  assert.equal(composedTitle(calls, true), 'Read 3 files in internal/x');
  assert.equal(composedTitle(calls, false), 'Reading 3 files in internal/x');
});

test('one file is named by its base name', () => {
  assert.equal(composedTitle([call('edit', { path: 'internal/x/x.go' })], true), 'Edited x.go');
  assert.equal(composedTitle([call('write', { path: 'notes.md' })], false), 'Writing notes.md');
});

test('files in different directories carry no directory', () => {
  const calls = [call('read', { path: 'a/x.go' }), call('read', { path: 'b/y.go' })];
  assert.equal(composedTitle(calls, true), 'Read 2 files');
});

test('commands are counted; known commands are named', () => {
  const two = [call('bash', { command: 'ls' }), call('bash', { command: 'pwd' })];
  assert.equal(composedTitle(two, true), 'Ran 2 commands');
  assert.equal(composedTitle([call('bash', { command: 'go test ./...' })], true), 'Ran the tests');
  assert.equal(composedTitle([call('bash', { command: 'go test ./...' })], false), 'Running the tests');
  assert.equal(composedTitle([call('bash', { command: 'echo hi' })], false), 'Running a command');
});

test('searches name their pattern or query', () => {
  assert.equal(composedTitle([call('grep', { pattern: 'TODO' })], true), 'Searched for “TODO”');
  assert.equal(composedTitle([call('web_search', { query: 'go slog' })], true), 'Searched the web for “go slog”');
  assert.equal(composedTitle([call('web_search', {})], true), 'Searched the web');
});

test('mixed tools join in call order', () => {
  const calls = [call('read', { path: 'a.go' }), call('bash', { command: 'ls' })];
  assert.equal(composedTitle(calls, true), 'Read a.go, ran a command');
});

test('unknown tools are named in words', () => {
  assert.equal(composedTitle([call('read_document')], true), 'Used read document');
});

test('narration becomes a one-line title', () => {
  assert.equal(narrationTitle('Let me check the **config** loader.\nSecond line.'), 'Let me check the config loader');
  assert.equal(narrationTitle('  '), '');
  assert.ok(narrationTitle('x'.repeat(200)).length <= 90);
});

test('category: caption wins, else the tool family', () => {
  assert.equal(categoryOf('bash', 'test'), 'test');
  assert.equal(categoryOf('bash'), 'run');
  assert.equal(categoryOf('write'), 'create');
  assert.equal(categoryOf('web_fetch'), 'browse');
  assert.equal(categoryOf('propose_task'), 'plan');
  assert.equal(categoryOf('mystery'), 'work');
});
