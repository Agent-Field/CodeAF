// Keyboard and native-menu wiring for the workspace (owned by the rail-and-keys lane). Every shortcut
// is matched through design/keyboard.ts; this hook only decides what each one does to the workspace.
import { useEffect, type Dispatch, type MutableRefObject } from 'react';
import { desktopTabEvent, isDesktopTabAction } from '../../lib/desktopTabs';
import { isOverviewShortcut, sequentialTabDirection, tabActionShortcut } from '../../design/keyboard';
import type { Tab, WorkspaceAction, WorkspaceState } from './model';

export type Switcher = { ids: string[]; index: number } | null;

type KeyOptions = {
  enabled: boolean; state: WorkspaceState; dispatch: Dispatch<WorkspaceAction>; visible: readonly Tab[];
  overviewOpen: boolean; setOverviewOpen: (update: (open: boolean) => boolean) => void;
  closeTab: (id: string) => void;
  /** The held-modifier recent-tab switcher: a ref the key handlers read synchronously, and its React state. */
  switcherRef: MutableRefObject<Switcher>; setSwitcher: (value: Switcher) => void;
};

export function useTabKeys({ enabled, state, dispatch, visible, overviewOpen, setOverviewOpen, closeTab, switcherRef, setSwitcher }: KeyOptions) {
  useEffect(() => {
    if (!enabled) { switcherRef.current = null; setSwitcher(null); return; }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && switcherRef.current) { event.preventDefault(); switcherRef.current = null; setSwitcher(null); return; }
      const modalOpen = !!document.querySelector('dialog[open]');
      if (isOverviewShortcut(event) && (!modalOpen || overviewOpen)) { event.preventDefault(); setOverviewOpen(open => !open); return; }
      if (!(event.metaKey || event.ctrlKey) || modalOpen) return;
      const direction = sequentialTabDirection(event);
      if (direction) {
        event.preventDefault();
        const index = visible.findIndex(tab => tab.id === state.activeId);
        dispatch({ type: 'select', id: visible[(index + direction + visible.length) % visible.length].id });
        return;
      }
      const key = event.key.toLowerCase();
      const action = tabActionShortcut(event);
      if (action) { event.preventDefault(); if (action === 'close') closeTab(state.activeId); else dispatch({ type: action }); return; }
      if (key === 'tab' && event.ctrlKey && !event.metaKey && !event.altKey) {
        event.preventDefault();
        const current = switcherRef.current;
        const ids = current?.ids ?? [state.activeId, ...state.recentIds.filter(id => id !== state.activeId)];
        const index = ((current?.index ?? 0) + (event.shiftKey ? -1 : 1) + ids.length) % ids.length;
        switcherRef.current = { ids, index }; setSwitcher(switcherRef.current);
      }
      if (/^[1-9]$/.test(key) && visible.length) {
        event.preventDefault(); dispatch({ type: 'select', id: visible[key === '9' ? visible.length - 1 : Math.min(Number(key) - 1, visible.length - 1)].id });
      }
    };
    const onRelease = (event: KeyboardEvent) => {
      if ((event.key === 'Control' || event.key === 'Meta') && switcherRef.current) {
        const current = switcherRef.current; dispatch({ type: 'select', id: current.ids[current.index] }); switcherRef.current = null; setSwitcher(null);
      }
    };
    const onBlur = () => { switcherRef.current = null; setSwitcher(null); };
    window.addEventListener('keydown', onKey); window.addEventListener('keyup', onRelease); window.addEventListener('blur', onBlur);
    return () => { window.removeEventListener('keydown', onKey); window.removeEventListener('keyup', onRelease); window.removeEventListener('blur', onBlur); };
  }, [enabled, overviewOpen, state.activeId, state.recentIds, visible]);
}

type MenuOptions = {
  state: WorkspaceState; dispatch: Dispatch<WorkspaceAction>; visible: readonly Tab[]; renaming: boolean;
  onActivate: () => void; closeTab: (id: string) => void; setOverviewOpen: (update: boolean | ((open: boolean) => boolean)) => void;
};

/** Native menu items arrive as a window event; they act on the workspace even from another page. */
export function useDesktopTabActions({ state, dispatch, visible, renaming, onActivate, closeTab, setOverviewOpen }: MenuOptions) {
  useEffect(() => {
    const onMenuAction = (event: Event) => {
      const action: unknown = (event as CustomEvent).detail;
      if (!isDesktopTabAction(action) || renaming) return;
      onActivate();
      if (action === 'overview') setOverviewOpen(open => !open);
      else if (action === 'close') { setOverviewOpen(false); closeTab(state.activeId); }
      else if (action === 'new' || action === 'reopen') { setOverviewOpen(false); dispatch({ type: action }); }
      else {
        setOverviewOpen(false);
        const index = visible.findIndex(tab => tab.id === state.activeId);
        dispatch({ type: 'select', id: visible[(index + (action === 'next' ? 1 : -1) + visible.length) % visible.length].id });
      }
    };
    window.addEventListener(desktopTabEvent, onMenuAction);
    return () => window.removeEventListener(desktopTabEvent, onMenuAction);
  }, [onActivate, renaming, state.activeId, visible]);
}
