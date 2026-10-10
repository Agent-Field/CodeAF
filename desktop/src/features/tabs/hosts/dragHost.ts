// Drag host: which tab is in flight, what dropping it on a tab or a group label does (design 2g: "split zones
// in the content, group target in the strip"), the edge drops that PaneGrid draws, and a release outside the
// window, which opens that tab in a new window (Interactions, "Dragging a tab out of the strip makes a new window").
// The gesture is the shared pointer drag. A press on the tab's button is the start; WebKit never opens an
// HTML5 drag from that button.
import { useRef, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type Ref } from 'react';
import { useSyncExternalStore } from 'react';
import { useDropTarget, usePointerDrag, type DragPayload, type DragPoint } from '../../../components/ui/usePointerDrag';
import { webOverlayDragEnd, webOverlayDragStart } from '../../../lib/native/webOverlay';
import type { TabsApi } from '../context';
import type { Tab, TabGroup } from '../model';
import { blockNeighbour, blockSlots } from '../reducers/groups';
import { mayTearOff, tabMoveToWindow, tearOffAt } from './tearOff';

export const tabDragType = 'application/codeaf-tab';
/** A whole group in flight, dragged by its label (Interactions, group label: "Drag moves the whole group"). */
export const groupDragType = 'application/codeaf-group';

// The id of the tab being dragged. The content card reads it to decide whether to draw its split zones.
let dragged: string | null = null;
const listeners = new Set<() => void>();
const setDragged = (id: string | null) => { if (dragged !== id) { dragged = id; listeners.forEach(listener => listener()); } };
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
/** The id of the tab in flight, or null. */
export const useDraggedTab = () => useSyncExternalStore(subscribe, () => dragged, () => null);
export const endTabDrag = () => setDragged(null);

/** Where on a tab a drop lands: the outer quarters reorder, the middle half groups (the "Group" target). */
export type TabDropZone = 'before' | 'group' | 'after';
export const dropZoneOf = (fraction: number, groupable: boolean): TabDropZone => {
  if (!groupable) return fraction < 0.5 ? 'before' : 'after';
  return fraction < 0.25 ? 'before' : fraction > 0.75 ? 'after' : 'group';
};
const zoneAt = (point: DragPoint, element: HTMLElement, groupable: boolean) => {
  const box = element.getBoundingClientRect();
  return dropZoneOf((point.x - box.left) / (box.width || 1), groupable);
};

function tabPayload(payload: DragPayload): string | undefined {
  return payload.kind === 'tab' ? payload.id : undefined;
}
function groupPayload(payload: DragPayload): string | undefined {
  return payload.kind === 'group' ? payload.id : undefined;
}

/**
 * Props for a tab's outer element. Dropping another tab on it groups (middle) or reorders (edges).
 * A whole group dropped on it goes before or after it. Groups never nest, so a group has no middle zone.
 * A release no target accepted, past the window edge, tears the tab off.
 */
export function useTabDrag(api: TabsApi, tab: Tab): { ref: Ref<HTMLDivElement>; onPointerDown: (event: ReactPointerEvent<HTMLElement>) => void } {
  const el = useRef<HTMLDivElement | null>(null);
  const pointer = usePointerDrag({
    payload: { kind: 'tab', id: tab.id },
    onStart: () => { setDragged(tab.id); webOverlayDragStart([tabDragType]); },
    onEnd: () => { setDragged(null); webOverlayDragEnd(); },
    onMiss: (point) => {
      const at = tearOffAt({ dropEffect: 'none', clientX: point.x, clientY: point.y, screenX: point.screenX, screenY: point.screenY }, { width: window.innerWidth, height: window.innerHeight });
      if (!at || !mayTearOff(tab, api.actions.canMove(tab))) return;
      tabMoveToWindow(tab, () => { void api.actions.moveToNewWindow(tab, at); });
    },
  });
  const dropRef = useDropTarget({
    kind: 'tab',
    id: tab.id,
    accepts: (payload) => {
      const groupId = groupPayload(payload);
      if (groupId) return tab.groupId !== groupId;
      const id = tabPayload(payload);
      return !!id && id !== tab.id;
    },
    hover: (point, payload) => {
      if (!el.current) return;
      el.current.dataset.drop = zoneAt(point, el.current, !groupPayload(payload) && !tab.pinned);
    },
    leave: () => { if (el.current) delete el.current.dataset.drop; },
    drop: (point, payload) => {
      const host = el.current;
      const zone = host ? zoneAt(point, host, !groupPayload(payload) && !tab.pinned) : 'after';
      if (host) delete host.dataset.drop;
      const groupId = groupPayload(payload);
      if (groupId) {
        if (tab.groupId !== groupId) api.dispatch({ type: 'move-group-block', id: groupId, targetId: tab.id, after: zone === 'after' });
        return true;
      }
      const id = tabPayload(payload);
      if (!id || id === tab.id) return false;
      if (zone !== 'group') api.dispatch({ type: 'reorder', id, targetId: tab.id, after: zone === 'after' });
      else if (tab.groupId) api.dispatch({ type: 'move-group', id, groupId: tab.groupId });
      else api.dispatch({ type: 'group', id: tab.id, ids: [id] });
      return true;
    },
  });
  return {
    onPointerDown: pointer.onPointerDown,
    ref: (node) => { el.current = node; dropRef(node); },
  };
}

