// Keyboard and native-menu wiring for the workspace (owned by the rail-and-keys lane). Every chord is decided by
// design/keyboard.ts (one registry for the whole shell); this hook only says what each one does to the workspace.
import { isOpenWebDetail, webOpenAction } from '../web/open.ts';
import { useEffect, useRef, type Dispatch, type MutableRefObject } from 'react';
import { desktopTabEvent, isDesktopTabAction } from '../../lib/desktopTabs';
import { shortcutLayer } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import { isOpenJobDetail, jobOpenAction } from '../jobs/open';
import { leaveKindAction, openKindAction, type ShellKind } from '../shell/openKind';
import { publishActiveKind, shellEvent, shellLeaveEvent } from '../shell/shellState';
import { kindDef } from './kinds/registry';
import { focusedPane, type Tab, type WorkspaceAction, type WorkspaceState } from './model';

export type Switcher = { ids: string[]; index: number } | null;

type KeyOptions = {
  enabled: boolean; state: WorkspaceState; dispatch: Dispatch<WorkspaceAction>; visible: readonly Tab[];
  overviewOpen: boolean; setOverviewOpen: (update: (open: boolean) => boolean) => void;
  closeTab: (id: string) => void;
  /** The held-modifier recent-tab switcher: a ref the key handlers read synchronously, and its React state. */
  switcherRef: MutableRefObject<Switcher>; setSwitcher: (value: Switcher) => void;
};

/** The tab `step` places away from the active one in the visible order, wrapping at both ends. */
const neighbour = (visible: readonly Tab[], activeId: string, step: number) => {
  const index = visible.findIndex(tab => tab.id === activeId);
  return visible[(index + step + visible.length) % visible.length];
};

export function useTabKeys({ enabled, state, dispatch, visible, overviewOpen, setOverviewOpen, closeTab, switcherRef, setSwitcher }: KeyOptions) {
  const active = state.tabs.find(tab => tab.id === state.activeId);
  useEffect(() => { if (active) publishActiveKind(focusedPane(active).kind); }, [active && focusedPane(active).kind]);

  useShortcuts(shortcutLayer.workspace, shortcut => {
    const modalOpen = !!document.querySelector('dialog[open]');
    if (shortcut.id === 'overview') {
      if (modalOpen && !overviewOpen) return false;
      setOverviewOpen(open => !open);
      return true;
    }
    if (modalOpen) return false;
    switch (shortcut.id) {
      case 'new': case 'reopen': dispatch({ type: shortcut.id }); return true;
      // ⌘G: the active tab and every ⌘-clicked tab become one new group, where the first of them stands.
      case 'group': dispatch({ type: 'group-picked' }); return true;
      // Pinned tabs never close with ⌘W (design 2h "Pinned").
      case 'close': if (!active?.pinned) closeTab(state.activeId); return true;
      case 'next': case 'previous': if (!visible.length) return false; dispatch({ type: 'select', id: neighbour(visible, state.activeId, shortcut.id === 'next' ? 1 : -1).id }); return true;
      case 'jump': {
        if (!visible.length) return false;
        dispatch({ type: 'select', id: visible[shortcut.index === 9 ? visible.length - 1 : Math.min(shortcut.index! - 1, visible.length - 1)].id });
        return true;
      }
      case 'history': {
        if (!kindDef('history').backed) return false;
        dispatch(openKindAction(state, 'history'));
        return true;
      }
      case 'switch': case 'switch-back': {
        const current = switcherRef.current;
        const ids = current?.ids ?? [state.activeId, ...state.recentIds.filter(id => id !== state.activeId)];
        const index = ((current?.index ?? 0) + (shortcut.id === 'switch-back' ? -1 : 1) + ids.length) % ids.length;
        switcherRef.current = { ids, index }; setSwitcher(switcherRef.current);
        return true;
      }
    }
    return false;
  }, enabled);

  useEffect(() => {
    if (!enabled) { switcherRef.current = null; setSwitcher(null); return; }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && switcherRef.current) { event.preventDefault(); switcherRef.current = null; setSwitcher(null); }
    };
    const onRelease = (event: KeyboardEvent) => {
      if ((event.key === 'Control' || event.key === 'Meta') && switcherRef.current) {
        const current = switcherRef.current; dispatch({ type: 'select', id: current.ids[current.index] }); switcherRef.current = null; setSwitcher(null);
      }
    };
    const onBlur = () => { switcherRef.current = null; setSwitcher(null); };
    window.addEventListener('keydown', onKey); window.addEventListener('keyup', onRelease); window.addEventListener('blur', onBlur);
    return () => { window.removeEventListener('keydown', onKey); window.removeEventListener('keyup', onRelease); window.removeEventListener('blur', onBlur); };
  }, [enabled]);
}

