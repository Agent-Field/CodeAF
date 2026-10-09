import { BrandMark, Icon, IconButton, KeyboardShortcut, NavigationItem, Separator, SidebarAction, ThemeSelect } from '../../components/ui';
import './rail.css';

type RailProps<Page extends string> = {
  pages: readonly Page[]; page: Page; icons: readonly ('code' | 'activity' | 'sliders' | 'grid')[];
  /** True when the rail is not on screen (narrow drawer closed, or collapsed): its controls leave the tab order. */
  inert: boolean; paletteOpen: boolean;
  onNavigate: (page: Page) => void; onHide: () => void; onSearch: () => void;
};

/**
 * The 232px rail (design 2a/2d). Its content is the current sidebar for now; the rail lane restyles it
 * (translucent, tinted with --frame) and adds its sections here without touching App.tsx.
 */
export function Rail<Page extends string>({ pages, page, icons, inert, paletteOpen, onNavigate, onHide, onSearch }: RailProps<Page>) {
  return <aside className="sidebar rail" aria-label="Main navigation" inert={inert}>
    <div className="sidebar-toolbar" data-tauri-drag-region>
      <IconButton label="Hide sidebar" icon="sidebar" title="Hide sidebar (⌘/Ctrl B)" onClick={onHide}/>
    </div>
    <SidebarAction variant="address" aria-haspopup="dialog" aria-expanded={paletteOpen} onClick={onSearch}><BrandMark/><span>codeaf</span><Icon name="search" size="xs"/></SidebarAction>
    <nav>{pages.map((p, i) => <NavigationItem key={p} icon={icons[i]} active={page === p} onClick={() => onNavigate(p)}>{p}</NavigationItem>)}</nav>
    <Separator className="sidebar-divider"/>
    <SidebarAction variant="new" onClick={onSearch}><Icon name="plus" size="sm"/><span>Find anything</span><KeyboardShortcut command="K"/></SidebarAction>
    <div className="sidebar-bottom"><ThemeSelect/></div>
  </aside>;
}
