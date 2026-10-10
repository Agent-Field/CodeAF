import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { Button } from '../../components/ui/Button';
import { PlaceSwatch, type TintName } from '../places/components/PlaceSwatch';
import design from '../../design/tokens.json';
import type { AttentionItem } from '../chat/world-client.ts';
import { blockingOf } from '../chat/world-client.ts';
import { nextUpChord } from './Banner';
import './queue-popover.css';

/** Hover waits this long before opening, so a pointer crossing the frame does not flash the list. */
export const QUEUE_OPEN_DELAY_MS = Number.parseFloat(design.foundation['i2-queue-open-delay']);

/** One line of the list, already in the words the row draws. */
export type QueueRow = {
  key: string;
  /** The question, for example "Allow 3 git actions?". */
  ask: string;
  /** "Place · task or conversation". Empty draws nothing under the question. */
  where: string;
  tint: TintName;
  /** The right-hand word. Absent when the question is neither blocking nor suggested. */
  tag?: 'Blocking' | 'Suggested';
};

/**
 * A feed question as a row. The place is the first one the asking chat belongs to; the
 * second half is the work it holds up, else the chat's title. Blocking outranks Suggested
 * because it is the reason to look now.
 */
export function queueRowOf(item: AttentionItem, tintOf: (placeId: string | undefined) => TintName): QueueRow {
  const place = item.placeNames?.[0] ?? '';
  const task = item.holdingUp?.[0] ?? item.title ?? '';
  const blocking = blockingOf(item);
  const blocks = blocking.turn || blocking.tasks.some(name => name !== '');
  const suggested = typeof item.suggestion?.key === 'string' && item.suggestion.key !== '';
  return {
    key: item.key,
    ask: item.text,
    where: [place, task].filter(part => part !== '').join(' · '),
    tint: tintOf(item.placeIds?.[0]),
    tag: blocks ? 'Blocking' : suggested ? 'Suggested' : undefined,
  };
}

/** "5 need you", and how many places they are spread over. Zero places draws no "· N places". */
export function queueHeading(count: number, places: number): { need: string; where: string } {
  return {
    need: `${count} need you`,
    where: places > 0 ? `in other conversations · ${places} ${places === 1 ? 'place' : 'places'}` : 'in other conversations',
  };
}

type Props = {
  /** The frame pill. The popover anchors under it and opens from its hover and focus. */
  children: ReactNode;
  rows: readonly QueueRow[];
  /** Questions Accept can answer. The button is absent at zero. */
  acceptable: number;
  /** Enter on a row: jump to that question, which becomes the current one. */
  onJump: (key: string) => void;
  onStart: () => void;
  onAccept: () => void;
  /** Hold one state for measurement. The product leaves this unset. */
  openNow?: boolean;
};

/**
 * The queue popover (Iteration 2 "Frame pill → hover list"). Opens 150ms after the pointer
 * arrives or at once on keyboard focus, closes when both leave or on Esc. The rows are real
 * buttons, so Up and Down move focus between them and Enter is the native activation.
 */
