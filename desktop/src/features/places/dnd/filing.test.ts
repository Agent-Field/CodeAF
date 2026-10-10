import test from 'node:test';
import assert from 'node:assert/strict';
import { performFiling, skippedSentence, type FilingDeps } from './filing.ts';

const rig = () => {
  const calls: unknown[][] = [];
  const deps: FilingDeps = {
    client: { addSource: async (...a) => { calls.push(['source', ...a]); }, addChats: async (...a) => { calls.push(['chats', ...a]); } },
    nativePath: file => file.name === 'web.png' ? undefined : `/home/me/${file.name}`,
    kindOf: async path => path.endsWith('docs') ? 'folder' : 'file',
  };
  return { calls, deps };
};

test('URLs become url sources in order', async () => {
  const { calls, deps } = rig();
  const out = await performFiling({ kind: 'sources-add', targetPlaceId: 'p', sources: { kind: 'urls', urls: ['https://a.dev', 'https://b.dev'] } }, deps);
  assert.deepEqual(calls, [['source', 'p', { kind: 'url', ref: 'https://a.dev' }], ['source', 'p', { kind: 'url', ref: 'https://b.dev' }]]);
  assert.deepEqual(out, { added: 2, skipped: [] });
});

test('files and folders use their native path and kind; a file with no path is skipped, not guessed', async () => {
  const { calls, deps } = rig();
  const files = [new File([''], 'notes.md'), new File([''], 'docs'), new File([''], 'web.png')];
  const out = await performFiling({ kind: 'sources-add', targetPlaceId: 'p', sources: { kind: 'files', files } }, deps);
  assert.deepEqual(calls, [['source', 'p', { kind: 'file', ref: '/home/me/notes.md' }], ['source', 'p', { kind: 'folder', ref: '/home/me/docs' }]]);
  assert.deepEqual(out, { added: 2, skipped: ['web.png'] });
  assert.match(skippedSentence(out.skipped), /“web\.png” could not be added/);
  assert.equal(skippedSentence([]), '');
});

test('a tab (resolved to its chat) files as membership; Option moves from the source place', async () => {
  const { calls, deps } = rig();
  await performFiling({ kind: 'chat-add', chatIds: ['c'], targetPlaceId: 'p' }, deps);
  await performFiling({ kind: 'chat-move', chatIds: ['c'], targetPlaceId: 'p', fromPlaceId: 'q' }, deps);
  assert.deepEqual(calls, [['chats', 'p', ['c'], {}], ['chats', 'p', ['c'], { moveFrom: 'q' }]]);
});

test('an engine refusal stops the drop and propagates', async () => {
  const { deps } = rig();
  deps.client.addSource = async () => { throw new Error('refused'); };
  await assert.rejects(performFiling({ kind: 'sources-add', targetPlaceId: 'p', sources: { kind: 'urls', urls: ['https://a.dev'] } }, deps), /refused/);
});
