// A native web view paints above the DOM, so a menu, preview, tooltip, toast,
// overview, drag zone, Quick Look or dialog that meets a sheet would slide
// under the page. covers.ts finds the shared surfaces by role. This registry
// is the other door: a lane that owns one of those surfaces calls
// registerOverlay with the element (or a rectangle) and views.ts treats the
// box as a cover. The existing overlay hold then hides that page and the
// release shows it again. A box that misses every sheet is ignored.
//
// The integrator adds the one call at each host (Menu, HoverPreview, Tooltip,
// and the overview, toast and split-zone hosts). Until those land, the role
// scan in covers.ts still hides the same surfaces.

import { overlaps, type Box } from './covers.ts';

export type OverlayRect = Box;

/** Anything with a viewport rectangle. A DOM element qualifies; tests pass a stub. */
export type OverlayElement = {
  getBoundingClientRect(): { x: number; y: number; width: number; height: number };
  isConnected?: boolean;
};

export type OverlayTarget = OverlayRect | OverlayElement;

type Entry = { target: OverlayTarget };

let nextId = 1;
const entries = new Map<number, Entry>();
const listeners = new Set<() => void>();

const notify = () => listeners.forEach(listener => listener());

function isElement(target: OverlayTarget): target is OverlayElement {
  return typeof (target as OverlayElement).getBoundingClientRect === 'function';
}

/** A positive rectangle, or nothing. A detached element and a zero box do not cover a page. */
function boxOf(target: OverlayTarget): Box | null {
  if (!isElement(target)) return target.width > 0 && target.height > 0 ? { x: target.x, y: target.y, width: target.width, height: target.height } : null;
  if (target.isConnected === false) return null;
  const rect = target.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return null;
  return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
}

export function subscribeOcclusion(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * Hold this overlay until the returned function runs. A rectangle is read as
 * given. An element is measured when the sheets are, so a menu that moves
 * still meets the page. Calling the release twice does nothing.
 */
export function registerOverlay(target: OverlayTarget): () => void {
  const id = nextId++;
  entries.set(id, { target });
  notify();
  let released = false;
  return () => {
    if (released) return;
    released = true;
    if (entries.delete(id)) notify();
  };
}

/** Boxes of every live registration, in registration order. */
export function overlayBoxes(): Box[] {
  const boxes: Box[] = [];
  for (const entry of entries.values()) {
    const box = boxOf(entry.target);
    if (box) boxes.push(box);
  }
  return boxes;
}

export type SheetRect = { pane: string; rect: Box };

/** Panes whose sheet meets a registered overlay. A sheet that misses every overlay is absent. */
export function panesHiddenByOverlays(sheets: readonly SheetRect[]): string[] {
  const boxes = overlayBoxes();
  return sheets.filter(sheet => boxes.some(box => overlaps(sheet.rect, box))).map(sheet => sheet.pane);
}

/** Drops every registration. Tests start from an empty window. */
export function resetOcclusionForTests(): void {
  entries.clear();
  listeners.clear();
  nextId = 1;
}
