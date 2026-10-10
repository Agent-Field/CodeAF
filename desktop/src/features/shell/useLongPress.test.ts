import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { contextMenuInit } from '../../components/ui/contextMenuEvent.ts';
import { holdOpensMenu, longPressDelayMs, longPressIgnore, longPressTargets } from './useLongPress.ts';

const desktop = fileURLToPath(new URL('../../..', import.meta.url));

test('the hold waits the interaction delay and only on a finger or a coarse pointer', () => {
  assert.equal(longPressDelayMs, 500);
  const open = { button: 0, coarse: true, touch: false, onClose: false, onTarget: true };
  assert.equal(holdOpensMenu(open), true);
  assert.equal(holdOpensMenu({ ...open, coarse: false, touch: true }), true);
  assert.equal(holdOpensMenu({ ...open, coarse: false, touch: false }), false);
  assert.equal(holdOpensMenu({ ...open, button: 2 }), false);
  assert.equal(holdOpensMenu({ ...open, onClose: true }), false);
  assert.equal(holdOpensMenu({ ...open, onTarget: false }), false);
});

test('a hold aims at tabs, group labels, and place rows, and not at their close marks', () => {
  assert.match(longPressTargets, /\.workspace-tabstrip \.workspace-tab\b/);
  assert.match(longPressTargets, /\.workspace-group-label\b/);
  assert.match(longPressTargets, /\.place-rail \.rail-row\b/);
  assert.match(longPressIgnore, /\.workspace-tab-close\b/);
  assert.match(longPressIgnore, /\.rail-row-close\b/);
});

test('the synthetic contextmenu is the same point Shift+F10 uses', () => {
  assert.deepEqual(contextMenuInit({ left: 10, width: 40, bottom: 80 }), {
    bubbles: true, cancelable: true, button: 2, clientX: 30, clientY: 80,
  });
  const menu = readFileSync(`${desktop}/src/components/ui/Menu.tsx`, 'utf8');
  const hook = readFileSync(`${desktop}/src/features/shell/useLongPress.ts`, 'utf8');
  assert.match(menu, /synthesizeContextMenu\(event\.currentTarget\)/);
  assert.match(hook, /synthesizeContextMenu\(target\)/);
  const shared = readFileSync(`${desktop}/src/components/ui/contextMenuEvent.ts`, 'utf8');
  assert.match(shared, /button: 2, clientX: bounds\.left \+ bounds\.width \/ 2, clientY: bounds\.bottom/);
});
