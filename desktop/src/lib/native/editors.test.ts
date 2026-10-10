import test from 'node:test';
import assert from 'node:assert/strict';
import { createOpenWith, openWith } from './editors.ts';

test('a browser build has no opener, so the chosen editor stays absent', () => {
  assert.equal(openWith, undefined);
  const calls: unknown[] = [];
  const opener = createOpenWith({
    desktop: false,
    invoke: async (command, args) => { calls.push({ command, args }); },
  });
  assert.equal(opener, undefined);
  assert.deepEqual(calls, []);
});

test('the desktop opener sends the path, the editor id and the conversation, never a command', async () => {
  const calls: { command: string; args: { path: string; editorId: string; session: string } }[] = [];
  const opener = createOpenWith({
    desktop: true,
    invoke: async (command, args) => { calls.push({ command, args }); },
  });
  assert.ok(opener);
  await opener('/ws/a.txt', 'code.desktop', 'ab'.repeat(32));
  assert.deepEqual(calls, [{
    command: 'open_with',
    args: { path: '/ws/a.txt', editorId: 'code.desktop', session: 'ab'.repeat(32) },
  }]);
  assert.equal('command' in calls[0].args, false);
});
