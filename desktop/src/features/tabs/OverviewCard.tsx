// One overview card (design 3h): kind and state line, title, key content, footer. Readable text, never a
// miniature screenshot. The body is the kind's `preview` renderer when the kind has one; otherwise the draft or
// the latest answer, which is what the engine actually knows.
import type { DragEvent, MouseEvent } from 'react';
import { Button, ContextMenu, Icon, IconButton, type MenuEntry } from '../../components/ui';
import { isMac } from '../../design/keyboard';
import { isBackgroundPress } from './overview-model';
import { relativeTime, type TabMark, type TabSummary } from '../conversation/tabSummary';
import { tabDragType } from './hosts/dragHost';
import { kindDef } from './kinds/registry';
import { PreviewContents } from './preview/PreviewCard';
import { focusedPane, panesOf, type Tab } from './model';

export const markWords: Record<TabMark, string> = { working: 'Working', waiting: 'Needs you', failed: 'Failed' };
const markRank: TabMark[] = ['waiting', 'failed', 'working'];

/** The state a tab shows: the most urgent mark among its panes (needs you, then failed, then working). */
export function tabMark(tab: Tab, summaries: Readonly<Record<string, TabSummary>>): TabMark | undefined {
  const marks = panesOf(tab).map(pane => summaries[pane.id]?.mark);
  return markRank.find(mark => marks.includes(mark));
}

/** What a card says when the kind has no preview renderer: the draft, else the latest answer, else the first line. */
export function cardText(tab: Tab, summaries: Readonly<Record<string, TabSummary>>): string {
  const pane = focusedPane(tab);
  const known = summaries[pane.id];
  return pane.draft.trim() || known?.digest || known?.firstLine || '';
}

export const kindLabel = (tab: Tab) => (tab.split ? 'Split' : kindDef(tab.kind).label);
export const kindIcon = (tab: Tab) => (tab.split ? 'split' as const : kindDef(tab.kind).glyph?.(tab.title) ?? kindDef(tab.kind).icon);

/** The 6px state dot, shared by the card head and the filmstrip label. Running is accent, needs you amber, failed red. */
export function StateDot({ mark }: { mark: TabMark }) {
  return <span className="overview-dot" data-state={mark} aria-hidden="true"/>;
}

/**
 * The press handlers of a card or filmstrip item: a plain click opens, a ⌘/Ctrl-click or a middle-click is the
 * background press. Cancel middle-button autoscroll and guard the ensuing PRIMARY paste separately:
 * Chromium can still send paste to a different focused field after canceled mouse events.
 */
export function backgroundPress(open: () => void, background: () => void) {
  return {
    onClick: (event: MouseEvent) => (isBackgroundPress(event, isMac) ? background() : open()),
    onAuxClick: (event: MouseEvent) => { if (event.button === 1) {
      event.preventDefault();
      // Chromium can paste PRIMARY into the focused filter after a canceled auxclick.
      // Suppress only the paste caused by this background press, then release the guard.
      const doc = event.currentTarget.ownerDocument;
      const preventPaste = (paste: Event) => paste.preventDefault();
      doc.addEventListener('paste', preventPaste, { capture: true, once: true });
      setTimeout(() => doc.removeEventListener('paste', preventPaste, true), 0);
      background();
    } },
    onMouseDown: (event: MouseEvent) => { if (event.button === 1) event.preventDefault(); },
  };
}

const overviewActions = { busy: false, allowAll: () => {}, review: () => {} };

function CardBody({ tab, summaries, now }: { tab: Tab; summaries: Readonly<Record<string, TabSummary>>; now: number }) {
  if (tab.split) return <span className="overview-card-panes">{tab.split.panes.map(pane => <span key={pane.id} className="overview-card-pane">{pane.title}</span>)}</span>;
  const known = summaries[tab.id];
  if (['conversation', 'task', 'newtab', 'inbox'].includes(tab.kind) && !tab.draft.trim() && !known?.digest && known?.mark !== 'waiting') return <span className="overview-card-text overview-card-empty">No work yet</span>;
  const Preview = kindDef(tab.kind).preview;
  if (Preview) return <PreviewContents><Preview pane={tab} title={tab.title} summary={summaries[tab.id]} now={now} act={overviewActions}/></PreviewContents>;
  const text = cardText(tab, summaries);
  return text ? <span className="overview-card-text">{text}</span> : <span className="overview-card-text overview-card-empty">No work yet</span>;
}

