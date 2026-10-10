import type { MouseEvent } from 'react';
import { Button, ContextMenu, Icon, type MenuEntry } from '../../../components/ui';
import { PlaceSwatch, type TintName } from '../components/PlaceSwatch';
import './home-tab.css';

export type HomeTabAppearance = 'hover' | 'pressed' | 'focus';

export type HomeTabProps = {
  /** The place's name. This tab shows that name, not the word Home. */
  name: string;
  tint: TintName;
  /** The page behind the strip is this place's Home. Canvas and the sh-1 lift. */
  active?: boolean;
  /**
   * The rail is put away (Places 9c). The tab grows a chevron and a click opens the place
   * switcher instead of focusing Home (HT-1).
   */
  switcher?: boolean;
  /** True while that switcher is open, so the tab can say so. Omit when the parent has not opened one. */
  switcherOpen?: boolean;
  /**
   * Another place needs you. The words are announced with the tab ("Needs you in Config parser")
   * and a 6px amber dot is drawn on the swatch. Blank or absent draws nothing: the rail already
   * carries the dot while it is open, so the shell sets this only with the rail collapsed.
   */
  needsYou?: string;
  /** Right-click opens this place menu (Interactions "Home tab (pinned)"). No entries, no menu. */
  menu?: readonly MenuEntry[];
  /** Click while the rail is open: focus this place's Home. */
  onSelect?: () => void;
  /** Click while the rail is collapsed: open the place switcher. */
  onOpenSwitcher?: () => void;
  /** Specimen only: draw a state the pointer is not in. */
  appearance?: HomeTabAppearance;
  tabIndex?: number;
  id?: string;
};

/**
 * The strip's pinned first tab (Places 6a, 9c). Swatch plus the place name, 30px, never a close.
 * The trailing hairline is the rule before the other tabs. The strip's leading slot is where this
 * mounts; this component does not place itself.
 */
export function HomeTab({ name, tint, active = false, switcher = false, switcherOpen, needsYou, menu, onSelect, onOpenSwitcher, appearance, tabIndex, id }: HomeTabProps) {
  const alert = needsYou?.trim() ?? '';
  const hasMenu = !!menu && menu.length > 0;
  const activate = () => { if (switcher) onOpenSwitcher?.(); else onSelect?.(); };
  // A right-click is the place menu. Stopping it here keeps the strip's own background menu from opening too.
  const keepPlaceMenu = (event: MouseEvent<HTMLDivElement>) => { if (hasMenu) event.stopPropagation(); };
  const chip = (
    <div className="home-tab" data-active={active} data-switcher={switcher || undefined} data-force={appearance} onContextMenu={keepPlaceMenu}>
      <Button className="home-tab-select" id={id} role="tab" aria-selected={active} aria-haspopup={switcher ? 'menu' : undefined} aria-expanded={switcher && switcherOpen !== undefined ? switcherOpen : undefined} aria-description={alert || undefined} tabIndex={tabIndex ?? (active ? 0 : -1)} onClick={activate}>
        <span className="home-tab-mark">
          <PlaceSwatch tint={tint} role="rail"/>
          {alert && <span className="home-tab-alert" aria-hidden="true"/>}
        </span>
        {name && <span className="home-tab-name">{name}</span>}
        {switcher && <span className="home-tab-switcher"><Icon name="switcher" size="tiny"/></span>}
      </Button>
    </div>
  );
  return (
    <div className="home-tab-slot">
      {hasMenu ? <ContextMenu items={menu} label="Place menu">{chip}</ContextMenu> : chip}
      <span className="home-tab-hairline" role="separator" aria-orientation="vertical" aria-hidden="true"/>
    </div>
  );
}
