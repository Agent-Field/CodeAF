import test from 'node:test';
import assert from 'node:assert/strict';
import type { Tab, WorkspaceState } from '../../types.ts';
import { armFilePrompt, filePromptArmed, planOpenFile, settleFilePrompt } from './openFile.ts';

const tab = (id: string, over: Partial<Tab> = {}): Tab => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...over });
const state = (tabs: Tab[], over: Partial<WorkspaceState> = {}): WorkspaceState => ({ tabs, groups: [], activeId: tabs[0]?.id ?? '', closed: [], nextNumber: tabs.length + 1, recentIds: tabs.map(item => item.id), ...over });

test('open-file opens a New tab when none is open', () => {
  assert.deepEqual(planOpenFile(state([tab('a')])), { kind: 'open' });
  assert.deepEqual(planOpenFile(state([])), { kind: 'open' });
});

test('open-file focuses the New tab already in front', () => {
  assert.deepEqual(planOpenFile(state([tab('a'), tab('n', { kind: 'newtab', title: 'New tab' })], { activeId: 'n' })), { kind: 'focus', paneId: 'n' });
});

test('open-file focuses an open New tab instead of opening another, most recently used first', () => {
  const older = tab('old', { kind: 'newtab', title: 'New tab' });
  const newer = tab('new', { kind: 'newtab', title: 'New tab' });
  assert.deepEqual(planOpenFile(state([tab('a'), older, newer], { activeId: 'a', recentIds: ['a', 'new', 'old'] })), { kind: 'focus', paneId: 'new' });
});

test('open-file focuses the New tab pane of the split in front', () => {
  const split = tab('sp', {
    split: {
      layout: '1x2', focus: 0,
      panes: [
        { id: 'chat', kind: 'conversation', title: 'Chat', draft: '' },
        { id: 'field', kind: 'newtab', title: 'New tab', draft: '' },
      ],
    },
  });
  assert.deepEqual(planOpenFile(state([split])), { kind: 'focus', paneId: 'field' });
  split.split!.focus = 1;
  assert.deepEqual(planOpenFile(state([split])), { kind: 'focus', paneId: 'field' });
});

test('a file prompt is for the named pane, or for whichever New tab mounts next, and then settles', () => {
  armFilePrompt('field');
  assert.equal(filePromptArmed('other'), false);
  assert.equal(filePromptArmed('field'), true);
  settleFilePrompt('other');
  assert.equal(filePromptArmed('field'), true);
  settleFilePrompt('field');
  assert.equal(filePromptArmed('field'), false);
  armFilePrompt();
  assert.equal(filePromptArmed('fresh'), true);
  settleFilePrompt('fresh');
  assert.equal(filePromptArmed('fresh'), false);
});
