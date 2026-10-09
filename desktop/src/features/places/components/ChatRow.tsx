import type { HTMLAttributes, KeyboardEvent, MouseEvent, ReactNode } from 'react';
import { Button, ContextMenu, Icon, StatusMark, type MenuEntry } from '../../../components/ui';
import './places-components.css';

/** A chat that is not simply settled shows its state in place of the message glyph (8a, Components "History row · live"). */
export type ChatStatus = 'running' | 'waiting' | 'failed';
const statusWords: Record<ChatStatus, string> = { running: 'Running', waiting: 'Needs you', failed: 'Failed' };

type ChatRowProps = Omit<HTMLAttributes<HTMLLIElement>, 'title' | 'onClick' | 'onKeyDown'> & {
  /** The canonical session id. It rides on the row (data-chat-id) and every callback closes over it; the row never makes one up. */
  id: string;
  title: string;
  /** The one sentence under the title: a recap, "Open · 4 tasks running". Omitted when the engine has none; never padded. */
  excerpt?: string;
  status?: ChatStatus;
  /** Words for a screen reader and the mark ("Running 12s"). Defaults to the plain state word. */
  statusLabel?: string;
  /** The model the chat runs on. The design draws none on a row, so it is carried as data-model for callers and tests, not shown. */
  model?: string;
  /** The caller's own short time ("Today", "Mon", "11:40"), and the machine-readable instant behind it. */
  timeLabel?: string;
  timeIso?: string;
  /** home: the 44px line of a place's Chats (8a). history: the taller History row with room for an excerpt and an artifact line. */
  variant?: 'home' | 'history';
  /** History only, non-interactive: the artifact line (file chips, decisions) under the excerpt. */
  details?: ReactNode;
  selected?: boolean;
  /** This row is being dragged (to file the chat into a place). */
  dragging?: boolean;
  disabled?: boolean;
  /** Plain click, Enter or Space. */
  onOpen: () => void;
  /** Command or Control click, or middle click: open in a new tab. */
  onOpenInNewTab?: () => void;
  /** Keyboard arrows within a list are the list's; every other key passes through. */
  onKeyDown?: (event: KeyboardEvent<HTMLButtonElement>) => void;
  menu?: readonly MenuEntry[];
  menuLabel?: string;
  /** Forces a look, for specimens only. */
  appearance?: 'hover' | 'pressed' | 'focus';
};

/** The one chat row, shared by a place's Home and History. Everything shown comes from props: no state is made up. */
export function ChatRow({ id, title, excerpt, status, statusLabel, model, timeLabel, timeIso, variant = 'home', details, selected, dragging, disabled, onOpen, onOpenInNewTab, onKeyDown, menu, menuLabel, appearance, className = '', ...frame }: ChatRowProps) {
  const open = (event: MouseEvent<HTMLButtonElement>) => {
    if ((event.metaKey || event.ctrlKey) && onOpenInNewTab) onOpenInNewTab(); else onOpen();
  };
  const row = <li {...frame} className={`places-chat-row ${className}`} data-chat-id={id} data-model={model} data-variant={variant} data-status={status}
    data-selected={selected || undefined} data-dragging={dragging || undefined} data-disabled={disabled || undefined} data-force={appearance}>
    <Button variant="ghost" className="places-row-main places-chat-main" disabled={disabled} aria-current={selected ? 'true' : undefined} data-force={appearance}
      onClick={open} onKeyDown={onKeyDown} onAuxClick={event => { if (event.button === 1 && onOpenInNewTab) { event.preventDefault(); onOpenInNewTab(); } }}>
      <span className="places-chat-lead">
        {status ? <StatusMark status={status} label={statusLabel ?? statusWords[status]} dense/> : <Icon name="tab" size="xs"/>}
      </span>
      <span className="places-chat-text">
        <span className="places-chat-title">{title}</span>
        {excerpt && <span className="places-chat-excerpt">{excerpt}</span>}
        {details && <span className="places-chat-details">{details}</span>}
      </span>
      {timeLabel && (timeIso ? <time className="places-chat-time" dateTime={timeIso}>{timeLabel}</time> : <span className="places-chat-time">{timeLabel}</span>)}
    </Button>
  </li>;
  return menu && menu.length > 0 ? <ContextMenu items={menu} label={menuLabel ?? `${title} actions`}>{row}</ContextMenu> : row;
}

/** A column of chat rows. On a Home they bleed 12px past the page column, the same as the attention rows. */
export function ChatList({ label, variant = 'home', className = '', ...props }: HTMLAttributes<HTMLUListElement> & { label: string; variant?: 'home' | 'history' }) {
  return <ul {...props} aria-label={label} className={`places-chat-list ${className}`} data-variant={variant}/>;
}
