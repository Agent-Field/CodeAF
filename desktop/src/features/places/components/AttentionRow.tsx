import type { HTMLAttributes, MouseEvent } from 'react';
import { Button, ContextMenu, StatusMark, type MenuEntry } from '../../../components/ui';
import './places-components.css';

/** Only three things ask for a row on a place's Home: your call (amber), work in flight (accent), a failure (red). */
export type AttentionStatus = 'waiting' | 'running' | 'failed';
const defaultWords: Record<AttentionStatus, string> = { waiting: 'needs you', running: 'running', failed: 'failed' };
const markLabel: Record<AttentionStatus, string> = { waiting: 'Needs you', running: 'Running', failed: 'Failed' };

type AttentionRowProps = Omit<HTMLAttributes<HTMLLIElement>, 'title' | 'onClick' | 'onKeyDown'> & {
  /** The canonical chat or task id this row answers for. Never invented: the caller owns the feed. */
  id: string;
  title: string;
  /** The place the item lives in when it is not the place being viewed: "title · in Config parser". */
  placeName?: string;
  status: AttentionStatus;
  /** The right-hand words ("needs you", "running · 2m"). Defaults to the plain status word. */
  statusText?: string;
  /** Plain click, Enter or Space. */
  onOpen: () => void;
  /** Command or Control click, or middle click. */
  onOpenInNewTab?: () => void;
  menu?: readonly MenuEntry[];
  menuLabel?: string;
  disabled?: boolean;
  /** Forces a look, for specimens only. */
  appearance?: 'hover' | 'pressed' | 'focus';
};

/** One line of "needs you" or "running" on a Home (design 8a). The text stays neutral: colour lives on the 6px glyph alone. */
export function AttentionRow({ id, title, placeName, status, statusText, onOpen, onOpenInNewTab, menu, menuLabel, disabled, appearance, className = '', ...frame }: AttentionRowProps) {
  const open = (event: MouseEvent<HTMLButtonElement>) => {
    if ((event.metaKey || event.ctrlKey) && onOpenInNewTab) onOpenInNewTab(); else onOpen();
  };
  const row = <li {...frame} className={`places-attention-row ${className}`} data-attention-id={id} data-status={status} data-disabled={disabled || undefined} data-force={appearance}>
    <Button variant="ghost" className="places-row-main places-attention-main" disabled={disabled} data-force={appearance}
      onClick={open} onAuxClick={event => { if (event.button === 1 && onOpenInNewTab) { event.preventDefault(); onOpenInNewTab(); } }}>
      <StatusMark status={status} label={markLabel[status]} dense/>
      <span className="places-row-text">{title}{placeName && <> · <span className="places-row-muted">in {placeName}</span></>}</span>
      <span className="places-row-aside">{statusText ?? defaultWords[status]}</span>
    </Button>
  </li>;
  return menu && menu.length > 0 ? <ContextMenu items={menu} label={menuLabel ?? `${title} actions`}>{row}</ContextMenu> : row;
}

/** The Home's attention list: rows bleed 12px past the column so their text lines up with the page's, 2px apart. */
export function AttentionList({ label, className = '', ...props }: HTMLAttributes<HTMLUListElement> & { label: string }) {
  return <ul {...props} aria-label={label} className={`places-attention-list ${className}`}/>;
}
