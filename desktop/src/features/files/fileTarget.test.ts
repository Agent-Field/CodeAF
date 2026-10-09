import test from 'node:test';
import assert from 'node:assert/strict';
import { defaultView, fileTab, fileTypeIcon, findFileTab, splitFilePath } from './fileTarget.ts';
import { cleanView } from '../tabs/view-state.ts';

test('splits a path into name and folder', () => {
  assert.deepEqual(splitFilePath('internal/parse/lexer.go'), { name: 'lexer.go', dir: 'internal/parse' });
  assert.deepEqual(splitFilePath('README.md'), { name: 'README.md', dir: '' });
});

test('a file tab opens on the file, a diff tab on the changes', () => {
  assert.equal(defaultView('file'), 'file');
  assert.equal(defaultView('diff'), 'changes');
  const tab = fileTab({ sessionFile: '/s/a.jsonl' }, 'internal/parse/lexer.go', 'diff', 't1');
  assert.deepEqual(tab, { id: 't1', kind: 'diff', title: 'lexer.go', titleSource: 'manual', pinned: false, draft: '', sessionFile: '/s/a.jsonl', file: { path: 'internal/parse/lexer.go', view: 'changes' } });
});

test('finds an open tab for the same file in the same session, also inside a split', () => {
  const one = fileTab({ sessionFile: 's1' }, 'a.go', 'file', 'one');
  const inSplit = { ...fileTab({ sessionFile: 's1' }, 'b.go', 'diff', 'host'), split: { layout: '1x2' as const, focus: 0, panes: [fileTab({ sessionFile: 's1' }, 'b.go', 'diff', 'pane1'), fileTab({ sessionFile: 's1' }, 'c.go', 'diff', 'pane2')] } };
  const tabs = [one, inSplit];
  assert.equal(findFileTab(tabs, { kind: 'file', sessionFile: 's1', file: { path: 'a.go' } }), 'one');
  assert.equal(findFileTab(tabs, { kind: 'diff', sessionFile: 's1', file: { path: 'c.go' } }), 'pane2');
  assert.equal(findFileTab(tabs, { kind: 'diff', sessionFile: 's2', file: { path: 'c.go' } }), undefined);
  assert.equal(findFileTab(tabs, { kind: 'diff', sessionFile: 's1', file: { path: 'a.go' } }), undefined);
});

test('the persisted file target is validated', () => {
  assert.deepEqual(cleanView({ file: { path: 'a.go', view: 'file' } }), { file: { path: 'a.go', view: 'file' } });
  assert.deepEqual(cleanView({ file: { path: 'a.go' } }), { file: { path: 'a.go' } });
  assert.deepEqual(cleanView({ file: { path: '', view: 'file' } }), {});
  assert.deepEqual(cleanView({ file: { path: 'a.go', view: 'blame' } }), {});
});

test('a file tab draws its type: code, JSON, prose and pictures', () => {
  assert.equal(fileTypeIcon('lexer.go'), 'fileCode2');
  assert.equal(fileTypeIcon('package.json'), 'fileJson');
  assert.equal(fileTypeIcon('README.md'), 'file');
  assert.equal(fileTypeIcon('Makefile'), 'file');
  assert.equal(fileTypeIcon('logo.png'), 'image');
});
