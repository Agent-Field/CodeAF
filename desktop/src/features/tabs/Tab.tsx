import type { HTMLAttributes, ReactElement, Ref } from 'react';
import { Button, Icon, IconButton } from '../../components/ui';
import { kindDef } from './kinds/registry';
import type { TabKind } from './kinds/types';
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
 */
export function KindIcon({ kind, title, monogram, favicon }: { kind: TabKind; title: string; monogram?: string; favicon?: string }) {
  if (kind === 'web') {
    const source = monogram?.trim() || title;
    return favicon
      ? <img className="tab-monogram" src={favicon} alt="" aria-hidden="true"/>
      : <span className="tab-monogram" data-hue={monogramHue(source)} aria-hidden="true">{source.trim().charAt(0).toLowerCase()}</span>;
  }
  const def = kindDef(kind);
  return <Icon name={def.glyph?.(title) ?? def.icon} size={kind === 'inbox' ? 'sm' : 'xs'}/>;
}

/** The leading mark: an amber or red 6px dot when the tab needs you or failed, otherwise the kind glyph. */
export function TabGlyph({ kind, title, monogram, state, favicon }: { kind: TabKind; title: string; monogram?: string; state?: TabState; favicon?: string }) {
  if (state) return <span className="tab-dot" data-state={state} role="img" aria-label={tabStateLabel[state]}/>;
  return <KindIcon kind={kind} title={title} monogram={monogram} favicon={favicon}/>;
}

type FrameProps = Omit<HTMLAttributes<HTMLDivElement>, 'onSelect' | 'onKeyDown' | 'id' | 'title'> & { ref?: Ref<HTMLDivElement> };
export type TabProps = FrameProps & {
  kind: TabKind;
  title: string;
  /** A web tab's mark source: its site. Falls back to the title. */
  monogram?: string;
  active?: boolean;
  pinned?: boolean;
  state?: TabState;
  /** Inbox only: the one pinned tab that carries a dot. */
  badge?: boolean;
  /** Icon-only at the narrowest widths (the 44px "compressed" tab). */
  compressed?: boolean;
  /** Forces the hover look, for specimens. */
  hover?: boolean;
  /** Alt held: the close slot becomes a stop square (close and stop). */
  closeMode?: 'close' | 'stop';
  tabIndex?: number;
  /** Wraps the select button; the preview lane puts the hover card here. */
  wrapSelect?: (select: ReactElement) => ReactElement;
  onSelect?: () => void;
  onClose?: () => void;
  onRename?: () => void;
  onKeyDown?: HTMLAttributes<HTMLButtonElement>['onKeyDown'];
  /** Props for the outer element (drag handlers, data attributes). */
  frame?: HTMLAttributes<HTMLDivElement> & { draggable?: boolean };
  id?: string;
  inGroup?: boolean;
  /** Specimen mode: a plain button with no tab role, for pages that show a tab outside any tablist. */
  specimen?: boolean;
};

/**
 * One tab, per design 2h "Tabs": 30px, radius 8, 13px kind glyph, 12px title faded over its last 20px,
 * a close in a fixed 20px slot. All state is props; the primitive owns no data and no menus.
 */
export function Tab({ kind, title, monogram, active = false, pinned = false, state, badge = false, compressed = false, hover = false, closeMode = 'close', tabIndex, wrapSelect, onSelect, onClose, onRename, onKeyDown, frame, id, inGroup = false, specimen = false, ...rest }: TabProps) {
  const select = (
    <Button className="workspace-tab-select" role={specimen ? undefined : 'tab'} id={id} aria-controls={specimen ? undefined : 'workspace-tab-panel'} aria-selected={specimen ? undefined : active} aria-current={specimen && active ? true : undefined} aria-label={title} aria-description={state ? tabStateLabel[state] : undefined} tabIndex={tabIndex ?? (active ? 0 : -1)} onClick={onSelect} onDoubleClick={onRename} onKeyDown={onKeyDown}>
      <TabGlyph kind={kind} title={title} monogram={monogram} state={state}/>
      {!pinned && !compressed && <span className="workspace-tab-title">{title}</span>}
      {badge && <span className="tab-badge" aria-hidden="true"/>}
    </Button>
  );
  const stop = closeMode === 'stop';
  return (
    <div {...rest} {...frame} className={`workspace-tab ${pinned ? 'is-pinned' : ''} ${frame?.className ?? ''}`} data-active={active} data-kind={kind} data-state={state} data-hover={hover || undefined} data-compressed={compressed || undefined} data-in-group={inGroup || undefined}>
      {wrapSelect ? wrapSelect(select) : select}
      {!pinned && !compressed && onClose && <span className="workspace-tab-close-slot"><IconButton className="workspace-tab-close" label={stop ? `Close and stop ${title}` : `Close ${title}`} title={stop ? 'Close and stop (⌥⌘W)' : undefined} icon={stop ? 'stop' : 'close'} iconSize="micro" tabIndex={active ? 0 : -1} onClick={onClose}/></span>}
    </div>
  );
}
