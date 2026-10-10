import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { BreathingDot, Button, IconButton, StatusMark, TextInput } from '../../components/ui';
import { NextUpChip, type NextUpProgress } from '../nextup/NextUpChip';
import './conversation-bar.css';

export type BarCounts = { running: number; needsYou: number };
export type BarPanel = { label: string; shown: boolean; onToggle: () => void };

const TITLE_MAX = 80;

type CountProps = { status: 'running' | 'waiting'; count: number; words: string; onClick?: () => void };

/** A 6px glyph and its count; the running glyph breathes, and the count is a quiet button only when something can answer the click. */
function Count({ status, count, words, onClick }: CountProps) {
  if (count <= 0) return null;
  const mark = status === 'running' ? <BreathingDot /> : <span aria-hidden="true"><StatusMark dense status={status} label={words} /></span>;
  if (!onClick) return <span className="conversation-bar-count">{mark}{count} {words}</span>;
  return <Button className="conversation-bar-count" onClick={onClick}>{mark}{count} {words}</Button>;
}

/** The title; with `onRename` it opens an inline field on double-click, Enter or F2. Enter commits, Esc cancels, and a blank or unchanged name changes nothing. */
function BarTitle({ title, onRename }: { title: string; onRename?: (name: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(title);
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => { if (editing) field.current?.select(); }, [editing]);
  if (!onRename) return <span className="conversation-bar-title">{title}</span>;
  const begin = () => { setDraft(title); setEditing(true); };
  const finish = (commit: boolean) => {
    setEditing(false);
    const name = draft.trim();
    if (commit && name && name !== title) onRename(name);
  };
  if (editing) {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Enter') { e.preventDefault(); finish(true); }
      else if (e.key === 'Escape') { e.preventDefault(); finish(false); }
    };
    return <TextInput ref={field} className="conversation-bar-rename" aria-label="Rename conversation" value={draft} maxLength={TITLE_MAX} onChange={(e) => setDraft(e.target.value)} onKeyDown={onKey} onBlur={() => finish(true)} />;
  }
  return (
    <span className="conversation-bar-title" role="button" tabIndex={0} title={title} onDoubleClick={begin}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === 'F2') { e.preventDefault(); begin(); } }}>{title}</span>
  );
}

type BarProps = { children?: ReactNode; /** The conversation's name; when given with `lead="title"` the bar draws it, and `onRename` makes it editable. */ title?: string; onRename?: (name: string) => void; counts: BarCounts; panel?: BarPanel; lead: 'title' | 'trail'; /** The Using chip: sits beside the title, and only on a conversation (the trail of a task page has no chip). */ using?: ReactNode; nextUp?: NextUpProgress; onNeedsYou?: () => void; onOpenFiltered?: (filter: 'running' | 'needs') => void; onTasksFilter?: (filter: 'running' | 'needs') => void };

/** The pane's top row (design 1a to 1c): the title or the trail on the left, what is happening on the right, then the panel toggle. */
export function ConversationBar({ children, title, onRename, counts, panel, lead, using, nextUp, onNeedsYou, onOpenFiltered, onTasksFilter }: BarProps) {
  const openFiltered = onOpenFiltered ?? onTasksFilter;
  return (
    <div className="conversation-bar" data-lead={lead}>
      <div className="conversation-bar-lead">{lead === 'title' && title !== undefined ? <BarTitle title={title} onRename={onRename} /> : children}{lead === 'title' && <NextUpChip progress={nextUp} />}{lead === 'title' && using}</div>
      <span className="conversation-bar-counts">
        <Count status="running" count={counts.running} words="running" onClick={openFiltered && (() => openFiltered('running'))} />
        <Count status="waiting" count={counts.needsYou} words={counts.needsYou === 1 ? 'needs you here' : 'need you here'} onClick={onNeedsYou ?? (openFiltered && (() => openFiltered('needs')))} />
      </span>
      {panel && (
        <IconButton className="conversation-bar-toggle" label={panel.label} icon="panelRight" iconSize="sm" aria-pressed={panel.shown} onClick={panel.onToggle} />
      )}
    </div>
  );
}
