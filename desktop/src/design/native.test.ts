import test from 'node:test';
import assert from 'node:assert/strict';
import { mockIPC, clearMocks } from '@tauri-apps/api/mocks';
import { openPath, revealPath } from './native.ts';

function asDesktop() {
  const globals = globalThis as unknown as { isTauri?: boolean; window?: object };
  globals.isTauri = true;
  globals.window = {};
  const calls: unknown[][] = [];
  mockIPC((command, args) => { calls.push([command, args]); });
  return calls;
}

function leaveDesktop() {
  clearMocks();
  const globals = globalThis as unknown as { isTauri?: boolean; window?: object };
  delete globals.isTauri;
  delete globals.window;
}

test('open and reveal send the path alone, even when a caller still passes a workspace', async () => {
  const calls = asDesktop();
  try {
    await openPath('/tmp/a.txt', '/tmp/not-the-root');
    await revealPath('/tmp/a.txt');
    assert.deepEqual(calls, [
      ['open_path', { path: '/tmp/a.txt' }],
      ['reveal_path', { path: '/tmp/a.txt' }],
    ]);
  } finally {
    leaveDesktop();
  }
});

test('a browser cannot open or reveal a path', async () => {
  await assert.rejects(openPath('/tmp/a.txt'), /desktop app/);
  await assert.rejects(revealPath('/tmp/a.txt', '/tmp'), /desktop app/);
});
