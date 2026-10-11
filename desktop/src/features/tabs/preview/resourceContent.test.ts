import assert from 'node:assert/strict';
import test from 'node:test';
import { loadedCache, resourceKey } from './loadedCache.ts';
import { previewSession, rememberDiff, rememberFile, rememberPreviewSession } from './resourceContent.ts';

test('workspace reads prime compact, separate file and diff previews synchronously', () => {
  const file = rememberFile('loaded-session', 'a.ts', { path: 'a.ts', name: 'a.ts', dir: '', abs: '/a.ts', size: 19, text: 'one\ntwo\nthree\nfour\n', lines: 4 });
  assert.deepEqual(file.head.map(line => line.text), ['one', 'two', 'three']);
  assert.equal(file.lines, 4);
  assert.equal(loadedCache.read(resourceKey('file', 'loaded-session', 'a.ts')), file);
  const diff = rememberDiff('loaded-session', 'a.ts', { path: 'a.ts', name: 'a.ts', dir: '', abs: '/a.ts', git: true, status: 'modified', added: 2, deleted: 1, lines: 4, hunks: [{ header: '', oldStart: 1, oldLines: 1, newStart: 1, newLines: 2, lines: [{ kind: 'del', text: 'old' }, { kind: 'add', text: 'new' }, { kind: 'add', text: 'later' }] }] });
  assert.deepEqual(diff.head.map(line => line.text), ['− old', '+ new']);
  assert.equal(loadedCache.read(resourceKey('file', 'loaded-session', 'a.ts')), file);
  assert.equal(loadedCache.read(resourceKey('diff', 'loaded-session', 'a.ts')), diff);
});

test('a single enormous file line does not turn the preview cache into a full file cache', () => {
  const file = rememberFile('s', 'long', { path: 'long', name: 'long', dir: '', abs: '/long', size: 1_000_000, text: 'x'.repeat(1_000_000), lines: 1 });
  assert.equal(file.head[0].text.length, 4096);
  assert.equal('text' in file, false);
});

test('saved previews resolve only the session attached for that saved file', () => {
  rememberPreviewSession('a.jsonl', 'session-a');
  assert.equal(previewSession('a.jsonl'), 'session-a');
  assert.equal(previewSession('b.jsonl'), undefined);
  assert.equal(previewSession(undefined), undefined);
});