/**
 * Props for a group label. Dragging it carries the whole group. Dropping a tab on it moves the tab into the group;
 * dropping another group on it puts that group just before this one.
 */
export function useGroupDrag(api: TabsApi, group: TabGroup, announce?: (message: string) => void): { ref: Ref<HTMLButtonElement>; onPointerDown: (event: ReactPointerEvent<HTMLElement>) => void; onKeyDown: (event: KeyboardEvent) => void } {
  const pointer = usePointerDrag({
    payload: { kind: 'group', id: group.id },
    onStart: () => webOverlayDragStart([groupDragType]),
    onEnd: () => webOverlayDragEnd(),
  });
  const dropRef = useDropTarget({
    kind: 'group',
    id: group.id,
    accepts: (payload) => !!tabPayload(payload) || (!!groupPayload(payload) && groupPayload(payload) !== group.id),
    hover: () => {},
    leave: () => {},
    drop: (_point, payload) => {
      const groupId = groupPayload(payload);
      if (groupId) {
        if (groupId !== group.id) api.dispatch({ type: 'move-group-block', id: groupId, targetId: group.id });
        return true;
      }
      const id = tabPayload(payload);
      if (!id) return false;
      api.dispatch({ type: 'move-group', id, groupId: group.id });
      return true;
    },
  });
  return {
    onPointerDown: pointer.onPointerDown,
    ref: dropRef,
    // Alt+Shift+←/→ moves the whole block one slot (S-3g-18) and says where it went, since nothing else tells a screen reader.
    onKeyDown: (event: KeyboardEvent) => {
      if (!event.altKey || !event.shiftKey || (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight')) return;
      event.preventDefault();
      const dir = event.key === 'ArrowLeft' ? -1 : 1;
      const targetId = blockNeighbour(api.state.tabs, group.id, dir);
      if (!targetId) return;
      api.dispatch({ type: 'move-group-block', id: group.id, targetId, after: dir > 0 });
      const slots = blockSlots(api.state.tabs);
      announce?.(`Moved group ${group.title} to position ${slots.indexOf(group.id) + dir + 1} of ${slots.length}`);
    },
  };
}

/** The edges of the content card that take a dropped tab, and what each does to the split. */
export type EdgeZone = 'left' | 'right' | 'bottom';
export const edgeZones: readonly EdgeZone[] = ['left', 'right', 'bottom'];
export const edgeLabel: Record<EdgeZone, string> = { left: 'Split left', right: 'Split right', bottom: 'Split down' };
export const edgeMerge = (zone: EdgeZone) => ({ at: zone === 'left' ? 'start' : 'end', layout: zone === 'bottom' ? '2x1' : '1x2' }) as const;

/** Whether a tab being dragged can be dropped into `host`'s content: both unpinned, different, and within four panes. */
export function canSplitInto(host: Tab, guest: Tab | undefined, capacity: number): boolean {
  if (!guest || guest.id === host.id || guest.pinned || host.pinned) return false;
  return (host.split?.panes.length ?? 1) + (guest.split?.panes.length ?? 1) <= capacity;
}
