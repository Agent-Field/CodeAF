import { useLayoutEffect, useRef, useState, useSyncExternalStore, type RefObject, type KeyboardEvent, type MouseEvent } from 'react';
import { isMac } from '../../design/keyboard';
import { useAltHeld } from './closing/altHeld';
import { closeShortcutFor, closeStopShortcut } from './closing/shortcuts';
import type { TabMark } from '../conversation/tabSummary';
import type { TabsApi } from './context';
import { tabDragProps } from './hosts/dragHost';
import { withTabMenu } from './hosts/menuHost';
import { withPreview } from './hosts/previewHost';
import { withSegmentTooltip } from './hosts/titleTooltipHost';
import { splitTitle } from './helpers';
import { focusedPane, panesOf, type Pane, type Tab } from './model';
import { SplitTab } from './SplitTab';
import { useWebFavicons } from '../web/favicons';
import { monogramOf } from '../web/address';
import { DropdownMenu, type IconName } from '../../components/ui';
import { kindDef } from './kinds/registry';
import { isPlaceHome } from './reducers/home';
import { isSelectionPress } from './selection';
import { stripMiddleCloses } from './stripPointer';
import { announce } from './announce';
import { Tab as TabView, type TabState } from './Tab';


/** The DOM id of the element that carries a tab's role=tab (a split's is its focused pane's segment). */
export const tabDomId = (tab: Tab) => `tab-${focusedPane(tab).id}`;
/** Every role=tab element a tab owns: one, or one segment per pane of a split. */
export const tabDomIds = (tab: Tab) => panesOf(tab).map(pane => `tab-${pane.id}`);

/** Running is silent: only waiting and failed marks reach the tab. */
export const stateOfMark = (mark?: TabMark): TabState | undefined => (mark === 'waiting' ? 'waiting' : mark === 'failed' ? 'failed' : undefined);

/** The kind's per-pane icon, when it is a registry name. A favicon or monogram stays on the web path. */
function paneIcon(pane: Pane): IconName | undefined {
  const picked = kindDef(pane.kind).iconFor?.(pane);
  return typeof picked === 'string' ? picked : undefined;
}

/** Roving focus along the strip's reading order. */
export function navigate(api: TabsApi, order: readonly Tab[], tab: Tab, pendingFocus: RefObject<string | null>) {
  return (event: KeyboardEvent) => {
    if (event.altKey && event.shiftKey && !event.ctrlKey && !event.metaKey && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
      const focused = (event.target as HTMLElement).closest<HTMLElement>('[role="tab"]');
      if (!focused) return;
      event.preventDefault();
      event.stopPropagation();
      // The keyboard move stays in its section; place Home remains the fixed first tab.
      const section = api.state.tabs.filter(item => !isPlaceHome(item) && item.pinned === tab.pinned && item.groupId === tab.groupId);
      const index = section.findIndex(item => item.id === tab.id);
      const direction = event.key === 'ArrowRight' ? 1 : -1;
      const target = section[index + direction];
      if (index < 0 || !target) return;
      pendingFocus.current = focused.id;
      api.dispatch({ type: 'reorder', id: tab.id, targetId: target.id, after: direction === 1 });
      announce(focused, `Moved to position ${index + direction + 1} of ${section.length}`);
      return;
    }
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    const index = order.findIndex(t => t.id === tab.id);
    const target = event.key === 'ArrowRight' ? order[(index + 1) % order.length] : event.key === 'ArrowLeft' ? order[(index - 1 + order.length) % order.length] : event.key === 'Home' ? order[0] : event.key === 'End' ? order[order.length - 1] : undefined;
    if (!target) return;
    event.preventDefault();
    api.dispatch({ type: 'select', id: target.id });
    requestAnimationFrame(() => document.getElementById(tabDomId(target))?.focus());
  };
}

/**
 * The 44px chip (Shell 3j "Compressed") is an inactive tab that would otherwise keep a title, and only at the
 * small breakpoint. A pinned tab is already a 30px icon. A place Home keeps its name. The active tab keeps its title.
 */
export function compressesTab(narrow: boolean, active: boolean, pinned: boolean, home: boolean): boolean {
  return narrow && !active && !pinned && !home;
}

/**
 * A compressed split draws one chip, so a pane that is not the focused one still has to light it.
 * Waiting outranks failed, the same order a full snapshot uses.
 */
function compressedSplitMark(marks: readonly (TabState | undefined)[]): TabState | undefined {
  if (marks.includes('waiting')) return 'waiting';
  if (marks.includes('failed')) return 'failed';
  return undefined;
}

