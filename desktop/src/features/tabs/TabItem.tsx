import { useState, type KeyboardEvent, type MouseEvent } from 'react';
import { isMac } from '../../design/keyboard';
import { useAltHeld } from './closing/altHeld';
import { closeShortcutFor, closeStopShortcut } from './closing/shortcuts';
import type { TabMark } from '../conversation/tabSummary';
import type { TabsApi } from './context';
import { tabDragProps } from './hosts/dragHost';
import { withTabMenu } from './hosts/menuHost';
import { withPreview } from './hosts/previewHost';
import { withSegmentTooltip, withTitleTooltip } from './hosts/titleTooltipHost';
import { focusedPane, panesOf, type Tab } from './model';
import { SplitTab } from './SplitTab';
import { useWebFavicons } from '../web/favicons';
import { monogramOf } from '../web/address';
import { DropdownMenu } from '../../components/ui';
import { isPlaceHome } from './reducers/home';
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

/** ⌘-click on a Mac, Ctrl-click elsewhere: picks a tab for ⌘G instead of selecting it (Shell, "⌘-selecting and pressing ⌘G"). */
const picks = (event: MouseEvent) => (isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey) && !event.altKey && !event.shiftKey;

/** One strip item: the tab primitive composed with the menu, preview and drag hosts. */
export function TabItem({ api, tab, order, inGroup = false }: { api: TabsApi; tab: Tab; order: readonly Tab[]; inGroup?: boolean }) {
  const active = tab.id === api.state.activeId;
  const favicons = useWebFavicons(api.workspaceKey, panesOf(tab));
  const drag = tabDragProps(api, tab);
  // Design 3l: Alt turns a running tab's close into a stop square, but only under the pointer or focus. An idle tab ignores Alt.
  const [engaged, setEngaged] = useState(false);
  const stop = useAltHeld() && engaged && api.isRunning(tab);
  const running = api.isRunning(tab);
  const picked = !!api.state.picked?.includes(tab.id);
  const choose = (id: string) => (event: MouseEvent) => api.dispatch(picks(event) ? { type: 'pick', id: tab.id } : { type: 'select', id });
  const frame = { ...drag, 'data-picked': picked || undefined, onPointerEnter: () => setEngaged(true), onPointerLeave: () => setEngaged(false), onFocus: () => setEngaged(true), onBlur: () => setEngaged(false) };
  if (tab.split) {
    const { panes, focus } = tab.split;
    const segments = panes.map(pane => ({ id: pane.id, kind: pane.kind, title: pane.title, monogram: monogramOf(pane), favicon: favicons.get(pane.id), state: stateOfMark(api.summaries[pane.id]?.mark) }));
    return withTabMenu(api, tab, <SplitTab segments={segments} focus={focus} active={active} frame={frame} onSelectPane={(index, event) => choose(panes[index].id)(event)} wrapSegment={(segment, button) => withSegmentTooltip(api, tab, segment.title, button)} onClose={() => api.closeTab(tab.id)}/>);
  }
  const switcher = isPlaceHome(tab) ? api.placeSwitcher : undefined;
  const view = (
    <TabView favicon={favicons.get(tab.id)} kind={tab.kind} title={tab.title} monogram={monogramOf(focusedPane(tab))} active={active} pinned={tab.pinned} placeTint={isPlaceHome(tab) ? api.placeTint ?? 'graphite' : undefined} inGroup={inGroup} picked={picked} state={stateOfMark(api.summaries[tab.id]?.mark)} id={tabDomId(tab)} frame={frame} badge={tab.kind === 'inbox' && (api.background.needsYou.length > 0 ? 'needsYou' : api.background.failed.length > 0 ? 'failed' : false)}
      closeMode={stop ? 'stop' : 'close'} closeHint={stop ? 'Close and stop' : running ? 'Close · keeps running' : 'Close'} closeShortcut={stop ? closeStopShortcut : closeShortcutFor(tab.kind)}
      onSelect={choose(tab.id)} onClose={() => (stop ? api.closeAndStop(tab.id) : api.closeTab(tab.id))} onRename={() => api.startRename(tab.id)}
      onKeyDown={navigate(api, order, tab)} wrapSelect={select => (switcher ? <DropdownMenu label="Place switcher" items={switcher.items}>{select}</DropdownMenu> : withTitleTooltip(api, tab, select, trigger => withPreview(api, tab, trigger)))} switcher={switcher && { alert: switcher.alert }}/>
  );
  // The switcher menu hangs on the tab's own button, so its popup attributes land on a control and not on the frame.
  return withTabMenu(api, tab, view);
}
