import { useEffect, useRef, useState, type RefObject } from 'react';
import { Button, ContextMenu, DropdownMenu, Icon, IconButton, SectionHeading, Text, TextInput, type MenuEntry } from '../../components/ui';
import type { Tab, TabGroup } from './model';
import './overview.css';

type Props = {
 open: boolean; tabs: readonly Tab[]; groups: readonly TabGroup[]; activeId: string;
 onClose: () => void; onSelect: (id: string) => void; onNew: () => void;
 onPin: (id: string) => void; onMoveGroup: (id: string, groupId?: string) => void;
 onCreateGroup: (id: string) => void; onCloseTab?: (id: string) => void;
 returnFocus?: RefObject<HTMLElement | null>;
};

export function TabOverview({ open, tabs, groups, activeId, onClose, onSelect, onNew, onPin, onMoveGroup, onCreateGroup, onCloseTab, returnFocus }: Props) {
 const dialog = useRef<HTMLDialogElement>(null);
 const search = useRef<HTMLInputElement>(null);
 const previousFocus = useRef<HTMLElement | null>(null);
 const [query, setQuery] = useState('');
 useEffect(() => {
  if (open && !dialog.current?.open) {
   previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
   dialog.current?.showModal();
   search.current?.focus();
  } else if (!open) {
   dialog.current?.close();
   setQuery('');
  }
 }, [open]);
 const matches = tabs.filter(tab => `${tab.title} ${tab.draft} ${groups.find(group => group.id === tab.groupId)?.title ?? ''}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
 const ordered = [
  ...matches.filter(tab => tab.pinned),
  ...matches.filter(tab => !tab.pinned && !tab.groupId),
  ...groups.flatMap(group => matches.filter(tab => tab.groupId === group.id)),
 ];
 function items(tab: Tab): MenuEntry[] {
  return [
   { id: 'pin', label: tab.pinned ? 'Unpin tab' : 'Pin tab', icon: 'pin', onSelect: () => onPin(tab.id) },
   { kind: 'submenu', id: 'group', label: 'Move to group', items: [
    { id: 'create', label: 'New group', icon: 'plus', onSelect: () => onCreateGroup(tab.id) },
    { kind: 'separator', id: 'group-separator' },
    { id: 'none', label: 'No group', checked: !tab.groupId, onSelect: () => onMoveGroup(tab.id) },
    ...groups.map(group => ({ id: group.id, label: group.title, checked: tab.groupId === group.id, onSelect: () => onMoveGroup(tab.id, group.id) })),
   ] },
   ...(onCloseTab ? [{ kind: 'separator' as const, id: 'close-separator' }, { id: 'close', label: 'Close tab', icon: 'close' as const, onSelect: () => onCloseTab(tab.id) }] : []),
  ];
 }
 return <dialog ref={dialog} className="tab-overview" aria-label="All tabs overview" onCancel={onClose} onClose={() => {
  onClose();
  // Safari pointer clicks do not focus buttons, so the owner supplies the reliable return target.
  const target = returnFocus?.current ?? previousFocus.current;
  if (target?.isConnected) target.focus();
 }} onClick={event => {
  if (event.target !== event.currentTarget) return;
  const bounds = event.currentTarget.getBoundingClientRect();
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) onClose();
 }}>
  <div className="overview-header"><div className="overview-heading"><SectionHeading>Tabs</SectionHeading><span className="overview-count">{tabs.length}</span></div><div className="overview-filter"><Icon name="search" size="sm"/><TextInput ref={search} aria-label="Filter tabs" placeholder="Find tabs…" value={query} onChange={event => setQuery(event.target.value)}/></div><div className="overview-actions"><IconButton label="New tab" icon="plus" onClick={() => { onNew(); onClose(); }}/><IconButton label="Close all tabs overview" icon="close" onClick={onClose}/></div></div>
  <div className="overview-scroll">
   <div className="overview-grid">
    {ordered.map(tab => <ContextMenu key={tab.id} label={`Actions for ${tab.title} preview`} items={items(tab)}><div className="overview-card" data-active={tab.id === activeId}>
     <Button className="overview-preview" aria-label={`Open ${tab.title}`} aria-pressed={tab.id === activeId} onClick={() => { onSelect(tab.id); onClose(); }}>
      {tab.draft ? <span className="overview-draft">{tab.draft}</span> : <span className="overview-empty"><Icon name="tab" size="md"/></span>}
     </Button>
     <div className="overview-card-footer"><div className="overview-card-label">{tab.pinned && <Icon name="pin" size="xs"/>}<span className="overview-card-title">{tab.title}</span>{tab.groupId && <span className="overview-group-name">{groups.find(group => group.id === tab.groupId)?.title}</span>}</div><DropdownMenu label={`Organize ${tab.title}`} items={items(tab)}><IconButton className="overview-card-menu" label={`Organize ${tab.title}`} icon="more" iconSize="xs"/></DropdownMenu></div>
    </div></ContextMenu>)}
   </div>
   {!matches.length && <Text className="overview-no-results">No matching tabs</Text>}
  </div>
 </dialog>;
}
