import { useEffect, useReducer, useRef, useState, type ReactNode } from 'react';
import { BrandMark, Button, Icon, IconButton, PageHeading, Text, TextInput, TextArea, HoverPreview, ContextMenu, DropdownMenu, type MenuEntry } from '../../components/ui';
import { readWorkspace, storageKey, workspaceReducer, type Tab } from './model';
import './workspace.css';
import { TabOverview } from './TabOverview';
import design from '../../design/tokens.json';

type Props = { enabled: boolean; onExplore: () => void; leading?: ReactNode };
export function Workspace({ enabled, onExplore, leading }: Props) {
 const [state, dispatch] = useReducer(workspaceReducer, undefined, readWorkspace);
 const [overviewOpen, setOverviewOpen] = useState(false);
 const [switcher, setSwitcher] = useState<{ ids: string[]; index: number } | null>(null);
 const switcherRef = useRef<typeof switcher>(null);
 const switcherFocus = useRef<HTMLDivElement>(null);
 const [rename, setRename] = useState<{ id: string; group: boolean; value: string } | null>(null);
 const renameDialog = useRef<HTMLDialogElement>(null);
 const renameInput = useRef<HTMLInputElement>(null);
 const strip = useRef<HTMLDivElement>(null);
 const overviewTrigger = useRef<HTMLButtonElement>(null);
 const [scrollEdges, setScrollEdges] = useState({ left: false, right: false });
 const active = state.tabs.find(tab => tab.id === state.activeId) ?? state.tabs[0];
 const visible = [...state.tabs.filter(t => t.pinned), ...state.tabs.filter(t => !t.pinned && !t.groupId), ...state.groups.flatMap(g => state.tabs.filter(t => t.groupId === g.id && (!g.collapsed || t.id === state.activeId)))];
 useEffect(() => { try { localStorage.setItem(storageKey, JSON.stringify(state)); } catch { /* A full or unavailable store must not interrupt local tab navigation. */ } }, [state]);
 useEffect(() => {
  const viewport = strip.current;
  if (!viewport || !enabled) return;
  const tolerance = parseFloat(design.foundation['border-width']);
  const measure = () => {
   const left = viewport.scrollLeft > tolerance;
   const right = viewport.scrollWidth - viewport.clientWidth - viewport.scrollLeft > tolerance;
   setScrollEdges(previous => previous.left === left && previous.right === right ? previous : { left, right });
  };
  const observer = new ResizeObserver(measure);
  observer.observe(viewport);
  Array.from(viewport.children).forEach(child => observer.observe(child));
  viewport.addEventListener('scroll', measure, { passive: true }); measure();
  return () => { observer.disconnect(); viewport.removeEventListener('scroll', measure); };
 }, [enabled, state.tabs, state.groups]);
 function scrollTabs(direction: number) {
  strip.current?.scrollBy({ left: direction * parseFloat(design.foundation['tab-max-width']), behavior: 'auto' });
 }
 useEffect(() => {
  const selected = strip.current?.querySelector<HTMLElement>('[aria-selected="true"]');
  selected?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' });
  // Overflow controls settle first so the active tab is revealed within the final viewport.
  const frame = requestAnimationFrame(() => selected?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' }));
  return () => cancelAnimationFrame(frame);
 }, [state.activeId, state.groups]);
 useEffect(() => {
  if (!switcher) return;
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  switcherFocus.current?.focus();
  return () => {
   if (previous?.closest('.workspace-tab')) strip.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus();
   else if (previous?.isConnected) previous.focus();
  };
 }, [!!switcher]);
 useEffect(() => {
  if (!switcher) return;
  switcherFocus.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest', behavior: 'instant' });
 }, [switcher]);
 useEffect(() => {
  if (rename) { renameDialog.current?.showModal(); renameInput.current?.focus(); renameInput.current?.select(); }
  else renameDialog.current?.close();
 }, [rename]);
 useEffect(() => {
  if (!enabled) { switcherRef.current = null; setSwitcher(null); return; }
  const onKey = (event: KeyboardEvent) => {
   if (event.key === 'Escape' && switcherRef.current) { event.preventDefault(); switcherRef.current = null; setSwitcher(null); return; }
   if (!(event.metaKey || event.ctrlKey) || document.querySelector('dialog[open]')) return;
   const key = event.key.toLowerCase();
   if (key === 't') { event.preventDefault(); dispatch({ type: event.shiftKey ? 'reopen' : 'new' }); }
   if (key === 'w') { event.preventDefault(); closeTab(state.activeId); }
   if (key === 'tab') {
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
 }, [enabled, state.activeId, state.recentIds, visible]);
 function closeTab(id: string) {
  const restore = !!document.activeElement?.closest('.workspace-tab');
  dispatch({ type: 'close', id });
  if (restore) requestAnimationFrame(() => strip.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.focus());
 }
 function startRename(id: string, group = false) { setRename({ id, group, value: (group ? state.groups : state.tabs).find(item => item.id === id)?.title ?? '' }); }
 function tabItems(tab: Tab): MenuEntry[] {
  return [
   { id: 'rename', label: 'Rename tab', onSelect: () => startRename(tab.id) },
   { id: 'pin', label: tab.pinned ? 'Unpin tab' : 'Pin tab', icon: 'pin', onSelect: () => dispatch({ type: 'pin', id: tab.id }) },
   { kind: 'submenu', id: 'group', label: 'Move to group', icon: 'folder', items: [
    { id: 'new-group', label: 'New group', onSelect: () => dispatch({ type: 'group', id: tab.id }) },
    ...state.groups.map(group => ({ id: group.id, label: group.title, checked: tab.groupId === group.id, onSelect: () => dispatch({ type: 'move-group', id: tab.id, groupId: group.id }) })),
    { id: 'no-group', label: 'No group', disabled: !tab.groupId, onSelect: () => dispatch({ type: 'move-group', id: tab.id }) },
   ] },
   { kind: 'separator', id: 'close-separator' },
   { id: 'close', label: 'Close tab', icon: 'close', shortcut: '⌘/Ctrl W', onSelect: () => closeTab(tab.id) },
   { id: 'reopen', label: 'Reopen closed tab', shortcut: '⌘/Ctrl ⇧ T', disabled: !state.closed.length, onSelect: () => dispatch({ type: 'reopen' }) },
   { kind: 'submenu', id: 'order', label: 'Move tab', disabled: state.tabs.length < 2, items: state.tabs.filter(t => t.id !== tab.id).map(target => ({ id: target.id, label: `Before ${target.title}`, onSelect: () => dispatch({ type: 'reorder', id: tab.id, targetId: target.id }) })) },
  ];
 }
 function renderTab(tab: Tab) {
  return <ContextMenu key={tab.id} label={`Actions for ${tab.title}`} items={tabItems(tab)}><div className={`workspace-tab ${tab.pinned ? 'is-pinned' : ''}`} data-active={tab.id === state.activeId} draggable onDragStart={event => { event.dataTransfer.setData('application/codeaf-tab', tab.id); event.dataTransfer.effectAllowed = 'move'; }} onDragOver={event => { if (event.dataTransfer.types.includes('application/codeaf-tab')) event.preventDefault(); }} onDrop={event => { const id = event.dataTransfer.getData('application/codeaf-tab'); if (id) { event.preventDefault(); dispatch({ type: 'reorder', id, targetId: tab.id }); } }}>
   <HoverPreview disabled={!!switcher || overviewOpen || !!rename} title={tab.title} description={tab.draft || 'No draft yet'} meta={tab.groupId ? state.groups.find(g => g.id === tab.groupId)?.title : tab.pinned ? 'Pinned tab' : undefined}><Button className="workspace-tab-select" role="tab" id={`tab-${tab.id}`} aria-controls="workspace-tab-panel" aria-selected={tab.id === state.activeId} tabIndex={tab.id === state.activeId ? 0 : -1} aria-label={tab.title} onClick={() => dispatch({ type: 'select', id: tab.id })} onKeyDown={event => {
    const index = visible.findIndex(t => t.id === tab.id);
    const target = event.key === 'ArrowRight' ? visible[(index + 1) % visible.length] : event.key === 'ArrowLeft' ? visible[(index - 1 + visible.length) % visible.length] : event.key === 'Home' ? visible[0] : event.key === 'End' ? visible[visible.length - 1] : undefined;
    if (target) { event.preventDefault(); dispatch({ type: 'select', id: target.id }); requestAnimationFrame(() => document.getElementById(`tab-${target.id}`)?.focus()); }
   }}><Icon name={tab.pinned ? 'pin' : 'tab'} size="xs"/><span>{tab.title}</span></Button></HoverPreview>
   {!tab.pinned && <IconButton className="workspace-tab-close" label={`Close ${tab.title}`} icon="close" iconSize="xs" tabIndex={tab.id === state.activeId ? 0 : -1} onClick={() => closeTab(tab.id)}/>}
  </div></ContextMenu>;
 }
 const overflowItems: MenuEntry[] = [
  { id: 'new', label: 'New tab', icon: 'plus', shortcut: '⌘/Ctrl T', onSelect: () => dispatch({ type: 'new' }) },
  { id: 'reopen', label: 'Reopen closed tab', disabled: !state.closed.length, onSelect: () => dispatch({ type: 'reopen' }) },
  { kind: 'separator', id: 'tabs-separator' },
  ...state.tabs.map(tab => ({ id: tab.id, label: tab.title, checked: tab.id === state.activeId, onSelect: () => dispatch({ type: 'select', id: tab.id }) })),
 ];
 return <section className="tab-workspace" aria-label="Conversation workspace">
  <div className="workspace-tabbar" data-tauri-drag-region>{leading}<div className="workspace-tablist-owner" role="tablist" aria-label="Conversation tabs" aria-owns={visible.map(tab => `tab-${tab.id}`).join(' ')}/>
   {scrollEdges.left && <IconButton className="workspace-scroll-tabs" label="Scroll tabs left" icon="chevronLeft" iconSize="xs" onClick={() => scrollTabs(-1)}/>}
   <div ref={strip} className="workspace-tabstrip" aria-label="Conversation tabs">
    <div className="workspace-ungrouped-tabs">{state.tabs.filter(t => t.pinned).map(renderTab)}{state.tabs.filter(t => !t.pinned && !t.groupId).map(renderTab)}</div>
    {state.groups.filter(g => state.tabs.some(t => t.groupId === g.id)).map(group => <div className="workspace-tab-group" key={group.id} data-collapsed={group.collapsed}>
     <ContextMenu label={`Actions for group ${group.title}`} items={[
      { id: 'rename', label: 'Rename group', onSelect: () => startRename(group.id, true) },
      { id: 'add', label: 'New tab in group', icon: 'plus', onSelect: () => dispatch({ type: 'new', groupId: group.id }) },
      { id: 'collapse', label: group.collapsed ? 'Expand group' : 'Collapse group', onSelect: () => dispatch({ type: 'collapse-group', id: group.id }) },
      { id: 'ungroup', label: 'Ungroup tabs', onSelect: () => dispatch({ type: 'ungroup', id: group.id }) },
     ]}><Button className="workspace-group-label" aria-expanded={!group.collapsed} onClick={() => dispatch({ type: 'collapse-group', id: group.id })}><Icon name="chevron" size="xs" motion="disclosure"/><span>{group.title}</span><span className="workspace-group-count">{state.tabs.filter(t => t.groupId === group.id).length}</span></Button></ContextMenu>
     <div className="workspace-group-tabs">{state.tabs.filter(t => t.groupId === group.id).map(tab => <div key={tab.id} className="workspace-group-tab-slot" data-hidden={group.collapsed && tab.id !== state.activeId} inert={group.collapsed && tab.id !== state.activeId} aria-hidden={group.collapsed && tab.id !== state.activeId ? true : undefined}>{renderTab(tab)}</div>)}</div>
    </div>)}
   </div>
   {scrollEdges.right && <IconButton className="workspace-scroll-tabs" label="Scroll tabs right" icon="chevronRight" iconSize="xs" onClick={() => scrollTabs(1)}/>}
   <div className="workspace-tab-actions"><IconButton label="New tab" title="New tab (⌘/Ctrl T)" icon="plus" onClick={() => dispatch({ type: 'new' })}/><IconButton ref={overviewTrigger} label="All tabs" icon="grid" onClick={() => setOverviewOpen(true)}/><DropdownMenu label="Tab actions" items={overflowItems}><IconButton label="Tab actions" icon="more"/></DropdownMenu></div>
  </div>
  <div className="workspace-conversation" role="tabpanel" id="workspace-tab-panel" aria-labelledby={`tab-${active.id}`} tabIndex={0}>
   <div className="conversation-empty"><BrandMark size="hero"/><PageHeading>A space for what’s next.</PageHeading><Text>One thought at a time. Keep another close by.</Text></div>
   <div className="conversation-composer"><TextArea aria-label={`Draft for ${active.title}`} placeholder="What would you like to build?" value={active.draft} onChange={event => dispatch({ type: 'draft', id: active.id, draft: event.target.value })}/><div className="conversation-composer-footer"><Text>UI preview · Drafts stay on this device</Text><Button variant="quiet" onClick={onExplore}>Explore the foundation <Icon name="arrow" size="xs" motion="directional"/></Button></div></div>
  </div>
  {switcher && <div className="workspace-switcher"><div ref={switcherFocus} className="workspace-switcher-list" role="listbox" tabIndex={0} aria-label="Switch tabs" aria-activedescendant={`switcher-${switcher.ids[switcher.index]}`}>
   {switcher.ids.map((id, index) => { const tab = state.tabs.find(t => t.id === id); return tab ? <Button key={id} id={`switcher-${id}`} className="workspace-switcher-item" role="option" aria-selected={index === switcher.index} tabIndex={-1} onClick={() => { dispatch({ type: 'select', id }); switcherRef.current = null; setSwitcher(null); }}><Icon name={tab.pinned ? 'pin' : 'tab'} size="sm"/><span>{tab.title}</span></Button> : null; })}
   </div><Text>Release Ctrl to switch · Escape to cancel</Text>
  </div>}
  <TabOverview returnFocus={overviewTrigger} open={overviewOpen} tabs={state.tabs} groups={state.groups} activeId={state.activeId} onClose={() => setOverviewOpen(false)} onSelect={id => dispatch({ type: 'select', id })} onNew={() => dispatch({ type: 'new' })} onPin={id => dispatch({ type: 'pin', id })} onMoveGroup={(id, groupId) => dispatch({ type: 'move-group', id, groupId })} onCreateGroup={id => dispatch({ type: 'group', id })} onCloseTab={id => dispatch({ type: 'close', id })}/>
  <dialog ref={renameDialog} className="workspace-rename" aria-label={rename?.group ? 'Rename group' : 'Rename tab'} onCancel={() => setRename(null)} onClose={() => setRename(null)}>
   <form onSubmit={event => { event.preventDefault(); if (rename) dispatch({ type: rename.group ? 'rename-group' : 'rename', id: rename.id, title: rename.value }); setRename(null); }}><TextInput ref={renameInput} aria-label="Name" value={rename?.value ?? ''} maxLength={80} onChange={event => setRename(current => current ? { ...current, value: event.target.value } : null)}/><div className="workspace-rename-actions"><Button onClick={() => setRename(null)}>Cancel</Button><Button type="submit" variant="secondary">Save</Button></div></form>
  </dialog>
 </section>;
}
