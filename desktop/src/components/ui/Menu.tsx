import { useEffect, useRef, useState, type ReactElement, type FocusEvent } from 'react';
import * as Context from '@radix-ui/react-context-menu';
import * as Dropdown from '@radix-ui/react-dropdown-menu';
import { Icon, type IconName } from './Icon';
import { KeyboardShortcut } from './KeyboardShortcut';
import design from '../../design/tokens.json';

export type MenuEntry =
 | { kind?: 'action'; id: string; label: string; icon?: IconName; shortcut?: string; detail?: string; disabled?: boolean; checked?: boolean; danger?: boolean; onSelect: () => void }
 | { kind: 'separator'; id: string }
 | { kind: 'submenu'; id: string; label: string; icon?: IconName; disabled?: boolean; items: readonly MenuEntry[] };
type MenuProps = { children: ReactElement; items: readonly MenuEntry[]; label: string; className?: string; onOpenChange?: (open: boolean) => void; /** The 240px menu of the design's tab menu; the default is 200px. */ wide?: boolean };

function useMenuLayer<T extends HTMLElement>(onOpenChange?: (open: boolean) => void) {
 const ref = useRef<T>(null);
 const [open, setOpen] = useState(false);
 const modal = ref.current?.closest('dialog') ?? undefined;
 useEffect(() => {
  if (!open) return;
  // Radix traps focus; inert also removes hidden background controls from accessibility checks.
  // Native dialogs own a top layer, so their popup is portaled inside that layer.
  const dialog = ref.current?.closest('dialog');
  const backgrounds = dialog
   ? [...dialog.children].filter((node): node is HTMLElement => node instanceof HTMLElement && !node.matches('.app-menu') && !node.querySelector('.app-menu'))
   : [document.getElementById('root')].filter((node): node is HTMLElement => !!node);
  const previous = backgrounds.map(node => node.inert);
  backgrounds.forEach(node => { node.inert = true; });
  return () => { backgrounds.forEach((node, index) => { node.inert = previous[index]; }); };
 }, [open]);
 return { ref, modal, open, onOpenChange: (value: boolean) => { setOpen(value); onOpenChange?.(value); } };
}
function focusFirstItem(event: FocusEvent<HTMLDivElement>) {
 if (event.target !== event.currentTarget) return;
 // Pointer-open menus also need a keyboard-reachable item when the surface scrolls.
 event.currentTarget.querySelector<HTMLElement>('[role^="menuitem"]:not([data-disabled])')?.focus();
}
function EntryContents({ entry }: { entry: Exclude<MenuEntry, { kind: 'separator' }> }) {
 return <>{entry.icon && <Icon name={entry.icon} size="sm"/>}<span className="menu-label">{entry.label}</span>{entry.kind === 'submenu'
  ? <Icon name="chevronRight" size="xs"/>
  : <>{'detail' in entry && entry.detail && <span className="menu-detail">{entry.detail}</span>}{entry.shortcut && <KeyboardShortcut label={entry.shortcut}/>}</>}</>;
}
function MenuItems({ items, type, container }: { items: readonly MenuEntry[]; type: 'context' | 'dropdown'; container?: HTMLElement }) {
 const P = type === 'context' ? Context : Dropdown;
 return <>{items.map(entry => {
  if (entry.kind === 'separator') return <P.Separator key={entry.id} className="menu-separator"/>;
  if (entry.kind === 'submenu') return <P.Sub key={entry.id}>
   <P.SubTrigger className="menu-item" disabled={entry.disabled}><EntryContents entry={entry}/></P.SubTrigger>
   <P.Portal container={container}><P.SubContent onFocusCapture={focusFirstItem} className={`app-menu app-menu-${type} app-menu-submenu`} sideOffset={design.overlay.subMenuOffset} alignOffset={design.overlay.subMenuAlign} collisionPadding={design.overlay.collisionPadding}>
    <MenuItems items={entry.items} type={type} container={container}/>
   </P.SubContent></P.Portal>
  </P.Sub>;
  const contents = <EntryContents entry={entry}/>;
  const className = `menu-item${entry.danger ? ' menu-item-danger' : ''}`;
  if (entry.checked !== undefined) return <P.CheckboxItem key={entry.id} className={className} checked={entry.checked} disabled={entry.disabled} onSelect={entry.onSelect}>
   {contents}<P.ItemIndicator className="menu-check"><Icon name="check" size="xs"/></P.ItemIndicator>
  </P.CheckboxItem>;
  return <P.Item key={entry.id} className={className} disabled={entry.disabled} onSelect={entry.onSelect}>{contents}</P.Item>;
 })}</>;
}
export function ContextMenu({ children, items, label, onOpenChange, wide = false, className = '' }: MenuProps) {
 const layer = useMenuLayer<HTMLSpanElement>(onOpenChange);
 return <Context.Root onOpenChange={layer.onOpenChange}>
  <Context.Trigger ref={layer.ref} asChild onKeyDown={event => {
   // macOS WebKit does not synthesize this browser event for Shift+F10.
   if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
   event.preventDefault();
   const bounds = event.currentTarget.getBoundingClientRect();
   event.currentTarget.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, button: 2, clientX: bounds.left + bounds.width / 2, clientY: bounds.bottom }));
   // A synthetic contextmenu opens the menu without moving focus, so the first item would not take the next key.
   requestAnimationFrame(() => {
    document.querySelector<HTMLElement>('.app-menu-context[data-state="open"] [role^="menuitem"]:not([data-disabled])')?.focus();
   });
  }}>{children}</Context.Trigger>
  <Context.Portal container={layer.modal}><Context.Content onFocusCapture={focusFirstItem} aria-label={label} className={`app-menu app-menu-context${wide ? ' app-menu-wide' : ''} ${className}`} collisionPadding={design.overlay.collisionPadding}>
   <MenuItems items={items} type="context" container={layer.modal}/>
  </Context.Content></Context.Portal>
 </Context.Root>;
}
export function DropdownMenu({ children, items, label, onOpenChange, className = '' }: MenuProps) {
 const layer = useMenuLayer<HTMLButtonElement>(onOpenChange);
 return <Dropdown.Root open={layer.open} onOpenChange={layer.onOpenChange}>
  <Dropdown.Trigger ref={layer.ref} asChild>{children}</Dropdown.Trigger>
  <Dropdown.Portal container={layer.modal}><Dropdown.Content onFocusCapture={focusFirstItem} aria-label={label} className={`app-menu app-menu-dropdown ${className}`} align="end" sideOffset={design.overlay.sideOffset} collisionPadding={design.overlay.collisionPadding}>
   <MenuItems items={items} type="dropdown" container={layer.modal}/>
  </Dropdown.Content></Dropdown.Portal>
 </Dropdown.Root>;
}
