import { useEffect, useId, useRef, useState, type FocusEvent, type KeyboardEvent, type PointerEvent, type ReactElement, type ReactNode } from 'react';
import * as Context from '@radix-ui/react-context-menu';
import * as Dropdown from '@radix-ui/react-dropdown-menu';
import { synthesizeContextMenu } from './contextMenuEvent';
import { Icon, type IconName } from './Icon';
import { KeyboardShortcut } from './KeyboardShortcut';
import design from '../../design/tokens.json';

/** One square in a swatches row. `id` is a place tint (`tide`, `rose`, …); an id with no token paints nothing. */
export type MenuSwatchOption = { id: string; label: string };
export type MenuEntry =
 | { kind?: 'action'; id: string; label: string; icon?: IconName; /** An empty 14px column, so words line up with rows that have a glyph. */ iconSlot?: boolean; shortcut?: string; detail?: string; disabled?: boolean; checked?: boolean; danger?: boolean; onSelect: () => void }
 | { kind: 'separator'; id: string }
 | { kind: 'submenu'; id: string; label: string; icon?: IconName; iconSlot?: boolean; disabled?: boolean; items: readonly MenuEntry[] }
 | { kind: 'swatches'; id: string; label: string; icon?: IconName; options: readonly MenuSwatchOption[]; selected?: string; onSelect: (id: string) => void };
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
function EntryContents({ entry }: { entry: Exclude<MenuEntry, { kind: 'separator' } | { kind: 'swatches' }> }) {
 // A row with no glyph still holds the icon column. Delete place… is danger words only, and the words stay aligned.
 return <>{entry.icon ? <Icon name={entry.icon} size="sm"/> : entry.iconSlot ? <span className="menu-icon-slot" aria-hidden="true"/> : null}<span className="menu-label">{entry.label}</span>{entry.kind === 'submenu'
  ? <Icon name="chevronRight" size="xs"/>
  : <>{'detail' in entry && entry.detail && <span className="menu-detail">{entry.detail}</span>}{entry.shortcut && <KeyboardShortcut label={entry.shortcut} variant="inline"/>}</>}</>;
}
function MenuItems({ items, type, container }: { items: readonly MenuEntry[]; type: 'context' | 'dropdown'; container?: HTMLElement }) {
 const P = type === 'context' ? Context : Dropdown;
 return <>{items.map(entry => {
  if (entry.kind === 'separator') return <P.Separator key={entry.id} className="menu-separator"/>;
  if (entry.kind === 'swatches') return <SwatchRow key={entry.id} entry={entry} Item={P.Item}/>;
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
type SwatchEntry = Extract<MenuEntry, { kind: 'swatches' }>;
type SwatchItem = (props: {
 role?: string;
 className?: string;
 'aria-label'?: string;
 'aria-orientation'?: 'horizontal';
 'aria-activedescendant'?: string;
 onSelect?: (event: Event) => void;
 onKeyDown?: (event: KeyboardEvent<HTMLDivElement>) => void;
 onPointerDown?: (event: PointerEvent<HTMLDivElement>) => void;
 children?: ReactNode;
}) => ReactNode;
function SwatchRow({ entry, Item }: { entry: SwatchEntry; Item: SwatchItem }) {
 const uid = useId();
 const fromPointer = useRef(false);
 const picked = useRef<string | undefined>(undefined);
 const optionKey = entry.options.map(option => option.id).join('\0');
 const chosen = entry.options.some(option => option.id === entry.selected) ? entry.selected : undefined;
 const [cursor, setCursor] = useState(chosen);
 // A fresh selection from outside (the menu stayed open) puts the ring back on that square.
 useEffect(() => { setCursor(chosen); }, [chosen, optionKey]);
 const move = (step: number) => {
  if (entry.options.length === 0) return;
  const index = entry.options.findIndex(option => option.id === cursor);
  // No ring yet (graphite, or any selected id the row does not offer): the first arrow lands on an end.
  const next = index < 0
   ? entry.options[step > 0 ? 0 : entry.options.length - 1]
   : entry.options[(index + step + entry.options.length) % entry.options.length];
  setCursor(next.id);
 };
 if (entry.options.length === 0) return null;
 return <Item role="radiogroup" aria-label={entry.label} aria-orientation="horizontal" aria-activedescendant={cursor ? `${uid}-${cursor}` : undefined} className="menu-swatch-entry"
  onPointerDown={event => {
   fromPointer.current = true;
   const hit = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-swatch]') : null;
   picked.current = hit?.dataset.swatch;
  }}
  onKeyDown={event => {
   // Enter's click follows this keydown. A leftover pointer flag would treat that Enter as a click on empty space.
   fromPointer.current = false;
   if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
   event.preventDefault();
   move(event.key === 'ArrowRight' ? 1 : -1);
  }}
  onSelect={event => {
   const id = fromPointer.current ? picked.current : cursor;
   fromPointer.current = false;
   picked.current = undefined;
   // Arrows only move the ring. Enter and a click on a square are what pick, and a pick with nowhere to land leaves the menu up.
   if (!id) { event.preventDefault(); return; }
   entry.onSelect(id);
  }}>
  <div className="menu-item menu-swatch-label" aria-hidden="true">{entry.icon && <Icon name={entry.icon} size="sm"/>}<span className="menu-label">{entry.label}</span></div>
  <div className="menu-swatches">{entry.options.map(option => <span key={option.id} id={`${uid}-${option.id}`} role="radio" aria-checked={option.id === cursor} aria-label={option.label} data-swatch={option.id} data-selected={option.id === cursor || undefined} className="menu-swatch"/>)}</div>
 </Item>;
}
export function ContextMenu({ children, items, label, onOpenChange, wide = false, className = '' }: MenuProps) {
 const layer = useMenuLayer<HTMLSpanElement>(onOpenChange);
 return <Context.Root onOpenChange={layer.onOpenChange}>
  <Context.Trigger ref={layer.ref} asChild onKeyDown={event => {
   // macOS WebKit does not synthesize this browser event for Shift+F10. The same helper opens the menu for a coarse long-press.
   if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return;
   event.preventDefault();
   synthesizeContextMenu(event.currentTarget);
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
