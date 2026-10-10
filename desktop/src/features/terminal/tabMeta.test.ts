import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import type { Tab } from '../tabs/types.ts';
import { clearTerminalTabMeta, finishedJobTabMeta, offerTerminalTabMeta, terminalTabMeta, type TabMetaSource } from './tabMeta.ts';

function tab(id: string): Tab {
  return { id, kind: 'terminal', title: 'nightly-bench', draft: '', pinned: false };
}

const exited = (exitCode?: number): TabMetaSource => ({ kind: 'job', state: 'exited', exitCode });

test('a finished job reads exit N; a running job and a shell read nothing', () => {
  assert.equal(finishedJobTabMeta(undefined), undefined);
  assert.equal(finishedJobTabMeta({ kind: 'job', state: 'running' }), undefined);
  assert.equal(finishedJobTabMeta({ kind: 'job', state: 'running', exitCode: 0 }), undefined);
  assert.equal(finishedJobTabMeta({ kind: 'terminal', state: 'exited', exitCode: 0 }), undefined);
  assert.equal(finishedJobTabMeta({ kind: 'terminal', state: 'running' }), undefined);
  assert.equal(finishedJobTabMeta({ kind: 'terminal', state: 'closed', exitCode: 129 }), undefined);
  // A job the person stopped says "closed" in the header, not an exit on the tab.
  assert.equal(finishedJobTabMeta({ kind: 'job', state: 'closed', exitCode: 129 }), undefined);
  assert.equal(finishedJobTabMeta(exited(0)), 'exit 0');
  assert.equal(finishedJobTabMeta(exited()), 'exit 0');
  // The words carry no colour. A non-zero exit lights the failed dot through the pane's mark, not through this string.
  assert.equal(finishedJobTabMeta(exited(2)), 'exit 2');

  const pane = tab('job');
  assert.equal(terminalTabMeta(pane), undefined);
  offerTerminalTabMeta(pane.id, () => exited(0));
  try {
    const kind = readFileSync(new URL('../tabs/kinds/terminal.ts', import.meta.url), 'utf8');
    assert.match(kind, /tabMeta:\s*terminalTabMeta/);
    assert.equal(terminalTabMeta(pane), 'exit 0');
    offerTerminalTabMeta(pane.id, () => ({ kind: 'job', state: 'running' }));
    assert.equal(terminalTabMeta(pane), undefined);
  } finally {
    clearTerminalTabMeta(pane.id);
  }
  assert.equal(terminalTabMeta(pane), undefined);
});
