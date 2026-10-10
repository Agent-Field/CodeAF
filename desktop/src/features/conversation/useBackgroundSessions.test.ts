import assert from 'node:assert/strict';
import test from 'node:test';
import type { EngineQuestion } from '../chat/engine-client.ts';
import type { WorldRow } from '../world/types.ts';
import type { TabSummary } from './tabSummary.ts';
import { mergeBackgroundSummary, rowForTarget, updatesFromWorldRows, type SessionTarget } from './useBackgroundSessions.ts';

const question: EngineQuestion = { id: 1, kind: 'consent', ask: 'permission', head: 'Run git' };

function row(over: Partial<WorldRow> & Pick<WorldRow, 'chatId'>): WorldRow {
  return { title: '', running: false, needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: false, archived: false, ...over };
}

const kept: TabSummary = {
  title: 'Kept title', firstLine: 'hello', digest: 'The fixtures run now.', mark: 'waiting',
  sessionId: 'session-1', chatId: 'old', running: 2, questions: [question],
};

test('a background tab matches its journal path, then its chat folder', () => {
  const byFile = row({ chatId: 'other', sessionFile: 'saved-2.jsonl', title: 'From file' });
  const byFolder = row({ chatId: 'chat-9', title: 'From folder' });
  assert.equal(rowForTarget([byFolder, byFile], { id: 't', sessionFile: 'saved-2.jsonl' })?.title, 'From file');
  assert.equal(rowForTarget([byFolder], { id: 't', sessionFile: 'projects/bucket/chat-9/session.jsonl' })?.title, 'From folder');
  assert.equal(rowForTarget([byFolder], { id: 't', sessionFile: 'saved-2.jsonl' }), undefined);
});

test('a missing row does not invent a snapshot, and a row that leaves clears the live marks', () => {
  const target: SessionTarget = { id: 'tab', sessionFile: 'projects/bucket/chat-9/session.jsonl' };
  const quiet = updatesFromWorldRows([target], [], new Map());
  assert.deepEqual(quiet, []);
  const live = row({ chatId: 'chat-9', title: 'Storage', running: true, needsYou: 2, tasksRunning: 4, updatedAt: '2026-10-10T00:00:00Z' });
  const first = updatesFromWorldRows([target], [live], new Map());
  assert.equal(first.length, 1);
  assert.equal(first[0].snapshot.running, true);
  assert.equal(first[0].snapshot.needsPerson, true);
  assert.equal(first[0].snapshot.title, 'Storage');
  assert.deepEqual(first[0].snapshot.entries, []);
  const again = updatesFromWorldRows([target], [live], new Map([[target.id, first[0].signature]]));
  assert.deepEqual(again, []);
  const gone = updatesFromWorldRows([target], [], new Map([[target.id, first[0].signature]]));
  assert.equal(gone.length, 1);
  assert.equal(gone[0].snapshot.running, false);
  assert.equal(gone[0].snapshot.needsPerson, false);
  const cleared = mergeBackgroundSummary(kept, gone[0].snapshot);
  assert.equal(cleared.mark, undefined);
  assert.deepEqual(cleared.questions, []);
  assert.equal(cleared.digest, 'The fixtures run now.');
  assert.equal(cleared.sessionId, 'session-1');
});

test('running, needs-you and title come from the row and a reply already read stays', () => {
  const target: SessionTarget = { id: 'tab', sessionFile: 'projects/bucket/chat-9/session.jsonl' };
  const waiting = updatesFromWorldRows([target], [row({ chatId: 'chat-9', title: 'Storage', needsYou: 1 })], new Map());
  const merged = mergeBackgroundSummary(kept, waiting[0].snapshot);
  assert.equal(merged.mark, 'waiting');
  assert.equal(merged.title, 'Storage');
  assert.equal(merged.digest, 'The fixtures run now.');
  assert.equal(merged.firstLine, 'hello');
  assert.deepEqual(merged.questions, [question]);
  assert.equal(merged.sessionId, 'session-1');
  assert.equal(merged.chatId, 'chat-9');

  const working = updatesFromWorldRows([target], [row({ chatId: 'chat-9', tasksRunning: 4 })], new Map());
  const busy = mergeBackgroundSummary(kept, working[0].snapshot);
  assert.equal(busy.mark, 'working');
  assert.equal(busy.running, 4);
  assert.equal(busy.title, 'Kept title');

  const failed = updatesFromWorldRows([target], [row({ chatId: 'chat-9', failed: 2 })], new Map());
  assert.equal(mergeBackgroundSummary(undefined, failed[0].snapshot).mark, 'failed');
  const askingAndFailed = updatesFromWorldRows([target], [row({ chatId: 'chat-9', needsYou: 1, failed: 2 })], new Map());
  assert.equal(mergeBackgroundSummary(undefined, askingAndFailed[0].snapshot).mark, 'waiting');
});
