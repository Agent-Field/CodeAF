// Drag host (owned by the split lane): which tab is in flight, what dropping it on a tab or a group label
// does (design 2g: "split zones in the content, group target in the strip"), and the edge drops that PaneGrid draws.
import { useSyncExternalStore, type DragEvent, type KeyboardEvent } from 'react';
import type { TabsApi } from '../context';
import type { Tab, TabGroup } from '../model';
import { blockNeighbour, blockSlots } from '../reducers/groups';

export const tabDragType = 'application/codeaf-tab';
/** A whole group in flight, dragged by its label (Interactions, group label: "Drag moves the whole group"). */
export const groupDragType = 'application/codeaf-group';
const carriesTab = (event: DragEvent) => event.dataTransfer.types.includes(tabDragType);
const carriesGroup = (event: DragEvent) => event.dataTransfer.types.includes(groupDragType);
// The group in flight, for the same reason as `dragged`: a group never drops onto its own members or label.
let draggedGroup: string | null = null;

// The id of the tab being dragged. dataTransfer cannot be read while a drag is over something, so the
// content card reads it here to decide whether to draw its split zones.
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
const zoneAt = (event: DragEvent<HTMLElement>, groupable: boolean) => {
  const box = event.currentTarget.getBoundingClientRect();
  return dropZoneOf((event.clientX - box.left) / (box.width || 1), groupable);
};
const clearZone = (element: HTMLElement) => { delete element.dataset.drop; };

/**
 * Props for a tab's outer element: it can be dragged; dropping another tab on it groups (middle) or reorders (edges).
 * A whole group dropped on it goes before or after it (or, on a member of another group, before or after that group):
 * groups never nest, so a group has no middle zone.
 */
export function tabDragProps(api: TabsApi, tab: Tab) {
  return {
    draggable: true,
    onDragStart: (event: DragEvent) => { event.dataTransfer.setData(tabDragType, tab.id); event.dataTransfer.effectAllowed = 'move'; setDragged(tab.id); },
    onDragEnd: () => setDragged(null),
    onDragOver: (event: DragEvent<HTMLElement>) => {
      const group = carriesGroup(event);
      if (group ? !draggedGroup || tab.groupId === draggedGroup : !carriesTab(event) || dragged === tab.id) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = 'move';
      event.currentTarget.dataset.drop = zoneAt(event, !group && !tab.pinned);
    },
    onDragLeave: (event: DragEvent<HTMLElement>) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) clearZone(event.currentTarget); },
    onDrop: (event: DragEvent<HTMLElement>) => {
      const groupId = event.dataTransfer.getData(groupDragType);
      const id = event.dataTransfer.getData(tabDragType);
      const zone = zoneAt(event, !groupId && !tab.pinned);
      clearZone(event.currentTarget);
      setDragged(null);
      draggedGroup = null;
      if (groupId) {
        event.preventDefault();
        if (tab.groupId !== groupId) api.dispatch({ type: 'move-group-block', id: groupId, targetId: tab.id, after: zone === 'after' });
        return;
      }
      if (!id || id === tab.id) return;
      event.preventDefault();
      if (zone !== 'group') api.dispatch({ type: 'reorder', id, targetId: tab.id, after: zone === 'after' });
      else if (tab.groupId) api.dispatch({ type: 'move-group', id, groupId: tab.groupId });
      else api.dispatch({ type: 'group', id: tab.id, ids: [id] });
    },
  };
}

/**
 * Props for a group label. Dragging it carries the whole group. Dropping a tab on it moves the tab into the group and
 * opens it; dropping another group on it puts that group just before this one.
 */
export function groupDragProps(api: TabsApi, group: TabGroup, announce?: (message: string) => void) {
  return {
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
    draggable: true,
    onDragStart: (event: DragEvent) => { event.dataTransfer.setData(groupDragType, group.id); event.dataTransfer.effectAllowed = 'move'; draggedGroup = group.id; },
    onDragEnd: () => { draggedGroup = null; },
    onDragOver: (event: DragEvent) => {
      if (carriesTab(event) || (carriesGroup(event) && draggedGroup && draggedGroup !== group.id)) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; }
    },
    onDrop: (event: DragEvent) => {
      const groupId = event.dataTransfer.getData(groupDragType);
      const id = event.dataTransfer.getData(tabDragType);
      setDragged(null);
      draggedGroup = null;
      if (groupId) { event.preventDefault(); if (groupId !== group.id) api.dispatch({ type: 'move-group-block', id: groupId, targetId: group.id }); }
      else if (id) { event.preventDefault(); api.dispatch({ type: 'move-group', id, groupId: group.id }); }
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
