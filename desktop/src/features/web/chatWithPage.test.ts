import test from 'node:test';
import assert from 'node:assert/strict';
import { newTab } from '../tabs/helpers.ts';
import { initialWorkspace, workspaceReducer, type Tab } from '../tabs/model.ts';
import { chatWithPage } from './chatWithPage.ts';

const png = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

function workspace(tabs: Tab[], groups: { id: string; title: string; collapsed: boolean }[] = []) {
  return { ...initialWorkspace(), tabs, groups, activeId: tabs[0].id, recentIds: tabs.map(tab => tab.id) };
}

test('chat-plus opens a focused conversation with the page link attached and no send', () => {
  const web = newTab({ id: 'web', kind: 'web', title: 'pkg.go.dev', target: { url: 'https://go.dev/doc' } });
  const later = newTab({ id: 'later', title: 'Later notes' });
  const state = workspace([newTab({ id: 'chat' }), web, later]);
  const started = chatWithPage(state.tabs, 'web', { url: 'https://go.dev/doc', title: '  json package  ', shot: png }, 'new-chat');
  const next = workspaceReducer(state, started.action);

  assert.deepEqual(next.tabs.map(tab => tab.id), ['chat', 'web', 'new-chat', 'later']);
  assert.equal(next.activeId, 'new-chat');
  assert.equal(started.action.background, false);
  assert.equal(started.action.at, 2);
  assert.deepEqual(started.link, { url: 'https://go.dev/doc', title: 'json package' });
  assert.equal(started.action.tab.kind, 'conversation');
  assert.equal(started.action.tab.draft, 'About this page, "json package": https://go.dev/doc\n\n');
  assert.equal(started.action.tab.sessionFile, undefined);
  assert.equal(started.action.tab.draft.includes(png), false);
  assert.equal(started.action.tab.draft.includes('<'), false);
  assert.equal(started.files.length, 1);
  assert.equal(started.files[0].name, 'page.png');
  assert.equal(started.files[0].type, 'image/png');
});

test('a grouped web tab keeps the conversation inside the group, right after it', () => {
  const web = newTab({ id: 'web', kind: 'web', title: 'Docs', groupId: 'g', target: { url: 'https://go.dev/doc' } });
  const mate = newTab({ id: 'mate', title: 'Mate', groupId: 'g' });
  const state = workspace([web, mate], [{ id: 'g', title: 'Reading', collapsed: false }]);
  const started = chatWithPage(state.tabs, 'web', { url: 'https://go.dev/doc', title: 'Docs' }, 'new-chat');
  const next = workspaceReducer(state, started.action);
  assert.deepEqual(next.tabs.map(tab => tab.id), ['web', 'new-chat', 'mate']);
  assert.equal(next.tabs[1].groupId, 'g');
  assert.equal(next.activeId, 'new-chat');
});

test('a web pane inside a split opens the conversation after that split', () => {
  const split = newTab({
    id: 'split',
    split: {
      layout: '1x2',
      focus: 0,
      panes: [
        { id: 'webpane', kind: 'web', title: 'Docs', draft: '', target: { url: 'https://go.dev/' } },
        { id: 'other', kind: 'conversation', title: 'Other', draft: '' },
      ],
    },
  });
  const later = newTab({ id: 'later', title: 'Later' });
  const state = workspace([split, later]);
  const started = chatWithPage(state.tabs, 'webpane', { url: 'https://go.dev/', title: '' }, 'new-chat');
  const next = workspaceReducer(state, started.action);
  assert.deepEqual(next.tabs.map(tab => tab.id), ['split', 'new-chat', 'later']);
  assert.deepEqual(started.link, { url: 'https://go.dev/', title: '' });
  assert.equal(started.action.tab.draft, 'About this page: https://go.dev/\n\n');
  assert.equal(started.action.tab.title, 'go.dev');
  assert.equal(started.files.length, 0);
});

test('an unknown pane appends, and a pinned web tab does not pin the conversation', () => {
  const web = newTab({ id: 'web', kind: 'web', title: 'Docs', pinned: true, target: { url: 'https://go.dev/' } });
  const inbox = newTab({ id: 'inbox', title: 'Inbox', pinned: true });
  const later = newTab({ id: 'later', title: 'Later' });
  const state = workspace([web, inbox, later]);
  const started = chatWithPage(state.tabs, 'missing', { url: 'https://go.dev/', title: 'Docs' }, 'new-chat');
  const next = workspaceReducer(state, started.action);
  assert.equal(next.tabs.at(-1)?.id, 'new-chat');
  assert.equal(next.tabs.at(-1)?.pinned, false);

  const afterPinned = chatWithPage(state.tabs, 'web', { url: 'https://go.dev/', title: 'Docs' }, 'after');
  const placed = workspaceReducer(state, afterPinned.action);
  assert.equal(placed.tabs.findIndex(tab => tab.id === 'after') > placed.tabs.findIndex(tab => tab.id === 'inbox'), true);
  assert.equal(placed.tabs.find(tab => tab.id === 'after')?.pinned, false);
});
