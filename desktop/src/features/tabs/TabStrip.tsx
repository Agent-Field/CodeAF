import { useEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { Button, DropdownMenu, Icon, IconButton } from '../../components/ui';
import design from '../../design/tokens.json';
import { overviewShortcut, tabShortcuts } from '../../design/keyboard';
import type { TabsApi } from './context';
import { GroupCapsule, MemberSlot } from './GroupCapsule';
import { groupDropProps } from './hosts/dragHost';
import { overflowItems, withGroupMenu } from './hosts/menuHost';
import { stateOfMark, TabItem, tabDomIds } from './TabItem';
import { focusedPane, visibleTabs, type Tab } from './model';
import './strip.css';

/**
 * The strip (46px): pinned tabs, a hairline, then tabs and group capsules, "+" and, at the far right, the
 * overview grid. Tabs compress from 190px to 112px, then the strip scrolls under a 40px mask at its right
 * edge and a "+N" menu lists every tab. No scrollbar, no chevron buttons, no wheel hijacking.
 */
export function TabStrip({ api, leading, overviewTrigger, onOverview }: { api: TabsApi; leading?: ReactNode; overviewTrigger: RefObject<HTMLButtonElement | null>; onOverview: () => void }) {
  const { state, dispatch } = api;
  const strip = useRef<HTMLDivElement>(null);
  const [edge, setEdge] = useState({ end: false, hidden: 0 });
  const order = visibleTabs(state);
  const pinned = state.tabs.filter(t => t.pinned);
  const loose = state.tabs.filter(t => !t.pinned && !t.groupId);
  // The strip only re-measures when its shape changes, never on a draft keystroke.
  const shape = JSON.stringify([state.activeId, state.groups, state.tabs.map(t => [t.id, t.title, t.pinned, t.groupId, t.split?.panes.map(p => [p.id, p.title])])]);
  const item = (tab: Tab, inGroup = false) => <TabItem key={tab.id} api={api} tab={tab} order={order} inGroup={inGroup}/>;

  useEffect(() => {
    const viewport = strip.current;
    if (!viewport) return;
    const tolerance = parseFloat(design.foundation['border-width']);
    const measure = () => {
      const bounds = viewport.getBoundingClientRect();
      const end = viewport.scrollWidth - viewport.clientWidth - viewport.scrollLeft > tolerance;
      // A tab counts as hidden when any part of it lies outside the strip's visible box.
      const hidden = Array.from(viewport.querySelectorAll<HTMLElement>('[role="tab"]')).filter(el => {
        if (el.closest('[inert]')) return false;
        const box = el.getBoundingClientRect();
        return box.left < bounds.left - tolerance || box.right > bounds.right + tolerance;
      }).length;
      setEdge(previous => (previous.end === end && previous.hidden === hidden ? previous : { end, hidden }));
    };
    const observer = new ResizeObserver(measure);
    observer.observe(viewport);
    Array.from(viewport.children).forEach(child => observer.observe(child));
    viewport.addEventListener('scroll', measure, { passive: true });
    measure();
    return () => { observer.disconnect(); viewport.removeEventListener('scroll', measure); };
  }, [shape]);

  useEffect(() => {
    const selected = strip.current?.querySelector<HTMLElement>('[aria-selected="true"]');
    selected?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' });
    const frame = requestAnimationFrame(() => selected?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' }));
    return () => cancelAnimationFrame(frame);
  }, [state.activeId, state.groups]);

  return (
    <div className="workspace-tabbar" data-tauri-drag-region>
      {leading}
      <div className="workspace-tablist-owner" role="tablist" aria-label="Conversation tabs" aria-owns={order.flatMap(tabDomIds).join(" ")}/>
      <div ref={strip} className="workspace-tabstrip" aria-label="Conversation tabs" data-fade-end={edge.end || undefined}>
        {pinned.map(tab => item(tab))}
        {pinned.length > 0 && <span className="workspace-tab-divider" role="separator" aria-orientation="vertical"/>}
        {loose.map(tab => item(tab))}
        {state.groups.filter(g => state.tabs.some(t => t.groupId === g.id)).map(group => {
          const members = state.tabs.filter(t => t.groupId === group.id);
          return (
            <GroupCapsule key={group.id} title={group.title} count={members.length} collapsed={group.collapsed} needsYou={members.some(t => stateOfMark(api.summaries[focusedPane(t).id]?.mark) === 'waiting')}
              onToggle={() => dispatch({ type: 'collapse-group', id: group.id })} {...groupDropProps(api, group)} wrapLabel={label => withGroupMenu(api, group, label)}>
              {members.map(tab => <MemberSlot key={tab.id} hidden={group.collapsed && tab.id !== state.activeId}>{item(tab, !group.collapsed)}</MemberSlot>)}
            </GroupCapsule>
          );
        })}
      </div>
      <div className="workspace-tab-actions">
        <IconButton className="workspace-tab-action" label="New tab" title={`New tab (${tabShortcuts.new})`} icon="plus" iconSize="sm" onClick={() => dispatch({ type: 'new' })}/>
        {edge.hidden > 0 && <DropdownMenu label="Tab actions" items={overflowItems(api)}><Button className="workspace-tab-more" aria-label="Tab actions">+{edge.hidden}<Icon name="chevron" size="micro" motion="disclosure"/></Button></DropdownMenu>}
        <IconButton ref={overviewTrigger} className="workspace-tab-action workspace-overview-trigger" label="All tabs" title={`All tabs (${overviewShortcut})`} icon="grid" iconSize="sm" onClick={onOverview}/>
      </div>
    </div>
  );
}
