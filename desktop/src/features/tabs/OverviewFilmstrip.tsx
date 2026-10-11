// The filmstrip (design 3i): the same tabs in the same order as the grid, each drawn in its pane layout at half
// scale (a 1040 x 660 pane under a 0.5 transform), read-only. The cursor's card is full size in the centre; the
// others sit at 0.86 and fade at the window edges. Only visible cards mount their cached content.
import { useEffect, useRef } from 'react';
import { Button, Icon } from '../../components/ui';
import type { TabSummary } from '../conversation/tabSummary';
import { panesOf, type Tab } from './model';
import { FilmstripPane } from './preview/FilmstripPane';
import { usePreviewVisible } from './preview/usePreviewVisible';
import { backgroundPress, kindIcon, StateDot, tabMark } from './OverviewCard';
import { isMac } from '../../design/keyboard';
import { PaneHeader } from './PaneGrid';

/** The tab's panes as they are in the workspace, but inert: nothing in them takes focus, a key or a click. */
function LivePane({ tab, summaries, now }: { tab: Tab; summaries: Props['summaries']; now: number }) {
  const panes = panesOf(tab);
  const body = (pane: (typeof panes)[number]) => <FilmstripPane pane={pane} summary={summaries[pane.id]} now={now}/>;
  return <div className="overview-live" inert aria-hidden="true">
    {tab.split
      ? <div className="workspace-conversation" data-split data-layout={tab.split.layout} data-count={panes.length}>
        {panes.map((pane, index) => <section key={pane.id} className="workspace-pane" data-focused={index === tab.split!.focus}><PaneHeader pane={pane} focused={index === tab.split!.focus}/><div className="workspace-pane-body">{body(pane)}</div></section>)}
      </div>
      : <div className="workspace-pane-body">{body(panes[0])}</div>}
  </div>;
}

type Props = {
  tabs: readonly Tab[]; summaries: Readonly<Record<string, TabSummary>>;
  now: number; activeId: string; cursorId: string | undefined; position: string;
  onCursor: (id: string) => void; onOpen: (id: string) => void;
};

export function OverviewFilmstrip({ tabs, summaries, now, activeId, cursorId, position, onCursor, onOpen }: Props) {
  const track = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const stage = track.current;
    const card = stage?.querySelector<HTMLElement>('[data-cursor="true"]');
    if (stage && card) stage.scrollLeft = card.offsetLeft - (stage.clientWidth - card.offsetWidth) / 2;
  }, [cursorId, tabs.length]);
  return <section className="overview-film" aria-label="Filmstrip">
    <span className="overview-film-position" aria-live="polite">{position}</span>
    <div ref={track} className="overview-film-track">
      {tabs.map(tab => <FilmItem key={tab.id} tab={tab} summaries={summaries} now={now} centre={tab.id === cursorId} active={tab.id === activeId} onCursor={onCursor} onOpen={onOpen}/>)}
    </div>
    <div className="overview-film-hint"><span>← → move</span><span>↵ open</span><span>{isMac ? '⌘W' : 'Ctrl W'} close</span><span>Esc back</span></div>
  </section>;
}

/** Offscreen cards keep their frame and label; their content waits until it is visible. */
function FilmItem({ tab, summaries, now, centre, active, onCursor, onOpen }: Pick<Props, 'summaries' | 'now' | 'onCursor' | 'onOpen'> & { tab: Tab; centre: boolean; active: boolean }) {
  const host = useRef<HTMLDivElement>(null);
  const shown = usePreviewVisible(host, centre);
  const mark = tabMark(tab, summaries);
  return <div ref={host} className="overview-film-item" data-cursor={centre} data-active={active} data-card-id={tab.id}>
    <div className="overview-film-frame">
      <div className="overview-film-viewport">{shown && <div className="overview-film-pane"><LivePane tab={tab} summaries={summaries} now={now}/></div>}</div>
      {shown && <Button className="overview-film-open" aria-label={`${centre ? 'Open' : 'Show'} ${tab.title}`} aria-current={active || undefined} {...backgroundPress(() => (centre ? onOpen(tab.id) : onCursor(tab.id)), () => onCursor(tab.id))}/>}
    </div>
    <span className="overview-film-label">{shown && (mark ? <StateDot mark={mark}/> : <Icon name={tab.pinned ? 'pin' : kindIcon(tab)} size="xs"/>)}<span>{tab.title}</span></span>
  </div>;
}
