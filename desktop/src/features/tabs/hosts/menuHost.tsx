// Menu host (owned by the menus-and-closing lane): the tab and group context menus and the strip's overflow menu.
import type { ReactElement } from 'react';
import { ContextMenu, type MenuEntry } from '../../../components/ui';
import { tabShortcuts } from '../../../design/keyboard';
import type { TabsApi } from '../context';
import type { Tab, TabGroup } from '../model';
import { isPlaceHome } from '../reducers/home';

export function tabMenuItems(api: TabsApi, tab: Tab): MenuEntry[] {
  const { state, dispatch } = api;
  // A place's Home is not an ordinary tab: it never closes, moves or groups, so its menu is the place's own.
  if (isPlaceHome(tab)) return api.placeMenu ?? [];
  const move: MenuEntry[] = api.moveToNewWindow ? [{ id: 'new-window', label: 'Move to new window', onSelect: () => api.moveToNewWindow?.(tab) }] : [];
  return [
    { id: 'rename', label: 'Rename tab', onSelect: () => api.startRename(tab.id) },
    { id: 'pin', label: tab.pinned ? 'Unpin tab' : 'Pin tab', icon: 'pin', onSelect: () => dispatch({ type: 'pin', id: tab.id }) },
    { kind: 'submenu', id: 'group', label: 'Move to group', icon: 'folder', items: [
      { id: 'new-group', label: 'Create group', onSelect: () => dispatch({ type: 'group', id: tab.id }) },
      ...state.groups.map(group => ({ id: group.id, label: group.title, checked: tab.groupId === group.id, onSelect: () => dispatch({ type: 'move-group', id: tab.id, groupId: group.id }) })),
      { id: 'no-group', label: 'No group', disabled: !tab.groupId, onSelect: () => dispatch({ type: 'move-group', id: tab.id }) },
    ] },
    ...(tab.split ? [{ id: 'unmerge', label: 'Separate split', onSelect: () => dispatch({ type: 'split-unmerge', id: tab.id }) }] : []),
    ...move,
    { kind: 'separator', id: 'close-separator' },
    { id: 'close', label: 'Close tab', icon: 'close', shortcut: tabShortcuts.close, onSelect: () => api.closeTab(tab.id) },
    { id: 'reopen', label: 'Reopen closed tab', shortcut: tabShortcuts.reopen, disabled: !state.closed.length, onSelect: () => dispatch({ type: 'reopen' }) },
    { kind: 'submenu', id: 'order', label: 'Move tab', disabled: state.tabs.length < 2, items: state.tabs.filter(t => t.id !== tab.id).map(target => ({ id: target.id, label: `Before ${target.title}`, onSelect: () => dispatch({ type: 'reorder', id: tab.id, targetId: target.id }) })) },
  ];
}

export const withTabMenu = (api: TabsApi, tab: Tab, node: ReactElement): ReactElement => {
  const items = tabMenuItems(api, tab);
  return items.length ? <ContextMenu key={tab.id} label={`Actions for ${tab.title}`} items={items}>{node}</ContextMenu> : node;
};

export function groupMenuItems(api: TabsApi, group: TabGroup): MenuEntry[] {
  return [
    { id: 'rename', label: 'Rename group', onSelect: () => api.startRename(group.id, true) },
    { id: 'add', label: 'New tab in group', icon: 'plus', onSelect: () => api.dispatch({ type: 'new', groupId: group.id }) },
    { id: 'split', label: 'Open as split', disabled: api.state.tabs.filter(t => t.groupId === group.id && !t.split && !t.pinned).length < 2, onSelect: () => api.dispatch({ type: 'split-group', groupId: group.id }) },
    { id: 'collapse', label: group.collapsed ? 'Expand group' : 'Collapse group', onSelect: () => api.dispatch({ type: 'collapse-group', id: group.id }) },
    { id: 'ungroup', label: 'Ungroup tabs', onSelect: () => api.dispatch({ type: 'ungroup', id: group.id }) },
  ];
}

export const withGroupMenu = (api: TabsApi, group: TabGroup, node: ReactElement): ReactElement => (
  <ContextMenu label={`Actions for group ${group.title}`} items={groupMenuItems(api, group)}>{node}</ContextMenu>
);

/** The "+N ⌄" menu: new and reopen, then every open tab so a scrolled-out tab is one pick away. */
export function overflowItems(api: TabsApi): MenuEntry[] {
  const { state, dispatch } = api;
  return [
    { id: 'new', label: 'New tab', icon: 'plus', shortcut: tabShortcuts.new, onSelect: () => dispatch({ type: 'new' }) },
    { id: 'reopen', label: 'Reopen closed tab', disabled: !state.closed.length, onSelect: () => dispatch({ type: 'reopen' }) },
    { kind: 'separator', id: 'tabs-separator' },
    ...state.tabs.map(tab => ({ id: tab.id, label: tab.title, checked: tab.id === state.activeId, onSelect: () => dispatch({ type: 'select', id: tab.id }) })),
  ];
}
