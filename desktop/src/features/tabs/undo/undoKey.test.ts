import test from 'node:test';
import assert from 'node:assert/strict';

Object.defineProperty(globalThis, 'navigator', { value: { platform: 'Linux x86_64' }, configurable: true });
const open: { dialog: boolean } = { dialog: false };
Object.defineProperty(globalThis, 'document', {
  value: { querySelector: () => (open.dialog ? {} : null), addEventListener() {}, removeEventListener() {} },
  configurable: true,
});
const { shouldUndo } = await import('./undoKey.ts');

test('undo reaches the stack outside editors', () => {
  assert.equal(shouldUndo({ id: 'undo' }, null), true);
});

test('other shortcuts are left alone', () => {
  assert.equal(shouldUndo({ id: 'new' }, null), false);
});

test('a terminal being typed in keeps its own undo', () => {
  const terminal = { closest: (selector: string) => (selector === '.xterm' ? {} : null) } as unknown as EventTarget;
  assert.equal(shouldUndo({ id: 'undo' }, terminal), false);
});

test('an open dialog keeps the key', () => {
  open.dialog = true;
  assert.equal(shouldUndo({ id: 'undo' }, null), false);
  open.dialog = false;
});
