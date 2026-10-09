import { BrandMark, Icon, NavigationItem, SidebarAction, ThemeSelect, type IconName } from '../../components/ui';
import { RailToggle } from './RailToggle';
import './rail.css';

export type RailItem = { label: string; icon: IconName; active: boolean; onSelect: () => void; /** Draws the hover fill, for the specimen. */ hover?: boolean };

type RailProps = {
  items: readonly RailItem[];
  /** True when the rail is not on screen (narrow drawer closed, or collapsed): its controls leave the tab order. */
  inert: boolean; paletteOpen: boolean;
  /** True while a collapsed rail is only peeking: its toggle then brings it back for good. */
  peeking: boolean;
  onToggle: () => void; onSearch: () => void;
};

/**
 * The 232px rail (design 2a/2d, Places PRAIL): no fill of its own, so the frame (or the native material tinted
 * with it) shows through; a top row with the toggle at its right; then today's items as 32px rail rows. The places
 * the design draws (Inbox, Now, Pinned, Open, All places) wait for an engine that knows places, so none is faked
 * here. Settings opens the Settings tab, so every item is a plain button whose open state reads like a tab.
 */
export function Rail({ items, inert, paletteOpen, peeking, onToggle, onSearch }: RailProps) {
  return <aside className="sidebar rail" aria-label="Main navigation" inert={inert}>
    <div className="rail-head" data-tauri-drag-region>
      <RailToggle placement="rail" collapsed={peeking} onClick={onToggle}/>
    </div>
    <SidebarAction variant="address" aria-haspopup="dialog" aria-expanded={paletteOpen} onClick={onSearch}><BrandMark/><span>codeaf</span><Icon name="search" size="xs"/></SidebarAction>
    <nav className="rail-nav">{items.map(item => <NavigationItem key={item.label} icon={item.icon} active={item.active} data-hover={item.hover || undefined} onClick={item.onSelect}>{item.label}</NavigationItem>)}</nav>
    <div className="sidebar-bottom"><ThemeSelect/></div>
  </aside>;
}
