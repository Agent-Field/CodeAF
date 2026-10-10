// Tear-off (Interactions, Flows "Dragging a tab out of the strip makes a new window").
// A release that nobody accepted and that landed outside this window opens another window on the same
// place, focused on that tab. A drop the strip, a group or a split zone accepted stays a reorder, a group
// or a split. Letting go still inside the window, even on chrome that accepts nothing, is a cancelled drag.
//
// The chip stays in this strip. Both windows on a place show one tab set, so closing it here would close it
// in the new window too. The window door moves only this window's focus off the tab when it was the one on
// screen, and it never stops the work. Pinned tabs and the Inbox stay put. A browser has no second window.
import type { Tab } from '../model.ts';

/** Where the pointer was when the drag ended. `dropEffect` is none when no target accepted the drop. */
export type DragRelease = {
  dropEffect: string;
  clientX: number;
  clientY: number;
  screenX: number;
  screenY: number;
};

/** This window's client box. A point in it is still inside, even when nothing there accepted the drop. */
export type WindowClientBox = { width: number; height: number };

/** The new window's top-left, in logical screen coordinates. Negative is a display to the left. */
export type TearOffAt = { x: number; y: number };

/**
 * The drop point when this release is a tear-off, or undefined when it is not.
 * Both gates are required: a refused drop inside the window is a cancel, and a pointer that left
 * the window onto something that accepted the drag (dropEffect move, copy or link) is not a new window.
 * The edge of the client box is outside. A non-finite coordinate opens nothing.
 */
export function tearOffAt(end: DragRelease, box: WindowClientBox): TearOffAt | undefined {
  if (end.dropEffect !== 'none') return undefined;
  if (!Number.isFinite(end.clientX) || !Number.isFinite(end.clientY) || !Number.isFinite(end.screenX) || !Number.isFinite(end.screenY)) return undefined;
  if (!Number.isFinite(box.width) || !Number.isFinite(box.height) || box.width <= 0 || box.height <= 0) return undefined;
  const inside = end.clientX >= 0 && end.clientY >= 0 && end.clientX < box.width && end.clientY < box.height;
  if (inside) return undefined;
  return { x: end.screenX, y: end.screenY };
}

/**
 * True when this tab may leave for a new window. `canMove` is the desktop door (a browser, the Inbox and a
 * place Home are already false). A pinned tab can still move from the menu; a drag out of the strip does not take it.
 */
export function mayTearOff(tab: Pick<Tab, 'pinned' | 'kind'>, canMove: boolean): boolean {
  return canMove && !tab.pinned;
}

/** The drag's one call into the window door. The caller supplies the place and the drop point. */
export function tabMoveToWindow(tab: Tab, move: (tab: Tab) => void | Promise<unknown>): void {
  void move(tab);
}

/** The fields a dragend carries that decide a tear-off. A missing transfer counts as a refused drop. */
export function dragRelease(event: { dataTransfer: { dropEffect: string } | null; clientX: number; clientY: number; screenX: number; screenY: number }): DragRelease {
  return {
    dropEffect: event.dataTransfer?.dropEffect ?? 'none',
    clientX: event.clientX,
    clientY: event.clientY,
    screenX: event.screenX,
    screenY: event.screenY,
  };
}
