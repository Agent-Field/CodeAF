import type { MouseEvent } from 'react';
import { Button, ContextMenu, type MenuEntry } from '../../../components/ui';
import { overviewShortcut, tabShortcuts } from '../../../design/keyboard';
import type { TabsApi } from '../context';

export function stripMenuItems(api: TabsApi, onOverview: () => void): MenuEntry[] {
  return [
    { id: 'new', label: 'New tab', shortcut: tabShortcuts.new, onSelect: () => api.dispatch({ type: 'new' }) },
    { id: 'reopen', label: 'Reopen closed tab', shortcut: tabShortcuts.reopen, disabled: !api.state.closed.length, onSelect: () => api.dispatch({ type: 'reopen' }) },
    { id: 'overview', label: 'Show all tabs', shortcut: overviewShortcut, onSelect: onOverview },
  ];
}

/** Blank frame space shares the spacer's menu without intercepting any tab or control's own menu. */
export function openStripBackgroundMenu(event: MouseEvent<HTMLDivElement>) {
  const target = event.target;
  if (!(target instanceof HTMLElement) || !target.matches('.workspace-tabbar, .workspace-tabstrip, .workspace-tab-actions')) return;
  const spacer = event.currentTarget.querySelector('.workspace-tab-spacer');
  if (!spacer) return;
  event.preventDefault();
  spacer.dispatchEvent(new window.MouseEvent('contextmenu', {
    bubbles: true, cancelable: true, button: 2, clientX: event.clientX, clientY: event.clientY,
  }));
}

export function StripMenu({ api, onOverview }: { api: TabsApi; onOverview: () => void }) {
  return <ContextMenu label="Tab strip actions" items={stripMenuItems(api, onOverview)}>
    <div className="workspace-tab-spacer" data-tauri-drag-region>
      {/* The keyboard door draws nothing, so the spacer stays native window drag space. */}
      <Button className="workspace-strip-note" aria-label="Tab strip actions" aria-haspopup="menu"/>
    </div>
  </ContextMenu>;
}
