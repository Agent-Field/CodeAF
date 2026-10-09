import type { Dispatch } from 'react';
import { OpenFileProvider } from '../files/OpenFile';
import { kindDef } from './kinds/registry';
import type { PaneActions } from './kinds/slots';
import { panesOf, type Pane, type Tab, type WorkspaceAction } from './model';
import { tabDomId } from './TabItem';
import './panes.css';

/** One pane's title line: 40px, only while the window is split. The pane lane puts hover controls in the trailing slot. */
export function PaneHeader({ pane, focused }: { pane: Pane; focused: boolean }) {
  return <div className="pane-header" data-focused={focused}><span className="pane-title">{pane.title}</span><span className="pane-header-controls"/></div>;
}

/**
 * The content card for the active tab: one pane is the card itself; a split is a grid of cards (1x2, 2x1,
 * 2x2) with a 40px title line each and a 1.5px accent-soft ring on the focused pane. Each pane body is
 * its kind's registered renderer; nothing here switches on kind.
 */
export function PaneGrid({ tab, dispatch, actionsFor }: { tab: Tab; dispatch: Dispatch<WorkspaceAction>; actionsFor: (pane: Pane) => PaneActions }) {
  const panes = panesOf(tab);
  const split = !!tab.split;
  const focus = tab.split?.focus ?? 0;
  return (
    <div className="workspace-conversation" role="tabpanel" id="workspace-tab-panel" aria-labelledby={tabDomId(tab)} tabIndex={0} data-split={split || undefined} data-layout={tab.split?.layout} data-count={panes.length}>
      {panes.map((pane, index) => {
        const Body = kindDef(pane.kind).pane;
        const focused = index === focus;
        return (
          <section key={pane.id} className="workspace-pane" data-focused={split ? focused : undefined} aria-label={split ? pane.title : undefined}
            onPointerDownCapture={split && !focused ? () => dispatch({ type: 'split-focus', id: tab.id, index }) : undefined}
            onFocusCapture={split && !focused ? () => dispatch({ type: 'split-focus', id: tab.id, index }) : undefined}>
            {split && <PaneHeader pane={pane} focused={focused}/>}
            <div className="workspace-pane-body"><OpenFileProvider value={actionsFor(pane).onOpenFile}><Body pane={pane} label={pane.title} focused={focused} split={split} actions={actionsFor(pane)}/></OpenFileProvider></div>
          </section>
        );
      })}
    </div>
  );
}
