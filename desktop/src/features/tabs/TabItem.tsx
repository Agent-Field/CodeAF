import type { KeyboardEvent } from 'react';
import type { TabMark } from '../conversation/tabSummary';
import type { TabsApi } from './context';
import { tabDragProps } from './hosts/dragHost';
import { withTabMenu } from './hosts/menuHost';
import { withPreview } from './hosts/previewHost';
import { focusedPane, panesOf, type Tab } from './model';
import { SplitTab } from './SplitTab';
import { monogramOf } from '../web/address';
import { Tab as TabView, type TabState } from './Tab';


/** The DOM id of the element that carries a tab's role=tab (a split's is its focused pane's segment). */
export const tabDomId = (tab: Tab) => `tab-${focusedPane(tab).id}`;
/** Every role=tab element a tab owns: one, or one segment per pane of a split. */
export const tabDomIds = (tab: Tab) => panesOf(tab).map(pane => `tab-${pane.id}`);

/** Running is silent: only waiting and failed marks reach the tab. */
export const stateOfMark = (mark?: TabMark): TabState | undefined => (mark === 'waiting' ? 'waiting' : mark === 'failed' ? 'failed' : undefined);

/** Roving focus along the strip's reading order. */
function navigate(api: TabsApi, order: readonly Tab[], tab: Tab) {
  return (event: KeyboardEvent) => {
    const index = order.findIndex(t => t.id === tab.id);
    const target = event.key === 'ArrowRight' ? order[(index + 1) % order.length] : event.key === 'ArrowLeft' ? order[(index - 1 + order.length) % order.length] : event.key === 'Home' ? order[0] : event.key === 'End' ? order[order.length - 1] : undefined;
    if (!target) return;
    event.preventDefault();
    api.dispatch({ type: 'select', id: target.id });
    requestAnimationFrame(() => document.getElementById(tabDomId(target))?.focus());
  };
}

/** One strip item: the tab primitive composed with the menu, preview and drag hosts. */
export function TabItem({ api, tab, order, inGroup = false }: { api: TabsApi; tab: Tab; order: readonly Tab[]; inGroup?: boolean }) {
  const active = tab.id === api.state.activeId;
  const frame = tabDragProps(api, tab);
  if (tab.split) {
    const { panes, focus } = tab.split;
    const segments = panes.map(pane => ({ id: pane.id, kind: pane.kind, title: pane.title, monogram: monogramOf(pane), state: stateOfMark(api.summaries[pane.id]?.mark) }));
    return withTabMenu(api, tab, <SplitTab segments={segments} focus={focus} active={active} frame={frame} onSelectPane={index => api.dispatch({ type: 'select', id: panes[index].id })} onClose={() => api.closeTab(tab.id)}/>);
  }
  return withTabMenu(api, tab, (
    <TabView kind={tab.kind} title={tab.title} monogram={monogramOf(focusedPane(tab))} active={active} pinned={tab.pinned} inGroup={inGroup} state={stateOfMark(api.summaries[tab.id]?.mark)} id={tabDomId(tab)} frame={frame}
      onSelect={() => api.dispatch({ type: 'select', id: tab.id })} onClose={() => api.closeTab(tab.id)} onRename={() => api.startRename(tab.id)}
      onKeyDown={navigate(api, order, tab)} wrapSelect={select => withPreview(api, tab, select)}/>
  ));
}
