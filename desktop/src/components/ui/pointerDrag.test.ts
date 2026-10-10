import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { firstAcceptingTarget, pointerDragThresholdPx, stepPointerDrag } from './pointerDrag.ts';

test('the threshold is the token, and a shorter move stays a click', () => {
  assert.equal(pointerDragThresholdPx, 4);
  assert.deepEqual(stepPointerDrag('idle', 'down', { button: 1 }), { phase: 'idle', effect: 'none' });
  assert.deepEqual(stepPointerDrag('idle', 'down', { button: 2 }), { phase: 'idle', effect: 'none' });
  assert.deepEqual(stepPointerDrag('idle', 'down', { button: 0 }), { phase: 'armed', effect: 'none' });
  assert.deepEqual(stepPointerDrag('armed', 'move', { distance: 3 }), { phase: 'armed', effect: 'none' });
  assert.deepEqual(stepPointerDrag('armed', 'up'), { phase: 'idle', effect: 'none' });
  assert.deepEqual(stepPointerDrag('armed', 'escape'), { phase: 'idle', effect: 'none' });
  assert.deepEqual(stepPointerDrag('armed', 'cancel'), { phase: 'idle', effect: 'none' });
});

test('past the threshold a release drops and Escape or cancel aborts', () => {
  assert.deepEqual(stepPointerDrag('armed', 'move', { distance: 4 }), { phase: 'dragging', effect: 'begin' });
  assert.deepEqual(stepPointerDrag('dragging', 'move', { distance: 20 }), { phase: 'dragging', effect: 'none' });
  assert.deepEqual(stepPointerDrag('dragging', 'up'), { phase: 'idle', effect: 'drop' });
  assert.deepEqual(stepPointerDrag('dragging', 'escape'), { phase: 'idle', effect: 'abort' });
  assert.deepEqual(stepPointerDrag('dragging', 'cancel'), { phase: 'idle', effect: 'abort' });
});

test('hit testing skips the ghost and lets a rejecting child fall through to its parent', () => {
  const ghost = { id: 'ghost' };
  const child = { id: 'child' };
  const parent = { id: 'parent' };
  const parentOf = (node: { id: string }) => node.id === 'child' ? parent : undefined;
  const accept = new Map([['child', true], ['parent', true]]);
  const lookup = (node: { id: string }) => accept.has(node.id) ? { accepts: accept.get(node.id)! } : undefined;
  const isGhost = (node: { id: string }) => node.id === 'ghost';
  assert.equal(firstAcceptingTarget([ghost, child], parentOf, lookup, isGhost), child);
  accept.set('child', false);
  assert.equal(firstAcceptingTarget([child], parentOf, lookup, isGhost), parent);
  accept.set('parent', false);
  assert.equal(firstAcceptingTarget([child], parentOf, lookup, isGhost), undefined);
});

test('in-app drags do not use the HTML5 drag attributes', () => {
  const here = dirname(fileURLToPath(import.meta.url));
  const root = join(here, '..', '..');
  const hits: string[] = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) { walk(path); continue; }
      if (!/\.tsx?$/.test(name)) continue;
      const rel = relative(root, path);
      if (rel === 'features/conversation/composer/useFileDrop.ts' || rel === 'components/ui/pointerDrag.test.ts') continue;
      const text = readFileSync(path, 'utf8');
      if (/\bdraggable\b/.test(text) || /onDragStart/.test(text) || /dataTransfer\.setData/.test(text)) hits.push(rel);
    }
  };
  walk(root);
  assert.deepEqual(hits, []);
});
