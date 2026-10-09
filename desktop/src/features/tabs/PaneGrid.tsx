import { useEffect, useRef, useState, type Dispatch, type ReactNode } from 'react';
import { DropdownMenu, IconButton, type MenuEntry } from '../../components/ui';
import { OpenFileProvider } from '../files/OpenFile';
import { kindDef } from './kinds/registry';
import type { PaneActions } from './kinds/slots';
import { panesOf, splitCapacity, type Pane, type Tab, type WorkspaceAction } from './model';
import { canSplitInto, useDraggedTab } from './hosts/dragHost';
import { SplitHandles } from './SplitHandles';
import { SplitZones } from './SplitZones';
import { tabDomId } from './TabItem';
import { pruneScrollMemory, useScrollRestore } from './scroll/useScrollRestore';
import { usePaneKeys } from './usePaneKeys';
import './panes.css';

/** The pane menu (Interactions "Split pane"): close it, trade places with another pane, or fill the card with it. */
function paneMenu(tab: Tab, pane: Pane, maximized: boolean, onMaximize: () => void, dispatch: Dispatch<WorkspaceAction>): MenuEntry[] {
  const others = tab.split!.panes.filter(p => p.id !== pane.id);
  const swap: MenuEntry[] = others.length === 1
    ? [{ id: 'swap', label: 'Swap', icon: 'transfer', disabled: maximized, onSelect: () => dispatch({ type: 'split-swap', id: tab.id, paneId: pane.id, withPaneId: others[0].id }) }]
    : [{ id: 'swap', label: 'Swap with', icon: 'transfer', disabled: maximized, kind: 'submenu', items: others.map(other => ({ id: `swap-${other.id}`, label: other.title, onSelect: () => dispatch({ type: 'split-swap', id: tab.id, paneId: pane.id, withPaneId: other.id }) })) }];
  return [
    { id: 'maximize', label: maximized ? 'Restore panes' : 'Maximize', icon: maximized ? 'shrink' : 'expand', onSelect: onMaximize },
    ...swap,
    { kind: 'separator', id: 'sep' },
    { id: 'close', label: 'Close pane', icon: 'close', danger: true, onSelect: () => dispatch({ type: 'split-close-pane', id: tab.id, paneId: pane.id }) },
  ];
}

/** One pane's title line: 40px, only while the window is split. Its controls show on hover, on keyboard focus and always on touch. */
export function PaneHeader({ pane, focused, onClose, menu }: { pane: Pane; focused: boolean; onClose?: () => void; menu?: MenuEntry[] }) {
  return (
    <div className="pane-header" data-focused={focused}>
      <span className="pane-title">{pane.title}</span>
      <span className="pane-header-controls">
        {menu && <DropdownMenu label={`Pane menu ${pane.title}`} items={menu}><IconButton className="pane-header-more" label={`Pane menu ${pane.title}`} icon="more" iconSize="micro"/></DropdownMenu>}
        {onClose && <IconButton className="pane-header-close" label={`Close pane ${pane.title}`} icon="close" iconSize="micro" onClick={onClose}/>}
      </span>
    </div>
  );
}

/** One pane's body. It remembers where every scroller inside it was left and puts each back when the tab returns (scroll/useScrollRestore). */
function PaneBody({ paneId, visible, children }: { paneId: string; visible: boolean; children: ReactNode }) {
  const body = useRef<HTMLDivElement>(null);
  useScrollRestore(paneId, body, visible);
  return <div ref={body} className="workspace-pane-body">{children}</div>;
}

/**
 * The content card for the active tab: one pane is the card itself; a split is a grid of cards (1x2, 2x1,
 * 2x2) with a 40px title line each and a 1.5px accent-soft ring on the focused pane. Each pane body is
 * its kind's registered renderer; nothing here switches on kind. While a tab is dragged over the card it
 * draws the split zones (design 2g); a split also draws hover resize handles (design 2h).
 */
export function PaneGrid({ tab, tabs, dispatch, actionsFor }: { tab: Tab; tabs: readonly Tab[]; dispatch: Dispatch<WorkspaceAction>; actionsFor: (pane: Pane) => PaneActions }) {
  const panes = panesOf(tab);
  const split = !!tab.split;
  const focus = tab.split?.focus ?? 0;
  const grid = useRef<HTMLDivElement>(null);
  // Maximize is a view of the card, not part of the saved split: the hidden panes stay mounted, so drafts and streams carry on.
  const [maximizedId, setMaximizedId] = useState<string | null>(null);
  const maximized = split && panes.some(p => p.id === maximizedId) ? maximizedId : null;
  useEffect(() => { setMaximizedId(null); }, [tab.id]);
  usePaneKeys(tab, dispatch, grid);
  // Closing a tab ends its panes' remembered positions; the memory only ever holds panes that are in the saved tab list.
  useEffect(() => { pruneScrollMemory(new Set(tabs.flatMap(t => panesOf(t).map(p => p.id)))); }, [tabs]);
  const draggedId = useDraggedTab();
  const guest = draggedId ? tabs.find(t => t.id === draggedId) : undefined;
  return (
    <div ref={grid} className="workspace-conversation" role="tabpanel" id="workspace-tab-panel" aria-labelledby={tabDomId(tab)} tabIndex={0} data-split={split || undefined} data-layout={maximized ? undefined : tab.split?.layout} data-count={maximized ? undefined : panes.length} data-maximized={maximized ? '' : undefined}>
      {panes.map((pane, index) => {
        const Body = kindDef(pane.kind).pane;
        const focused = index === focus;
        return (
          <section key={pane.id} className="workspace-pane" hidden={maximized !== null && maximized !== pane.id} tabIndex={split ? -1 : undefined} data-focused={split ? focused : undefined} aria-label={split ? pane.title : undefined}
            onPointerDownCapture={split && !focused ? () => dispatch({ type: 'split-focus', id: tab.id, index }) : undefined}
            onFocusCapture={split && !focused ? () => dispatch({ type: 'split-focus', id: tab.id, index }) : undefined}>
            {split && <PaneHeader pane={pane} focused={focused} onClose={() => dispatch({ type: 'split-close-pane', id: tab.id, paneId: pane.id })} menu={paneMenu(tab, pane, maximized === pane.id, () => { setMaximizedId(maximized === pane.id ? null : pane.id); if (!focused) dispatch({ type: 'split-focus', id: tab.id, index }); }, dispatch)}/>}
            <PaneBody paneId={pane.id} visible={maximized === null || maximized === pane.id}><OpenFileProvider value={actionsFor(pane).onOpenFile}><Body pane={pane} label={pane.title} focused={focused} split={split} actions={actionsFor(pane)}/></OpenFileProvider></PaneBody>
          </section>
        );
      })}
      {tab.split && !maximized && <SplitHandles tab={tab} grid={grid} dispatch={dispatch}/>}
      {draggedId && canSplitInto(tab, guest, splitCapacity) && <SplitZones hostId={tab.id} guestId={draggedId} dispatch={dispatch}/>}
    </div>
  );
}