type MenuOptions = {
  state: WorkspaceState; dispatch: Dispatch<WorkspaceAction>; visible: readonly Tab[]; renaming: boolean;
  onActivate: () => void; closeTab: (id: string) => void; closeAndStop: (id: string) => void; setOverviewOpen: (update: boolean | ((open: boolean) => boolean)) => void;
};

/** Native menu items and the rail's "open this kind" request arrive as window events; they act on the workspace even from another page. */
export function useDesktopTabActions({ state, dispatch, visible, renaming, onActivate, closeTab, closeAndStop, setOverviewOpen }: MenuOptions) {
  // The listener below is re-bound when the strip changes, so a job open reads the tabs of this render plus any
  // open dispatched earlier in the same turn (React has not painted those yet).
  const openTabs = useRef(state.tabs);
  openTabs.current = state.tabs;
  useEffect(() => {
    const pending: Tab[] = [];
    const onMenuAction = (event: Event) => {
      const action: unknown = (event as CustomEvent).detail;
      // A job rides the same event as the native menu. The detail is an object, so the string commands below ignore it.
      if (isOpenJobDetail(action)) {
        if (renaming) return;
        const next = jobOpenAction([...openTabs.current, ...pending], action);
        if (!next) return;
        if (next.type === 'open') pending.push(next.tab);
        // A background open leaves the person where they are, including on the all-tabs layer.
        if (!action.background) { onActivate(); setOverviewOpen(false); }
        dispatch(next);
        return;
      }
      // A web open rides it too: the link chip does not hold the strip. A refused scheme never gets this far.
      if (isOpenWebDetail(action)) {
        if (renaming) return;
        if (!action.background) { onActivate(); setOverviewOpen(false); }
        pending.push(action.tab);
        dispatch(webOpenAction([...openTabs.current, ...pending.slice(0, -1)], action));
        return;
      }
      if (!isDesktopTabAction(action) || renaming) return;
      onActivate();
      if (action === 'overview') setOverviewOpen(open => !open);
      else if (action === 'close') { setOverviewOpen(false); closeTab(state.activeId); }
      else if (action === 'close-stop') { setOverviewOpen(false); closeAndStop(state.activeId); }
      else if (action === 'new' || action === 'reopen') { setOverviewOpen(false); dispatch({ type: action }); }
      else { setOverviewOpen(false); dispatch({ type: 'select', id: neighbour(visible, state.activeId, action === 'next' ? 1 : -1).id }); }
    };
    window.addEventListener(desktopTabEvent, onMenuAction);
    return () => window.removeEventListener(desktopTabEvent, onMenuAction);
  }, [onActivate, renaming, state.activeId, visible]);
  useEffect(() => {
    const onOpenKind = (event: Event) => {
      const kind = (event as CustomEvent<ShellKind>).detail;
      if (!kindDef(kind).backed || renaming) return;
      setOverviewOpen(false);
      dispatch(openKindAction(state, kind));
      onActivate();
    };
    const onLeaveKind = (event: Event) => {
      if (renaming) return;
      setOverviewOpen(false);
      dispatch(leaveKindAction(state, (event as CustomEvent<ShellKind>).detail));
      onActivate();
    };
    window.addEventListener(shellEvent, onOpenKind);
    window.addEventListener(shellLeaveEvent, onLeaveKind);
    return () => { window.removeEventListener(shellEvent, onOpenKind); window.removeEventListener(shellLeaveEvent, onLeaveKind); };
  }, [onActivate, renaming, state]);
}
