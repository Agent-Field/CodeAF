import { useRef, useState } from 'react';
import type { HTMLAttributes, MouseEvent, ReactElement, Ref } from 'react';
import { Button, Icon, IconButton, useTooltip, type IconName } from '../../components/ui';
import { kindDef } from './kinds/registry';
import { PlaceSwatch, type TintName } from '../places/components/PlaceSwatch';
import type { TabKind } from './kinds/types';
import { useTitleOverflow } from './useTitleOverflow';
import './tab.css';

/** Running is deliberately silent on a tab (3j): only these two states ever replace the type icon. */
export type TabState = 'waiting' | 'failed';
export const tabStateLabel: Record<TabState, string> = { waiting: 'Needs you', failed: 'Failed' };

const hueCount = 6;
/** A stable 1..6 hue for a site monogram, so the same title is always the same colour. */
export function monogramHue(title: string): number {
  let hash = 0;
  for (const ch of title) hash = (hash * 31 + ch.codePointAt(0)!) % 997;
  return (hash % hueCount) + 1;
}

/**
 * The kind glyph: a Lucide icon, or for web the favicon if there is one, else a monogram. The monogram is the page's
 * site (`monogram`, from the pane's address), not its title: a page's title changes as it loads and the mark must not.
 * `icon` is the kind's iconFor result, so a file tab draws its type from the path. Absent, the title glyph or the kind icon draws.
 */
export function KindIcon({ kind, title, icon, monogram, favicon }: { kind: TabKind; title: string; icon?: IconName; monogram?: string; favicon?: string }) {
  const [failed, setFailed] = useState<string>();
  if (kind === 'web') {
    const source = monogram?.trim() || title;
    return favicon && failed !== favicon
      ? <img className="tab-monogram" src={favicon} onError={() => setFailed(favicon)} alt="" aria-hidden="true"/>
      : <span className="tab-monogram" data-hue={monogramHue(source)} aria-hidden="true">{source.trim().charAt(0).toLowerCase()}</span>;
  }
  const def = kindDef(kind);
  return <Icon name={icon ?? def.glyph?.(title) ?? def.icon} size={'xs'}/>;
}

/** The leading mark: an amber or red 6px dot when the tab needs you or failed, otherwise the kind glyph. */
export function TabGlyph({ kind, title, icon, monogram, state, favicon }: { kind: TabKind; title: string; icon?: IconName; monogram?: string; state?: TabState; favicon?: string }) {
  if (state) return <span className="tab-dot" data-state={state} role="img" aria-label={tabStateLabel[state]}/>;
  return <KindIcon kind={kind} title={title} icon={icon} monogram={monogram} favicon={favicon}/>;
}

type FrameProps = Omit<HTMLAttributes<HTMLDivElement>, 'onSelect' | 'onKeyDown' | 'id' | 'title'> & { ref?: Ref<HTMLDivElement> };
export type TabProps = FrameProps & {
  kind: TabKind;
  title: string;
  /** A per-tab icon from the kind's iconFor. A file tab passes its type icon. */
  icon?: IconName;
  /** A web tab's mark source: its site. Falls back to the title. */
  monogram?: string;
  favicon?: string;
  active?: boolean;
  pinned?: boolean;
  state?: TabState;
  /** A corner glyph reports real needs-you or failed work supplied by the tab owner; absence stays quiet. */
  badge?: false | 'needsYou' | 'failed';
  /** Icon-only at the narrowest widths (the 44px "compressed" tab). */
  compressed?: boolean;
  /** Forces the hover look, for specimens. */
  hover?: boolean;
  /** Picked with ⌘-click (Ctrl-click on Linux) for ⌘G: a field fill, and "selected for grouping" for a screen reader. */
  picked?: boolean;
  /** Alt held: the close slot becomes a stop square (close and stop). */
  closeMode?: 'close' | 'stop';
  /** The close button's tooltip text ("Close · keeps running") and its muted shortcut. */
  closeHint?: string;
  closeShortcut?: string;
  tabIndex?: number;
  /** Wraps the select button; the preview lane puts the hover card here. */
  wrapSelect?: (select: ReactElement) => ReactElement;
  /** The click, so the caller can tell a ⌘-click (pick) from a plain one (select). */
  onSelect?: (event: MouseEvent<HTMLButtonElement>) => void;
  onClose?: () => void;
  onRename?: () => void;
  onKeyDown?: HTMLAttributes<HTMLButtonElement>['onKeyDown'];
  /** Props for the outer element (the pointer drag, data attributes). */
  frame?: HTMLAttributes<HTMLDivElement> & { ref?: Ref<HTMLDivElement> };
  id?: string;
  inGroup?: boolean;
  /** Specimen mode: a plain button with no tab role, for pages that show a tab outside any tablist. */
  specimen?: boolean;
  /** A place's Home tab: its tint square replaces the kind glyph and its name stays visible though it is pinned (Shell 3j). */
  placeTint?: TintName;
  /** The rail is put away: the Home tab is the place switcher (⇅), with an amber glyph when another place needs you (Places 9c, 9e). */
  switcher?: { alert?: string };
  /** A hover preview is open on the strip. The full-title tooltip stays shut so the two never stack. */
  previewOpen?: boolean;
  /** Muted words after the title, such as a finished job's `exit 0`. Absent draws nothing. */
  meta?: string;
};