/** One strip item: the tab primitive composed with the menu, preview and drag hosts. */
export function TabItem({ api, tab, order, inGroup = false, narrow = false }: { api: TabsApi; tab: Tab; order: readonly Tab[]; inGroup?: boolean; narrow?: boolean }) {
  const pendingFocus = useRef<string | null>(null);
  // Restore after React commits the DOM move so browser focus loss cannot outlast the reorder.
  useLayoutEffect(() => {
    if (!pendingFocus.current) return;
    document.getElementById(pendingFocus.current)?.focus();
    pendingFocus.current = null;
  });
  const active = tab.id === api.state.activeId;
  const favicons = useWebFavicons(api.workspaceKey, panesOf(tab));
  const drag = tabDragProps(api, tab);
  const home = isPlaceHome(tab);
  // Middle-click is the × (SH-OQ4): the view closes and the work keeps running. A pin and a place Home have no close.
  const onMouseDown = (event: MouseEvent) => { if (event.button === 1) event.preventDefault(); };
  const onAuxClick = (event: MouseEvent) => {
    if (event.button !== 1) return;
    event.preventDefault();
    if (stripMiddleCloses(tab)) api.closeTab(tab.id);
  };
  // Design 3l: Alt turns a running tab's close into a stop square, but only under the pointer or focus. An idle tab ignores Alt.
  const [engaged, setEngaged] = useState(false);
  const stop = useAltHeld() && engaged && api.isRunning(tab);
  const running = api.isRunning(tab);
  const picked = !!api.state.picked?.includes(tab.id);
  // While any preview card is open the tab's own full-title tooltip stays shut (Shell 3l: the two never stack).
  const previewOpen = useSyncExternalStore(api.previews.subscribe, () => api.previews.get() !== null);
  const choose = (id: string) => (event: MouseEvent) => api.dispatch(isSelectionPress(event, isMac) ? { type: 'pick', id: tab.id } : { type: 'select', id });
  const frame = { ...drag, 'data-picked': picked || undefined, 'data-selected': picked || undefined, onMouseDown, onAuxClick, onPointerEnter: () => setEngaged(true), onPointerLeave: () => setEngaged(false), onFocus: () => setEngaged(true), onBlur: () => setEngaged(false) };
  const compressed = compressesTab(narrow, active, tab.pinned, home);
  if (tab.split && compressed) {
    const panes = panesOf(tab);
    const focus = focusedPane(tab);
    // One chip cannot show a segment per pane. The tooltip and the accessible name keep every pane title,
    // joined the same way the split's own title is, and the segment row returns when this split is active.
    const view = (
      <TabView compressed icon={paneIcon(focus)} favicon={favicons.get(focus.id)} kind={focus.kind} title={splitTitle(panes)} monogram={monogramOf(focus)} active={false} inGroup={inGroup} picked={picked} state={compressedSplitMark(panes.map(pane => stateOfMark(api.summaries[pane.id]?.mark)))} id={tabDomId(tab)} frame={frame} previewOpen={previewOpen} onSelect={choose(focus.id)} onKeyDown={navigate(api, order, tab, pendingFocus)}/>
    );
    return withTabMenu(api, tab, view);
  }
  if (tab.split) {
    const { panes, focus } = tab.split;
    const segments = panes.map(pane => ({ id: pane.id, kind: pane.kind, title: pane.title, icon: paneIcon(pane), monogram: monogramOf(pane), favicon: favicons.get(pane.id), state: stateOfMark(api.summaries[pane.id]?.mark) }));
    return withTabMenu(api, tab, <SplitTab picked={picked} segments={segments} focus={focus} active={active} frame={{ ...frame, onKeyDown: navigate(api, order, tab, pendingFocus) }} onSelectPane={(index, event) => choose(panes[index].id)(event)} wrapSegment={(segment, button) => withSegmentTooltip(api, tab, segment.title, button)} onClose={() => api.closeTab(tab.id)}/>);
  }
  const switcher = home ? api.placeSwitcher : undefined;
  // The kind's words after the name. A finished job contributes `exit N`; everything else contributes nothing.
  const meta = kindDef(tab.kind).tabMeta?.(tab, api.summaries[tab.id]);
  const view = (
    <TabView compressed={compressed} icon={paneIcon(tab)} favicon={favicons.get(tab.id)} kind={tab.kind} title={tab.title} meta={meta} monogram={monogramOf(focusedPane(tab))} active={active} pinned={tab.pinned} placeTint={home ? api.placeTint ?? 'graphite' : undefined} inGroup={inGroup} picked={picked} state={stateOfMark(api.summaries[tab.id]?.mark)} id={tabDomId(tab)} frame={frame} badge={tab.kind === 'inbox' && (api.background.needsYou.length > 0 ? 'needsYou' : api.background.failed.length > 0 ? 'failed' : false)}
      closeMode={stop ? 'stop' : 'close'} closeHint={stop ? 'Close and stop' : running ? 'Close · keeps running' : 'Close'} closeShortcut={stop ? closeStopShortcut : closeShortcutFor(tab.kind)}
      onSelect={choose(tab.id)} onClose={() => (stop ? api.closeAndStop(tab.id) : api.closeTab(tab.id))} onRename={() => api.startRename(tab.id)}
      onKeyDown={navigate(api, order, tab, pendingFocus)} previewOpen={previewOpen} wrapSelect={select => (switcher ? <DropdownMenu label="Place switcher" items={switcher.items}>{select}</DropdownMenu> : withPreview(api, tab, select))} switcher={switcher && { alert: switcher.alert }}/>
  );
  // The switcher menu hangs on the tab's own button, so its popup attributes land on a control and not on the frame.
  return withTabMenu(api, tab, view);
}
