// Drag host (owned by the split lane for edge drops; grouping and reordering drags live here today).
import type { DragEvent } from 'react';
import type { TabsApi } from '../context';
import type { Tab, TabGroup } from '../model';

export const tabDragType = 'application/codeaf-tab';
const carriesTab = (event: DragEvent) => event.dataTransfer.types.includes(tabDragType);

/** Props for a tab's outer element: it can be dragged, and dropping another tab on it reorders. */
export function tabDragProps(api: TabsApi, tab: Tab) {
  return {
    draggable: true,
    onDragStart: (event: DragEvent) => { event.dataTransfer.setData(tabDragType, tab.id); event.dataTransfer.effectAllowed = 'move'; },
    onDragOver: (event: DragEvent) => { if (carriesTab(event)) event.preventDefault(); },
    onDrop: (event: DragEvent) => {
      const id = event.dataTransfer.getData(tabDragType);
      if (id) { event.preventDefault(); api.dispatch({ type: 'reorder', id, targetId: tab.id }); }
    },
  };
}

/** Props for a group label: dropping a tab on it moves the tab into the group and opens it. */
export function groupDropProps(api: TabsApi, group: TabGroup) {
  return {
    onDragOver: (event: DragEvent) => { if (carriesTab(event)) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; } },
    onDrop: (event: DragEvent) => {
      const id = event.dataTransfer.getData(tabDragType);
      if (id) { event.preventDefault(); api.dispatch({ type: 'move-group', id, groupId: group.id }); }
    },
  };
}
