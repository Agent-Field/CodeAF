// The filmstrip (design 3i): the same tabs in the same order as the grid, each drawn as its real pane at half
// scale (a 1040 x 660 pane under a 0.5 transform), read-only. The cursor's card is full size in the centre; the
// others sit at 0.86 and fade at the window edges. Only cards near the centre mount a live pane.
import { useEffect, useRef } from 'react';
import { Button, Icon } from '../../components/ui';
import type { TabSummary } from '../conversation/tabSummary';
import { kindDef } from './kinds/registry';
import type { PaneActions } from './kinds/slots';
import { panesOf, type Pane, type Tab } from './model';
import { backgroundPress, kindIcon, StateDot, tabMark } from './OverviewCard';
import { isMac } from '../../design/keyboard';
import { PaneHeader } from './PaneGrid';

/** Mounted panes: the centre card and this many on each side. Cards beyond that are masked or off screen. */
const liveReach = 3;
const idle: PaneActions = { onDraft: () => {}, onView: () => {}, onSummary: () => {}, onOpenTaskTab: () => {}, onOpenFile: () => {} };

/** The tab's panes as they are in the workspace, but inert: nothing in them takes focus, a key or a click. */
function LivePane({ tab }: { tab: Tab }) {
  const panes = panesOf(tab);
  const body = (pane: Pane) => { const Body = kindDef(pane.kind).pane; return <Body pane={pane} label={pane.title} focused={false} split={!!tab.split} actions={idle}/>; };
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
  activeId: string; cursorId: string | undefined; position: string;
  onCursor: (id: string) => void; onOpen: (id: string) => void;
};

export function OverviewFilmstrip({ tabs, summaries, activeId, cursorId, position, onCursor, onOpen }: Props) {
  const track = useRef<HTMLDivElement>(null);
  const at = tabs.findIndex(tab => tab.id === cursorId);
  useEffect(() => {
    const stage = track.current;
    const card = stage?.querySelector<HTMLElement>('[data-cursor="true"]');
    if (stage && card) stage.scrollLeft = card.offsetLeft - (stage.clientWidth - card.offsetWidth) / 2;
  }, [cursorId, tabs.length]);
  return <section className="overview-film" aria-label="Filmstrip">
    <span className="overview-film-position" aria-live="polite">{position}</span>
    <div ref={track} className="overview-film-track">
      {tabs.map((tab, index) => {
        const centre = tab.id === cursorId;
        const mark = tabMark(tab, summaries);
        return <div key={tab.id} className="overview-film-item" data-cursor={centre} data-active={tab.id === activeId} data-card-id={tab.id}>
          <div className="overview-film-frame">
            <div className="overview-film-viewport"><div className="overview-film-pane">{Math.abs(index - at) <= liveReach && <LivePane tab={tab}/>}</div></div>
            <Button className="overview-film-open" aria-label={`${centre ? 'Open' : 'Show'} ${tab.title}`} aria-current={tab.id === activeId || undefined} {...backgroundPress(() => (centre ? onOpen(tab.id) : onCursor(tab.id)), () => onCursor(tab.id))}/>
          </div>
          <span className="overview-film-label">{mark ? <StateDot mark={mark}/> : <Icon name={tab.pinned ? 'pin' : kindIcon(tab)} size="xs"/>}<span>{tab.title}</span></span>
        </div>;
      })}
    </div>
    <div className="overview-film-hint"><span>← → move</span><span>↵ open</span><span>{isMac ? '⌘W' : 'Ctrl W'} close</span><span>Esc back</span></div>
  </section>;
}
