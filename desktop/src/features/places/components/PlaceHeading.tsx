import { useEffect, useRef, type HTMLAttributes, type MouseEvent } from 'react';
import { Button, DropdownMenu, HomeTitle, Icon, IconButton, TextInput, type MenuEntry } from '../../../components/ui';
import { PlaceSwatch, type TintName } from './PlaceSwatch';
import './places-components.css';

export type Crumb = {
  id: string;
  label: string;
  /** Click: Go to that place, or the root. */
  onGo: () => void;
  /** Command or Control click, or middle click. */
  onGoInNewWindow?: () => void;
};

type PlaceHeadingProps = Omit<HTMLAttributes<HTMLElement>, 'title'> & {
  title: string;
  /** The effective tint (inherited or its own). */
  tint: TintName;
  /** Ancestors, outermost first ("All places"). Empty for the root, which draws no breadcrumb at all. */
  breadcrumb?: readonly Crumb[];
  /** The ⋯ menu. Without entries there is no button: a menu with nothing in it is not drawn. */
  menu?: readonly MenuEntry[];
  menuLabel?: string;
  /** Inline rename (Q-14): the title becomes a field holding the current name. Enter commits, Escape cancels. */
  renaming?: boolean;
  onRename?: (name: string) => void;
  onRenameCancel?: () => void;
};

function CrumbLink({ crumb }: { crumb: Crumb }) {
  const go = (event: MouseEvent<HTMLButtonElement>) => {
    if ((event.metaKey || event.ctrlKey) && crumb.onGoInNewWindow) crumb.onGoInNewWindow(); else crumb.onGo();
  };
  return <Button variant="ghost" className="places-crumb" onClick={go}
    onAuxClick={event => { if (event.button === 1 && crumb.onGoInNewWindow) { event.preventDefault(); crumb.onGoInNewWindow(); } }}>{crumb.label}</Button>;
}

/** Longer than the shared menu's exit animation (duration-overlay). */
const menuHandoffMs = 1000;

function RenameField({ title, onRename, onCancel }: { title: string; onRename?: (name: string) => void; onCancel?: () => void }) {
  const field = useRef<HTMLInputElement>(null);
  // The menu that started the rename gives focus back to its button once its exit animation ends. For that short window the field
  // takes focus back from a menu button, and only from one: never from something the person chose.
  useEffect(() => {
    const claim = () => { field.current?.focus(); field.current?.select(); };
    const reclaim = (event: FocusEvent) => { if ((event.target as Element).hasAttribute('aria-haspopup')) claim(); };
    document.addEventListener('focusin', reclaim);
    const first = setTimeout(claim, 0);
    const done = setTimeout(() => document.removeEventListener('focusin', reclaim), menuHandoffMs);
    return () => { clearTimeout(first); clearTimeout(done); document.removeEventListener('focusin', reclaim); };
  }, []);
  return <TextInput ref={field} className="places-heading-rename type-home-title" defaultValue={title} aria-label="Place name" autoComplete="off" spellCheck={false}
    onKeyDown={event => {
      if (event.nativeEvent.isComposing) return;
      if (event.key === 'Enter') { event.preventDefault(); const name = event.currentTarget.value.trim(); if (name && name !== title) onRename?.(name); else onCancel?.(); }
      else if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onCancel?.(); }
    }}/>;
}

/** The anatomy every place page, History and All places share (Components "Places · page"): a breadcrumb, then the tint square,
 * the 28px title and the ⋯ menu. Sections follow under their own 11px labels, outside this component. */
export function PlaceHeading({ title, tint, breadcrumb = [], menu, menuLabel, renaming, onRename, onRenameCancel, className = '', ...props }: PlaceHeadingProps) {
  return <header {...props} className={`places-heading ${className}`}>
    {breadcrumb.length > 0 && <nav aria-label="Breadcrumb"><ol className="places-crumbs">
      {breadcrumb.map((crumb, index) => <li key={crumb.id} className="places-crumb-item">
        {index > 0 && <Icon name="chevronRight" size="micro"/>}
        <CrumbLink crumb={crumb}/>
      </li>)}
    </ol></nav>}
    <div className="places-heading-row">
      <PlaceSwatch tint={tint} role="title"/>
      {renaming ? <RenameField title={title} onRename={onRename} onCancel={onRenameCancel}/> : <HomeTitle className="places-heading-title">{title}</HomeTitle>}
      {!renaming && menu && menu.length > 0 && <DropdownMenu items={menu} label={menuLabel ?? `${title} actions`}>
        <IconButton className="places-heading-menu" label="Place actions" icon="more" iconSize="sm"/>
      </DropdownMenu>}
    </div>
  </header>;
}
