import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { previewMayOpen } from './previewOpen.ts';

const read = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8');

test('a screen that cannot hover never opens a preview, and a touch pointer does not either', () => {
  const open = { hoverNone: false, pointerType: 'mouse', disabled: false, pressed: false };
  assert.equal(previewMayOpen(open), true);
  assert.equal(previewMayOpen({ ...open, pointerType: 'pen' }), true);
  assert.equal(previewMayOpen({ ...open, hoverNone: true }), false);
  assert.equal(previewMayOpen({ ...open, pointerType: 'touch' }), false);
  assert.equal(previewMayOpen({ ...open, hoverNone: true, pointerType: 'mouse' }), false);
  assert.equal(previewMayOpen({ ...open, disabled: true }), false);
  assert.equal(previewMayOpen({ ...open, pressed: true }), false);
});

test('the trigger asks matchMedia for hover none before it opens, including keyboard focus', () => {
  const source = read('usePreviewTrigger.ts');
  assert.match(source, /window\.matchMedia\('\(hover: none\)'\)/);
  assert.match(source, /previewMayOpen\(\{ hoverNone: hoverNone\(\), pointerType: event\.pointerType/);
  assert.match(source, /previewMayOpen\(\{ hoverNone: hoverNone\(\), pointerType: 'mouse'/);
  assert.match(source, /if \(!hoverNone\(\)\) store\.open\(id\)/);
});

test('the preview card uses 8px collision padding and caps at min(300px, 100vw - 16px) from 600px down', () => {
  const tokens = JSON.parse(read('../../../design/tokens.json')) as { foundation: Record<string, string>; breakpoints: { small: number } };
  assert.equal(tokens.foundation['preview-collision-padding'], '8px');
  assert.equal(tokens.foundation['preview-card-width'], '300px');
  assert.equal(tokens.breakpoints.small, 600);
  const css = read('preview.css');
  assert.match(css, /@media \(max-width: 600px\)/);
  assert.match(css, /max-width: min\(var\(--preview-card-width\), calc\(100vw - var\(--preview-collision-padding\) \* 2\)\)/);
  const trigger = read('usePreviewTrigger.ts');
  assert.match(trigger, /Number\.parseInt\(design\.foundation\['preview-collision-padding'\], 10\)/);
  const host = read('../hosts/previewHost.tsx');
  assert.match(host, /collisionPadding=\{previewCollisionPadding\}/);
});
