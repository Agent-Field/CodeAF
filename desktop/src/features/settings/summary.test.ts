import test from 'node:test';
import assert from 'node:assert/strict';
import { effortWord, settingsSummary } from './summary.ts';

const pinned = [{ label: 'GLM Flash' }, { label: 'DS Flash' }, { label: 'GLM 5.3' }];

test('the summary names the pins in order and the default effort', () => {
  assert.equal(settingsSummary({ pinned, conversationEffort: 'medium' }), 'Pinned: GLM Flash, DS Flash, GLM 5.3. Default effort: Medium.');
});

test('the effort clause is absent while the model runs on its own effort', () => {
  assert.equal(settingsSummary({ pinned }), 'Pinned: GLM Flash, DS Flash, GLM 5.3.');
  assert.equal(settingsSummary({ pinned, conversationEffort: 'loud' }), 'Pinned: GLM Flash, DS Flash, GLM 5.3.');
});

test('with nothing known the summary is empty', () => {
  assert.equal(settingsSummary({ pinned: [] }), '');
  assert.equal(effortWord(undefined), '');
});
