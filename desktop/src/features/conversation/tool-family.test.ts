import assert from 'node:assert/strict';
import { test } from 'node:test';
import { argsRestateHint, toolIcon } from './tool-family.ts';

test('arguments that only restate the hint are hidden', () => {
  assert.equal(argsRestateHint('{"path": "/tmp/ws"}', 'ls /tmp/ws'), true);
  assert.equal(argsRestateHint('', 'ls'), true);
});

test('arguments with anything the hint lacks are kept', () => {
  assert.equal(argsRestateHint('{"path": "/tmp/ws", "depth": 2}', 'ls /tmp/ws'), false);
  assert.equal(argsRestateHint('{"command": "rm -rf x"}', 'bash'), false);
  assert.equal(argsRestateHint('not json', 'ls'), false);
});

test('listing tools use the folder mark, searches the file-search mark', () => {
  assert.equal(toolIcon('ls'), 'folder');
  assert.equal(toolIcon('list_files'), 'folder');
  assert.equal(toolIcon('grep'), 'findFiles');
  assert.equal(toolIcon('read_file'), 'file');
});
