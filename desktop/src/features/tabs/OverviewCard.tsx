// One overview card (design 3h): kind and state line, title, key content, footer. Readable text, never a
// miniature screenshot. The body is the kind's `preview` renderer when the kind has one; otherwise the draft or
// the latest answer, which is what the engine actually knows.
import { Button, ContextMenu, Icon, IconButton, type MenuEntry } from '../../components/ui';
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
export const kindIcon = (tab: Tab) => (tab.split ? 'split' as const : kindDef(tab.kind).icon);

/** The 6px state dot, shared by the card head and the filmstrip label. Running is accent, needs you amber, failed red. */
export function StateDot({ mark }: { mark: TabMark }) {
  return <span className="overview-dot" data-state={mark} aria-hidden="true"/>;
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
  tab: Tab; summaries: Readonly<Record<string, TabSummary>>; now: number;
  active: boolean; cursor: boolean; menu: MenuEntry[];
  onOpen: () => void; onClose?: () => void;
};

export function OverviewCard({ tab, summaries, now, active, cursor, menu, onOpen, onClose }: Props) {
  const mark = tabMark(tab, summaries);
  const updated = summaries[focusedPane(tab).id]?.updatedAt;
  return <ContextMenu label={`Actions for ${tab.title}`} items={menu}>
    <article className="overview-card" draggable onDragStart={event => { event.dataTransfer.setData(tabDragType, tab.id); event.dataTransfer.effectAllowed = 'move'; }} data-active={active} data-cursor={cursor} data-card-id={tab.id} data-kind={tab.split ? 'split' : tab.kind}>
      <Button className="overview-card-open" aria-label={`Open ${tab.title}`} aria-current={active || undefined} onClick={onOpen}/>
      <div className="overview-card-face">
        <div className="overview-card-head">
          <Icon name={kindIcon(tab)} size="micro"/><span>{kindLabel(tab)}</span>
          {mark && <span className="overview-card-state"><StateDot mark={mark}/>{markWords[mark]}</span>}
        </div>
        <span className="overview-card-title">{tab.title}</span>
        <div className="overview-card-body"><CardBody tab={tab} summaries={summaries} now={now}/></div>
        {updated !== undefined && <span className="overview-card-foot">{relativeTime(updated, now)}</span>}
      </div>
      {onClose && <IconButton className="overview-card-close" label={`Close ${tab.title}`} icon="close" iconSize="micro" onClick={onClose}/>}
    </article>
  </ContextMenu>;
}
