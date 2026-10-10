import type { ReactNode } from 'react';
import { isMac, shellShortcuts } from '../../design/keyboard';
import type { IconName } from '../../components/ui';
import type { RailSections } from '../places/shell/selectors';
import { PlaceRailSections, type PlaceRailActions, type RailNow } from '../places/rail/PlaceRail';
import { RailRow } from '../places/rail/RailRow';
import { SETTINGS_TAB_ICON } from '../settings/summary';
import { RailToggle } from './RailToggle';
import { designSystemRow } from './railLegacy';
import { useLongPress } from './useLongPress';
import './rail.css';
import './place-rail.css';
import './touch.css';

export type { PlaceRailActions };

/** A rail row that opens one window page. The chord is drawn in ink-3 and kept out of the accessible name. */
export type RailLink = { active: boolean; onOpen: () => void };

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
  /** Opens the Settings tab. Absent on a specimen that pictures only the place rail. */
  settings?: RailLink;
  /** Development builds only. PlaceRail drops this unless the build is a development build. */
  designSystem?: RailLink;
};

/** One bottom row. It is not a place, so it stays out of the place list's arrow keys (End still lands on All places). */
function footerRow(name: string, icon: IconName, link: RailLink, shortcut?: string) {
  return <RailRow name={name} icon={icon} active={link.active}
    aria-keyshortcuts={shortcut ? (isMac ? 'Meta+Comma' : 'Control+Comma') : undefined}
    meta={shortcut ? <span aria-hidden="true">{shortcut}</span> : undefined}
    onContextMenu={event => event.preventDefault()}
    onClick={link.onOpen}/>;
}

/**
 * The rail chrome: the toggle, then the place sections (Now, Pinned, Open, All places), then Settings.
 * There is no Inbox row: Iteration 2 I2.1 moved that count to the frame pill.
 * There is no address field, no Activity row, no Find anything row and no theme control.
 * On first launch the sections are Now and All places, with no heading, and Settings still sits at the bottom.
 */
export function PlaceRail(props: PlaceRailProps) {
  // The shared listener keeps finger holds working after the place list moved into its own component.
  useLongPress();
  const { inert, peeking, onToggle, settings, designSystem, ...sections } = props;
  // Above Settings, so Settings stays the bottom row. Production never receives the design row.
  const design = designSystemRow(import.meta.env.DEV, designSystem);
  const foot: ReactNode = (design || settings) ? <>
    {design && footerRow('Design system', 'grid', design)}
    {settings && footerRow('Settings', SETTINGS_TAB_ICON, settings, shellShortcuts.settings)}
  </> : undefined;

  return <aside className="sidebar rail place-rail" aria-label="Main navigation" inert={inert}>
    <div className="rail-head" data-tauri-drag-region>
      <RailToggle placement="rail" collapsed={peeking} onClick={onToggle}/>
    </div>
    <PlaceRailSections {...sections} foot={foot}/>
  </aside>;
}
