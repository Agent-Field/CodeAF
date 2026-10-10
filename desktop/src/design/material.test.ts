import test from 'node:test';
import assert from 'node:assert/strict';
import { resolveMaterial, type MaterialInputs } from './material.ts';

const base: MaterialInputs = { desktop: false, platform: 'Linux', active: true, reducedTransparency: false, moreContrast: false, backdropFilter: true, blurOpen: false };
const m = (over: Partial<MaterialInputs>) => resolveMaterial({ ...base, ...over });

test('a browser with backdrop-filter gets glass, without it solid', () => {
  assert.equal(m({}), 'glass');
  assert.equal(m({ backdropFilter: false }), 'solid');
});
test('macOS and Windows desktops are native', () => {
  assert.equal(m({ desktop: true, platform: 'MacIntel' }), 'native');
  assert.equal(m({ desktop: true, platform: 'Win32' }), 'native');
});
test('Linux desktop is solid', () => assert.equal(m({ desktop: true, platform: 'Linux x86_64' }), 'solid'));
test('an inactive window is solid in every environment', () => {
  for (const env of [{}, { desktop: true, platform: 'MacIntel' }]) assert.equal(m({ ...env, active: false }), 'solid');
});
test('reduced transparency, more contrast and an open blur layer force solid', () => {
  for (const over of [{ reducedTransparency: true }, { moreContrast: true }, { blurOpen: true }]) {
    assert.equal(m(over), 'solid');
    assert.equal(m({ ...over, desktop: true, platform: 'MacIntel' }), 'solid');
  }
});
