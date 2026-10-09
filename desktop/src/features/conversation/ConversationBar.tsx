import type { ReactNode } from 'react';
import { IconButton, StatusMark } from '../../components/ui';
import './conversation-bar.css';

export type BarCounts = { running: number; needsYou: number };
export type BarPanel = { label: string; shown: boolean; onToggle: () => void };

type CountProps = { status: 'running' | 'waiting'; count: number; words: string };

/** A 6px glyph and its count; the glyph is colour only, the words carry the meaning. */
function Count({ status, count, words }: CountProps) {
  if (count <= 0) return null;
  return (
    <span className="conversation-bar-count">
      <span aria-hidden="true"><StatusMark dense status={status} label={words} /></span>
      {count} {words}
    </span>
  );
}

type BarProps = { children: ReactNode; counts: BarCounts; panel?: BarPanel; lead: 'title' | 'trail' };

/** The pane's top row (design 1a to 1c): the title or the trail on the left, what is happening on the right, then the panel toggle. */
export function ConversationBar({ children, counts, panel, lead }: BarProps) {
  return (
    <div className="conversation-bar" data-lead={lead}>
      <div className="conversation-bar-lead">{children}</div>
      <span className="conversation-bar-counts">
        <Count status="running" count={counts.running} words="running" />
        <Count status="waiting" count={counts.needsYou} words="need you" />
      </span>
      {panel && (
        <IconButton className="conversation-bar-toggle" label={panel.label} icon="panelRight" iconSize="sm" aria-pressed={panel.shown} onClick={panel.onToggle} />
      )}
    </div>
  );
}
