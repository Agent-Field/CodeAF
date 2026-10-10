import type { ReactNode } from 'react';
import { Button, IconButton, StatusMark } from '../../components/ui';
import { NextUpChip, type NextUpProgress } from '../nextup/NextUpChip';
import './conversation-bar.css';
import { nextUpWalk, useNextUpWalkState } from '../nextup/useNextUpWalk';

export type BarCounts = { running: number; needsYou: number };
export type BarPanel = { label: string; shown: boolean; onToggle: () => void };

type CountProps = { status: 'running' | 'waiting'; count: number; words: string; onClick?: () => void };

/** A 6px glyph and its count; the glyph is colour only, the words carry the meaning. */
function Count({ status, count, words, onClick }: CountProps) {
  if (count <= 0) return null;
  return (
    <Button className="conversation-bar-count" onClick={onClick}>
      <span aria-hidden="true"><StatusMark dense status={status} label={words} /></span>
      {count} {words}
    </Button>
  );
}

type BarProps = { children: ReactNode; counts: BarCounts; panel?: BarPanel; lead: 'title' | 'trail'; /** The Using chip: sits beside the title, and only on a conversation (the trail of a task page has no chip). */ using?: ReactNode; nextUp?: NextUpProgress; onNeedsYou?: () => void; onTasksFilter?: (filter: 'running' | 'needs') => void };

/** The pane's top row (design 1a to 1c): the title or the trail on the left, what is happening on the right, then the panel toggle. */
export function ConversationBar({ children, counts, panel, lead, using, nextUp, onNeedsYou, onTasksFilter }: BarProps) {
  const walk = useNextUpWalkState();
  return (
    <div className="conversation-bar" data-lead={lead}>
      <div className="conversation-bar-lead">{children}{lead === 'title' && <NextUpChip progress={nextUp} />}{lead === 'title' && using}</div>
      <span className="conversation-bar-counts">
        {nextUp && walk.origin && <Button className="nextup-walk-return" onClick={() => nextUpWalk.exit()}>Back to {walk.origin.label}<span>Esc</span></Button>}
        <Count status="running" count={counts.running} words="running" onClick={() => onTasksFilter?.('running')} />
        <Count status="waiting" count={counts.needsYou} words={counts.needsYou === 1 ? 'needs you here' : 'need you here'} onClick={onNeedsYou} />
      </span>
      {panel && (
        <IconButton className="conversation-bar-toggle" label={panel.label} icon="panelRight" iconSize="sm" aria-pressed={panel.shown} onClick={panel.onToggle} />
      )}
    </div>
  );
}
