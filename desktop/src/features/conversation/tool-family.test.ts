import assert from 'node:assert/strict';
import { test } from 'node:test';
import { argsRestateHint, hintText, rowHint, toolIcon } from './tool-family.ts';

test('arguments that only restate the hint are hidden', () => {
  assert.equal(argsRestateHint('{"path": "/tmp/ws"}', 'ls /tmp/ws'), true);
  assert.equal(argsRestateHint('', 'ls'), true);
});

test('arguments with anything the hint lacks are kept', () => {
  assert.equal(argsRestateHint('{"path": "/tmp/ws", "depth": 2}', 'ls /tmp/ws'), false);
  assert.equal(argsRestateHint('{"command": "rm -rf x"}', 'bash'), false);
  assert.equal(argsRestateHint('not json', 'ls'), false);
});

test('listing tools use the folder mark; searches, reads, edits and images the marks the design draws', () => {
  assert.equal(toolIcon('ls'), 'folder');
  assert.equal(toolIcon('list_files'), 'folder');
  assert.equal(toolIcon('grep'), 'search');
  assert.equal(toolIcon('read_file'), 'book');
  assert.equal(toolIcon('edit'), 'pencil');
  assert.equal(toolIcon('generate_image'), 'sparkles');
});

test('the hint drops the tool word the row already shows', () => {
  assert.equal(hintText('bash', 'bash ls -A | wc -l'), 'ls -A | wc -l');
  assert.equal(hintText('propose_task', 'propose_task List files'), 'List files');
  assert.equal(hintText('bash', 'bash'), '');
  assert.equal(hintText('read', 'README.md'), 'README.md');
  assert.equal(hintText('bash', 'bashful ls'), 'bashful ls');
});

test('a row keeps the tool word when its icon is the generic one', () => {
  assert.equal(rowHint('bash', 'bash ls'), 'ls');
  assert.equal(rowHint('jobs', 'jobs output'), 'jobs output');
  assert.equal(rowHint('bash', 'bash'), 'bash');
});
