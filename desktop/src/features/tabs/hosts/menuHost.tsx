// Menu host (owned by the menus-and-closing lane): the tab menu, the group label menu and the strip's overflow menu.
// Design 3g "Right-click: tab menu with a submenu, group label menu" and 3l "Right-click, the explicit path".
import type { ReactElement } from 'react';
import { ContextMenu, type MenuEntry } from '../../../components/ui';
import { copyLinkShortcut, tabShortcuts } from '../../../design/keyboard';
import { closeShortcutFor, closeStopShortcut, newGroupShortcut } from '../closing/shortcuts';
import type { TabsApi } from '../context';
import { kindDef } from '../kinds/registry';
import { focusedPane, splitCapacity, visibleTabs, type Tab, type TabGroup } from '../model';

const separator = (id: string): MenuEntry => ({ kind: 'separator', id });

/** "Open in split": the other plain tabs this one can be merged with. */
function splitEntry(api: TabsApi, tab: Tab): MenuEntry {
  const { state, dispatch } = api;
  const full = (tab.split?.panes.length ?? 1) >= splitCapacity;
  const partners = state.tabs.filter(other => other.id !== tab.id && !other.split && other.kind !== 'inbox');
  return {
    kind: 'submenu', id: 'split', label: 'Open in split', icon: 'split', disabled: full || !partners.length,
    items: partners.map(other => ({ id: other.id, label: other.title, icon: kindDef(other.kind).icon, onSelect: () => dispatch({ type: 'split-merge', id: tab.id, withId: other.id }) })),
  };
}

/** "Add to group": every group (the current one is checked; choosing it again leaves the group), then "New group…". */
function groupEntry(api: TabsApi, tab: Tab): MenuEntry {
  const { state, dispatch } = api;
  const groups: MenuEntry[] = state.groups.map(group => ({
    id: group.id, label: group.title, icon: 'layers', checked: tab.groupId === group.id,
    onSelect: () => dispatch({ type: 'move-group', id: tab.id, groupId: tab.groupId === group.id ? undefined : group.id }),
  }));
  return {
    kind: 'submenu', id: 'group', label: 'Add to group', icon: 'layers',
    items: [...groups, ...(groups.length ? [separator('groups-separator')] : []), { id: 'new-group', label: 'New group…', icon: 'plus', shortcut: newGroupShortcut, onSelect: () => dispatch({ type: 'group', id: tab.id }) }],
  };
}

/**
 * Copy link and Move to new window are present only where they can work. A capability that cannot work is ABSENT, not
 * disabled: a tab with nothing durable behind it has no link (links/deepLinks.ts), and a tab moves only in the desktop app.
 */
function transferEntries(api: TabsApi, tab: Tab): MenuEntry[] {
  const { actions } = api;
  const entries: MenuEntry[] = [];
  // The chord is Copy path on a file or diff tab (keyboard.ts), so those tabs show Copy link without it.
  const chord = ['file', 'diff'].includes(focusedPane(tab).kind) ? undefined : copyLinkShortcut;
  if (actions.linkFor(tab)) entries.push({ id: 'copy-link', label: 'Copy link', icon: 'link', shortcut: chord, onSelect: () => void actions.copyLink(tab) });
  if (actions.canMove(tab)) entries.push({ id: 'new-window', label: 'Move to new window', icon: 'appWindow', onSelect: () => void actions.moveToNewWindow(tab) });
  return entries.length ? [separator('link-separator'), ...entries] : [];
}

export function tabMenuItems(api: TabsApi, tab: Tab): MenuEntry[] {
  const { state, dispatch } = api;
  const toTheRight = visibleTabs(state).slice(visibleTabs(state).findIndex(t => t.id === tab.id) + 1).filter(t => !t.pinned && t.kind !== 'inbox');
  return [
    splitEntry(api, tab),
    groupEntry(api, tab),
    { id: 'pin', label: tab.pinned ? 'Unpin tab' : 'Pin tab', icon: 'pin', onSelect: () => dispatch({ type: 'pin', id: tab.id }) },
    { id: 'duplicate', label: 'Duplicate', icon: 'copy', onSelect: () => dispatch({ type: 'duplicate', id: tab.id }) },
    { id: 'rename', label: 'Rename tab', icon: 'pencil', onSelect: () => api.startRename(tab.id) },
    ...(tab.split ? [{ id: 'unmerge', label: 'Separate split', onSelect: () => dispatch({ type: 'split-unmerge', id: tab.id }) }] : []),
    ...transferEntries(api, tab),
    separator('close-separator'),
    { id: 'close', label: 'Close tab', shortcut: closeShortcutFor(tab.kind), onSelect: () => api.closeTab(tab.id) },
    // The explicit path for people who never hold Option (3l). Nothing running means nothing to stop, so it is not offered.
    ...(api.isRunning(tab) ? [{ id: 'close-stop', label: 'Close and stop', shortcut: closeStopShortcut, onSelect: () => api.closeAndStop(tab.id) }] : []),
    { id: 'close-others', label: 'Close other tabs', disabled: !state.tabs.some(t => t.id !== tab.id && !t.pinned && t.kind !== 'inbox'), onSelect: () => dispatch({ type: 'close-others', id: tab.id }) },
    { id: 'close-right', label: 'Close tabs to the right', disabled: !toTheRight.length, onSelect: () => dispatch({ type: 'close-right', id: tab.id }) },
  ];
}

/** The Inbox is the pinned tab that is always there: it has no menu. */
export const withTabMenu = (api: TabsApi, tab: Tab, node: ReactElement): ReactElement => (
  tab.kind === 'inbox' ? node : <ContextMenu key={tab.id} wide label={`Actions for ${tab.title}`} items={tabMenuItems(api, tab)}>{node}</ContextMenu>
);

export function groupMenuItems(api: TabsApi, group: TabGroup): MenuEntry[] {
  const members = api.state.tabs.filter(t => t.groupId === group.id);
  return [
    { id: 'rename', label: 'Rename', icon: 'pencil', onSelect: () => api.startRename(group.id, true) },
    { id: 'split', label: 'Open as split', icon: 'grid', disabled: members.filter(t => !t.split && !t.pinned).length < 2, onSelect: () => api.dispatch({ type: 'split-group', groupId: group.id }) },
    { id: 'collapse', label: group.collapsed ? 'Expand' : 'Collapse', icon: group.collapsed ? 'expand' : 'shrink', onSelect: () => api.dispatch({ type: 'collapse-group', id: group.id }) },
    { id: 'ungroup', label: 'Ungroup', icon: 'layers', onSelect: () => api.dispatch({ type: 'ungroup', id: group.id }) },
    separator('close-separator'),
    { id: 'close-all', label: `Close ${members.length} ${members.length === 1 ? 'tab' : 'tabs'}`, danger: true, onSelect: () => api.dispatch({ type: 'close-group', id: group.id }) },
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
    separator('tabs-separator'),
    ...state.tabs.map(tab => ({ id: tab.id, label: tab.title, checked: tab.id === state.activeId, onSelect: () => dispatch({ type: 'select', id: tab.id }) })),
  ];
}
