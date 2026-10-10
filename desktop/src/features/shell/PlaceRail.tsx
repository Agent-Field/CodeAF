import type { ReactNode } from 'react';
import { NavigationItem, ThemeSelect, type IconName } from '../../components/ui';
import type { RailSections } from '../places/shell/selectors';
import { PlaceRailSections, type PlaceRailActions, type RailNow } from '../places/rail/PlaceRail';
import { RailToggle } from './RailToggle';
import './rail.css';
import './place-rail.css';

export type { PlaceRailActions };

/** One of the window's other pages, drawn quietly under All places. `hover` forces the hover fill, for the specimen. */
export type RailItem = { label: string; icon: IconName; active: boolean; onSelect: () => void; hover?: boolean };

export type PlaceRailProps = {
  /** True when the rail is not on screen: its controls leave the tab order. */
  inert: boolean; peeking: boolean;
  onToggle: () => void;
  now: RailNow;
  /** Pinned and Open; absent while the engine has no Places to read. */
  sections?: RailSections;
  /** The place this window shows (its row reads as the open one). */
  current?: string;
  /** True when the graph has places but none is pinned or open: the one quiet line of 10a "Empty". */
  emptyHint?: boolean;
  /** Non-archived places from a completed read. Zero hides the All places shortcut (Places 8e). */
  livePlaces?: number;
  /** One muted line when Places cannot be read, with Retry when retrying can help. */
  notice?: { text: string; onRetry?: () => void };
  allPlaces?: { active: boolean; shortcut: string; onOpen: () => void; onOpenInNewTab?: () => void };
  actions: PlaceRailActions;
  /** The open tab count of a place in this window, for the close hint ("Close Marketing · 4 tabs"). */
  tabCount?: (id: string) => number;
  slotShortcut: (index: number) => string;
  closeShortcut: string;
  newWindowShortcut: string;
  /** The window's other pages (Activity, Settings, Design system), quiet under All places. */
  appItems: readonly RailItem[];
};

/**
 * The rail chrome (toggle, then the place sections). There is no Inbox row: Iteration 2 I2.1 moved
 * that count to the frame pill. On first launch the sections are Now and All places, with no heading.
 */
export function PlaceRail(props: PlaceRailProps) {
  const { inert, peeking, onToggle, appItems, ...sections } = props;
  const foot: ReactNode = appItems.length > 0 ? <>
    <nav className="rail-nav rail-app" aria-label="App">{appItems.map(item => <NavigationItem key={item.label} icon={item.icon} active={item.active} data-hover={item.hover || undefined} onClick={item.onSelect}>{item.label}</NavigationItem>)}</nav>
    <div className="sidebar-bottom"><ThemeSelect/></div>
  </> : undefined;
  return <aside className="sidebar rail place-rail" aria-label="Main navigation" inert={inert}>
    <div className="rail-head" data-tauri-drag-region>
      <RailToggle placement="rail" collapsed={peeking} onClick={onToggle}/>
    </div>
    <PlaceRailSections {...sections} foot={foot}/>
  </aside>;
}