type Props = {
  modelNames?: Readonly<Record<string, string>>;
  tab: Tab; summaries: Readonly<Record<string, TabSummary>>; now: number;
  active: boolean; cursor: boolean;
  /** The tab's own menu, built by the shared tab menu builder; absent for a tab that has none (the Inbox). */
  menu?: MenuEntry[];
  /** A plain press. */
  onOpen: () => void;
  /** ⌘-click or middle-click: Interactions "Background tab". The tab is already open, so this opens nothing. */
  onBackground: () => void;
  onClose?: () => void;
  /** A card dragged onto this one lands before it (left half) or after it (right half), in this card's section. */
  onDropTab?: (id: string, after: boolean) => void;
};

/** Which half of the card a drag is over: the left half drops before the card, the right half after it. */
const halfOf = (event: DragEvent<HTMLElement>) => { const box = event.currentTarget.getBoundingClientRect(); return event.clientX - box.left > box.width / 2 ? 'after' : 'before'; };

export function OverviewCard({ tab, summaries, now, active, cursor, menu, onOpen, onBackground, onClose, onDropTab, modelNames }: Props) {
  const drop = onDropTab && {
    onDragOver: (event: DragEvent<HTMLElement>) => {
      if (!event.dataTransfer.types.includes(tabDragType)) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = 'move';
      event.currentTarget.dataset.drop = halfOf(event);
    },
    onDragLeave: (event: DragEvent<HTMLElement>) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) delete event.currentTarget.dataset.drop; },
    onDrop: (event: DragEvent<HTMLElement>) => {
      const id = event.dataTransfer.getData(tabDragType);
      const after = halfOf(event) === 'after';
      delete event.currentTarget.dataset.drop;
      if (!id) return;
      // The section under the card sees the drop too (it clears its own highlight) and leaves a handled drop alone.
      event.preventDefault();
      if (id !== tab.id) onDropTab(id, after);
    },
  };
  const mark = tabMark(tab, summaries);
  const pane = focusedPane(tab);
  const summary = summaries[pane.id];
  const updated = summary?.updatedAt;
  const model = pane.kind === 'conversation' ? summary?.model : pane.kind === 'task' && pane.route?.taskId ? summary?.taskModels?.[pane.route?.taskId] : undefined;
  const footer = [updated === undefined ? undefined : relativeTime(updated, now), model ? modelNames?.[model] || model : undefined].filter(Boolean).join(' · ');
  const card = (
    <article className="overview-card" draggable {...drop} onDragStart={event => { event.dataTransfer.setData(tabDragType, tab.id); event.dataTransfer.effectAllowed = 'move'; }} data-active={active} data-cursor={cursor} data-card-id={tab.id} data-kind={tab.split ? 'split' : tab.kind}>
      <Button className="overview-card-open" aria-label={`Open ${tab.title}`} aria-current={active || undefined} {...backgroundPress(onOpen, onBackground)}/>
      <div className="overview-card-face">
        <div className="overview-card-head">
          <Icon name={kindIcon(tab)} size="micro"/><span>{kindLabel(tab)}</span>
          {mark && <span className="overview-card-state"><StateDot mark={mark}/>{markWords[mark]}</span>}
        </div>
        <span className="overview-card-title">{tab.title}</span>
        <div className="overview-card-body"><CardBody tab={tab} summaries={summaries} now={now}/></div>
        {footer && <span className="overview-card-foot">{footer}</span>}
      </div>
      {onClose && <IconButton className="overview-card-close" label={`Close ${tab.title}`} icon="close" iconSize="micro" onClick={onClose}/>}
    </article>
  );
  return menu ? <ContextMenu label={`Actions for ${tab.title}`} items={menu}>{card}</ContextMenu> : card;
}
