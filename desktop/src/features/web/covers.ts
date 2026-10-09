// A native web view draws above the whole DOM, so anything of the app's that
// opens over a web pane (a menu, a hover card, a tooltip, the overview, the
// palette, a dialog, a toast) would be drawn UNDER the page. The rule: while
// any such surface overlaps the pane's sheet, the view is hidden. This module
// only answers "is this rectangle covered"; views.ts acts on it.

export type Box = { x: number; y: number; width: number; height: number };

/**
 * Everything that floats. Roles cover the shared Menu, HoverPreview, Tooltip,
 * Select and every dialog; `data-native-cover` lets another lane mark a
 * surface of its own (a toast, the tray's sheet) without editing this list.
 */
export const COVER_SELECTOR = [
  '[role="menu"]', '[role="dialog"]', '[role="alertdialog"]', '[role="listbox"]', '[role="tooltip"]',
  'dialog[open]', '.app-menu', '.hover-preview', '[data-radix-popper-content-wrapper]', '[data-native-cover]',
].join(', ');

export function overlaps(a: Box, b: Box): boolean {
  return a.width > 0 && a.height > 0 && b.width > 0 && b.height > 0
    && a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
}

/** The boxes of every floating surface now in the document, except those inside `own` (the pane's own chrome). */
export function coverBoxes(own?: Element | null): Box[] {
  const boxes: Box[] = [];
  for (const element of document.querySelectorAll(COVER_SELECTOR)) {
    if (own?.contains(element)) continue;
    // A modal dialog dims the whole window behind it, so its cover is the window.
    if (isModal(element)) {
      boxes.push({ x: 0, y: 0, width: window.innerWidth, height: window.innerHeight });
      continue;
    }
    const r = element.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) boxes.push({ x: r.left, y: r.top, width: r.width, height: r.height });
  }
  return boxes;
}

function isModal(element: Element): boolean {
  try {
    return element.matches(':modal');
  } catch {
    return element.getAttribute('aria-modal') === 'true';
  }
}

export const isCovered = (sheet: Box, covers: Box[]): boolean => covers.some(cover => overlaps(sheet, cover));
