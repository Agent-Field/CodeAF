import { Fragment, useEffect, useId, useState, type KeyboardEvent, type ReactNode } from 'react';
import { Button, Icon, SectionLabel } from '../../components/ui';
import { decidedCount, decidedLabel, decidedShowsAll, decidedText, visibleDecided, type DecidedItem } from './decidedModel';
import './decided.css';

export type { DecidedItem, DecidedWhy } from './decidedModel';
export { DECIDED_CAP, DECIDED_PREVIEW, decidedCount, decidedLabel, decidedShowsAll, orderedDecided, visibleDecided } from './decidedModel';

type DecidedRowsProps = {
  items: readonly DecidedItem[];
  /** The engine's full count, when `items` is only a page of it. The label uses it. */
  total?: number;
  /** Replaces the Why? body (By, Because, Sure). The card around it stays. */
  renderWhy?: (item: DecidedItem) => ReactNode;
  /** Fires when a row opens Why?, not when it closes. */
  onOpenWhy?: (item: DecidedItem) => void;
  /** Forces one look on every row, for a specimen. */
  appearance?: 'hover' | 'pressed' | 'focus';
};

/** By, Because and Sure, and nothing for a field the decision did not record. */
function DecidedWhyBody({ item }: { item: DecidedItem }) {
  const rows: [string, string][] = [];
  const by = decidedText(item.why?.by);
  const because = decidedText(item.why?.because);
  const sure = decidedText(item.why?.sure);
  if (by) rows.push(['By', by]);
  if (because) rows.push(['Because', because]);
  if (sure) rows.push(['Sure', sure]);
  return <>
    <span className="decided-why-title">Decided automatically</span>
    {rows.length > 0 && <div className="decided-why-grid">
      {rows.map(([key, value]) => <Fragment key={key}>
        <span className="decided-why-key">{key}</span>
        <span>{value}</span>
      </Fragment>)}
    </div>}
  </>;
}

/**
 * "Decided automatically · N" on a place Home. Three rows, then All for the rest.
 * A row opens that decision's Why? card. No titled row: the section is absent.
 */
export function DecidedRows({ items, total, renderWhy, onOpenWhy, appearance }: DecidedRowsProps) {
  const [expanded, setExpanded] = useState(false);
  const [openId, setOpenId] = useState<string>();
  const uid = useId();
  // The card closes from a press outside the row, or from Escape, and focus returns to the row that opened it.
  useEffect(() => {
    if (!openId) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      const id = openId;
      setOpenId(undefined);
      document.getElementById(`${uid}-row-${id}`)?.focus();
    };
    const onPointer = (event: PointerEvent) => {
      const row = document.getElementById(`${uid}-item-${openId}`);
      if (row && event.target instanceof Node && row.contains(event.target)) return;
      setOpenId(undefined);
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onPointer);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onPointer);
    };
  }, [openId, uid]);
  const count = decidedCount(items, total);
  const more = decidedShowsAll(items);
  const visible = visibleDecided(items, expanded && more);
  if (visible.length === 0) return null;

  const move = (event: KeyboardEvent<HTMLButtonElement>) => {
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    const rows = [...event.currentTarget.closest('ul')!.querySelectorAll<HTMLButtonElement>('.decided-main')];
    const next = rows[rows.indexOf(event.currentTarget) + step];
    if (!next) return;
    event.preventDefault();
    next.focus();
  };

  const toggle = (item: DecidedItem) => {
    const willOpen = openId !== item.id;
    setOpenId(willOpen ? item.id : undefined);
    if (willOpen) onOpenWhy?.(item);
  };

  return <section className="decided" aria-label="Decided automatically">
    <div className="decided-head">
      <SectionLabel>{decidedLabel(count)}</SectionLabel>
      {more && <Button variant="ghost" className="decided-all" aria-expanded={expanded} onClick={() => setExpanded(value => !value)}>{expanded ? 'Show fewer' : 'All'}</Button>}
    </div>
    <ul className="decided-list" aria-label="Decided automatically">
      {visible.map(item => {
        const open = openId === item.id;
        const detail = decidedText(item.detail);
        const age = decidedText(item.age);
        const whyId = `${uid}-why-${item.id}`;
        return <li key={item.id} id={`${uid}-item-${item.id}`} className="decided-row" data-decided-id={item.id}>
          <Button id={`${uid}-row-${item.id}`} variant="ghost" className="decided-main" data-force={appearance} aria-expanded={open} aria-controls={open ? whyId : undefined} aria-haspopup="dialog"
            onClick={() => toggle(item)} onKeyDown={move}>
            <span className="decided-check"><Icon name="check" size="tiny"/></span>
            <span className="decided-text">
              <span className="decided-title">{item.title}</span>
              {detail && <span className="decided-sub">{detail}</span>}
            </span>
            {age && (item.at ? <time className="decided-age" dateTime={item.at}>{age}</time> : <span className="decided-age">{age}</span>)}
          </Button>
          {open && <div id={whyId} className="decided-why" role="dialog" aria-label="Why?">
            {renderWhy ? renderWhy(item) : <DecidedWhyBody item={item}/>}
          </div>}
        </li>;
      })}
    </ul>
  </section>;
}