/**
 * One tab, per design 2h "Tabs": 30px, radius 8, 13px kind glyph, 12px title faded over its last 20px,
 * a close in a fixed 20px slot. All state is props; the primitive owns no data and no menus.
 * `meta` sits after the title, outside its fade, so a finished job's `exit 0` stays readable (C-EDGE-5).
 * The full title is a shared tooltip only when the name is cut and this tab does not open a hover
 * preview (the active tab, and a compressed tab). The delay is the tooltip's own 500ms, the same
 * number as the preview open delay. Pressing the chip fills it with field-2 (tab.css).
 */
export function Tab({ kind, title, icon, monogram, favicon, active = false, pinned = false, state, picked = false, badge = false, compressed = false, hover = false, closeMode = 'close', closeHint, closeShortcut, tabIndex, wrapSelect, onSelect, onClose, onRename, onKeyDown, frame, id, inGroup = false, specimen = false, placeTint, switcher, previewOpen = false, meta, ...rest }: TabProps) {
  const home = !!placeTint;
  const titleShown = home || (!pinned && !compressed);
  const titleRef = useRef<HTMLSpanElement>(null);
  const overflow = useTitleOverflow(titleRef, !titleShown);
  // An inactive tab's preview already carries the title. A card that is still open keeps this shut.
  const tip = overflow && (active || compressed) && !previewOpen ? title : '';
  const tooltip = useTooltip<HTMLButtonElement>(tip);
  const select = (
    <Button {...tooltip.props} className="workspace-tab-select" role={specimen ? undefined : 'tab'} id={id} aria-controls={specimen ? undefined : 'workspace-tab-panel'} aria-selected={specimen ? undefined : active} aria-current={specimen && active ? true : undefined} aria-label={title} aria-description={[state && tabStateLabel[state], titleShown && meta, picked && 'selected for grouping', !state && badge && (badge === 'failed' ? 'A task failed' : 'Needs you')].filter(Boolean).join(', ') || undefined} tabIndex={tabIndex ?? (active ? 0 : -1)} onClick={onSelect} onDoubleClick={onRename} onKeyDown={onKeyDown}>
      {home && !state ? <PlaceSwatch tint={placeTint} role="rail"/> : <TabGlyph icon={icon} favicon={favicon} kind={kind} title={title} monogram={monogram} state={state}/>}
      {titleShown && <span ref={titleRef} className="workspace-tab-title">{title}</span>}
      {titleShown && meta && <span className="workspace-tab-meta">{meta}</span>}
      {home && switcher && <>{switcher.alert && <span className="tab-dot" data-state="waiting" role="img" aria-label={switcher.alert}/>}<Icon name="switcher" size="micro"/></>}
      {badge && <span className="tab-badge" data-kind={badge} aria-hidden="true"/>}
    </Button>
  );
  const stop = closeMode === 'stop';
  return (
    <div {...rest} {...frame} className={`workspace-tab ${pinned && !home ? 'is-pinned' : ''} ${home ? 'is-place-home' : ''} ${frame?.className ?? ''}`} data-active={active} data-kind={kind} data-state={state} data-hover={hover || undefined} data-compressed={compressed || undefined} data-in-group={inGroup || undefined} data-picked={picked || undefined} data-selected={picked || undefined} data-title-overflow={overflow || undefined}>
      {wrapSelect ? wrapSelect(select) : select}
      {tooltip.element}
      {!pinned && !compressed && onClose && <span className="workspace-tab-close-slot"><IconButton className="workspace-tab-close" label={stop ? `Close and stop ${title}` : `Close ${title}`} title={closeHint} shortcut={closeShortcut} data-close-mode={closeMode} icon={stop ? 'stop' : 'close'} iconSize="micro" tabIndex={active ? 0 : -1} onClick={onClose}/></span>}
    </div>
  );
}
