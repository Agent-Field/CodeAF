import test from 'node:test';
import assert from 'node:assert/strict';
import { homeShort, limitSentence, metaLine, removeLabel, toneOf } from './state.ts';

test('the home folder shortens to a tilde and other paths stay whole', () => {
  assert.equal(homeShort('/Users/ana/codeaf'), '~/codeaf');
  assert.equal(homeShort('/home/ana'), '~');
  assert.equal(homeShort('/srv/builds/codeaf'), '/srv/builds/codeaf');
  assert.equal(homeShort('/Users'), '/Users');
});
test('the meta line names the place and the kind', () => {
  assert.equal(metaLine({ cwd: '/Users/ana/codeaf', kind: 'job' }), '~/codeaf · job');
  assert.equal(metaLine({ cwd: '/Users/ana/codeaf', kind: 'terminal' }), '~/codeaf · terminal');
});
test('the glyph tone follows the state and the exit code', () => {
  assert.equal(toneOf({ state: 'running' }), 'running');
  assert.equal(toneOf({ state: 'exited', exitCode: 0 }), 'done');
  assert.equal(toneOf({ state: 'exited', exitCode: 2 }), 'failed');
  assert.equal(toneOf({ state: 'closed', exitCode: 129 }), 'stopped');
});
test('the removal row names what is removed', () => {
  assert.equal(removeLabel('job'), 'Remove job');
  assert.equal(removeLabel('terminal'), 'Remove terminal');
});
test('the engine limit is worded for a person, and other refusals pass through', () => {
  assert.equal(limitSentence('16 terminals are already running; close one first'), '16 terminals are open in this conversation. Close one to start another.');
  assert.equal(limitSentence('no such session'), undefined);
});
