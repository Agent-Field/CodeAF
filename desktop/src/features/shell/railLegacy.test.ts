import assert from 'node:assert/strict';
import test from 'node:test';
import { designSystemRow } from './railLegacy.ts';

test('a production build drops the Design system rail row', () => {
  assert.equal(designSystemRow(false, { active: true }), undefined);
  assert.equal(designSystemRow(false, undefined), undefined);
});

test('a development build draws the Design system row only when a link was supplied', () => {
  assert.equal(designSystemRow(true, undefined), undefined);
  assert.deepEqual(designSystemRow(true, { active: false }), { active: false });
});