export function QueuePopover({ children, rows, acceptable, onJump, onStart, onAccept, openNow }: Props) {
  const [open, setOpen] = useState(false);
  const timer = useRef(0);
  const root = useRef<HTMLDivElement>(null);
  const panel = useId();
  const shown = (open || !!openNow) && rows.length > 0;

  const cancel = () => window.clearTimeout(timer.current);
  useEffect(() => cancel, []);

  const places = new Set(rows.map(row => row.where.split(' · ')[0]).filter(name => name !== '')).size;
  const heading = queueHeading(rows.length, places);

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape' && shown) {
      event.stopPropagation();
      cancel();
      setOpen(false);
      // Focus goes back to the pill so Esc from a row does not strand it in a closed panel.
      root.current?.querySelector<HTMLElement>('[data-queue-trigger] > :first-child, .frame-pill')?.focus();
      return;
    }
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
    if (!shown) {
      if (event.key === 'ArrowDown') { setOpen(true); event.preventDefault(); }
      return;
    }
    const stops = Array.from(root.current?.querySelectorAll<HTMLElement>('.queue-popover-row, .queue-popover-action, .frame-pill') ?? []);
    const at = stops.indexOf(document.activeElement as HTMLElement);
    const next = event.key === 'ArrowDown' ? at + 1 : at - 1;
    if (next < 0 || next >= stops.length) return;
    event.preventDefault();
    stops[next].focus();
  }

  return (
    <div
      ref={root}
      className="queue-popover-anchor"
      onPointerEnter={() => { cancel(); timer.current = window.setTimeout(() => setOpen(true), QUEUE_OPEN_DELAY_MS); }}
      onPointerLeave={() => { cancel(); setOpen(false); }}
      onFocus={() => { cancel(); setOpen(true); }}
      onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) { cancel(); setOpen(false); } }}
      onKeyDown={onKeyDown}
    >
      <div data-queue-trigger aria-controls={shown ? panel : undefined}>{children}</div>
      {shown && (
        <div id={panel} className="queue-popover" role="group" aria-label={`${heading.need} ${heading.where}`}>
          <div className="queue-popover-head">
            <span className="queue-popover-need">{heading.need}</span>
            <span className="queue-popover-where">{heading.where}</span>
          </div>
          {rows.map(row => (
            <Button key={row.key} variant="ghost" className="queue-popover-row" onClick={() => { onJump(row.key); setOpen(false); }}>
              <PlaceSwatch tint={row.tint} role="card"/>
              <span className="queue-popover-copy">
                <span className="queue-popover-ask">{row.ask}</span>
                {row.where !== '' && <span className="queue-popover-sub">{row.where}</span>}
              </span>
              {row.tag && <span className="queue-popover-tag">{row.tag}</span>}
            </Button>
          ))}
          <div className="queue-popover-rule" role="separator"/>
          <div className="queue-popover-actions">
            <Button variant="primary" className="queue-popover-action" onClick={() => { onStart(); setOpen(false); }}>
              Start<span className="queue-popover-chord">{nextUpChord}</span>
            </Button>
            {acceptable > 0 && (
              <Button variant="quiet" className="queue-popover-action" onClick={() => { onAccept(); setOpen(false); }}>
                Accept {acceptable} {acceptable === 1 ? 'suggestion' : 'suggestions'}
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

const sample: QueueRow[] = [
  { key: 'a', ask: 'Allow 3 git actions?', where: 'Codeaf · Config parser', tint: 'tide', tag: 'Blocking' },
  { key: 'b', ask: 'Use the shared lexer?', where: 'Docs · Style guide', tint: 'sage', tag: 'Suggested' },
  { key: 'c', ask: 'Overwrite notes.md?', where: 'Docs · Release notes', tint: 'sage', tag: 'Blocking' },
  { key: 'd', ask: 'Keep the old schema?', where: 'Ledger · Import', tint: 'sand', tag: 'Suggested' },
  { key: 'e', ask: 'Run the migration?', where: 'Ledger · Import', tint: 'sand', tag: 'Suggested' },
];

/** The measurement page (`?specimen=queue-popover`). `n` picks the rows, `accept` the suggestion count, `open=1` pins it open. */
export function QueuePopoverSpecimen() {
  const query = new URLSearchParams(window.location.search);
  const rows = sample.slice(0, Number(query.get('n') ?? sample.length));
  const mark = (what: string) => () => { document.body.dataset.opened = what; };
  return (
    <div className="queue-popover-specimen">
      <QueuePopover rows={rows} acceptable={Number(query.get('accept') ?? '3')} openNow={query.get('open') === '1'}
        onJump={key => { document.body.dataset.opened = `jump:${key}`; }} onStart={mark('start')} onAccept={mark('accept')}>
        <Button variant="ghost" className="frame-pill"><span className="frame-pill-glyph" aria-hidden="true"/>{rows.length} need you</Button>
      </QueuePopover>
    </div>
  );
}
