import assert from 'node:assert/strict';
import test from 'node:test';

import { panesHiddenByOverlays, registerOverlay, resetOcclusionForTests, type OverlayElement, type SheetRect } from './occlusion.ts';

const page: SheetRect = { pane: 'page', rect: { x: 100, y: 80, width: 400, height: 300 } };
const other: SheetRect = { pane: 'other', rect: { x: 0, y: 0, width: 40, height: 20 } };

function element(rect: SheetRect['rect'], connected = true): OverlayElement {
  return { getBoundingClientRect: () => rect, isConnected: connected };
}

test('intersecting overlay hides, release shows, non-intersecting ignored', () => {
  resetOcclusionForTests();
  const release = registerOverlay({ x: 120, y: 100, width: 80, height: 40 });
  assert.deepEqual(panesHiddenByOverlays([page, other]), ['page']);
  release();
  assert.deepEqual(panesHiddenByOverlays([page, other]), []);

  const missed = registerOverlay({ x: 900, y: 900, width: 10, height: 10 });
  assert.deepEqual(panesHiddenByOverlays([page, other]), []);
  missed();

  const overPage = registerOverlay(element({ x: 110, y: 90, width: 20, height: 20 }));
  assert.deepEqual(panesHiddenByOverlays([page, other]), ['page']);
  overPage();
  overPage();
  assert.deepEqual(panesHiddenByOverlays([page, other]), []);

  // A node that has left the document, and a box with no area, are not overlays.
  registerOverlay(element({ x: 110, y: 90, width: 20, height: 20 }, false));
  registerOverlay({ x: 110, y: 90, width: 0, height: 20 });
  assert.deepEqual(panesHiddenByOverlays([page]), []);
});
