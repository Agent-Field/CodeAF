// Messages waiting for their turn. Edit and reorder are offered only when the
// caller can really do them: a caller that passes remove alone gets rows with
// just that. Immediate delivery uses the same capability rule. The engine
// decides whether a change still applies (a message whose turn has started
// refuses it), so the rows never claim a change on their own.

import { useEffect, useRef, useState, type FocusEvent, type KeyboardEvent } from 'react';
import { Button, ContextMenu, DropdownMenu, Icon, IconButton, RowActions } from '../../../components/ui';
import { plainMessage } from '../composer/pastedText';
import { QueuedEdit } from './QueuedEdit';
import './queued.css';

export type QueuedItem = { id: string; text: string };

type Props = {
  items: QueuedItem[];
  onRemove: (id: string) => void;
  onEdit?: (id: string, text: string) => void;
  /** Move the item so it ends at index `to` of the queue. */
  onMove?: (id: string, to: number) => void;
  onSendNow?: (id: string) => void;
};

const VISIBLE = 2;

/**
 * Hands focus back to a row after an edit ends or a keyboard move. A move reaches
 * the list only when the engine's snapshot returns, and re-ordering the DOM drops
 * focus, so the wish is kept until the order changes and dropped once focus goes
 * to something else on purpose.
 */
function useRowFocus(order: string) {
  const list = useRef<HTMLUListElement>(null);
  const wanted = useRef<string | null>(null);
  const [asked, setAsked] = useState(0);
  useEffect(() => {
    if (wanted.current === null) return;
    const row = list.current?.querySelector<HTMLElement>(`[data-queued-id="${CSS.escape(wanted.current)}"]`);
    if (row) row.focus();
    else wanted.current = null;
  }, [order, asked]);
  return {
    list,
    focusRow: (id: string) => {
      wanted.current = id;
      setAsked((count) => count + 1);
    },
    // A blur that lands on another control is the person leaving; one that lands nowhere is the DOM moving.
    release: (event: FocusEvent<HTMLElement>) => {
      if (event.relatedTarget && !list.current?.contains(event.relatedTarget as Node)) wanted.current = null;
    },
  };
}

/** Messages waiting for their turn, drawn just above the composer. */
export function QueuedRows({ items, onRemove, onEdit, onMove, onSendNow }: Props) {
  const [coarse, setCoarse] = useState(() => window.matchMedia('(pointer: coarse)').matches);
  useEffect(() => {
    const pointer = window.matchMedia('(pointer: coarse)');
    const update = () => setCoarse(pointer.matches);
    pointer.addEventListener('change', update);
    return () => pointer.removeEventListener('change', update);
  }, []);
  const [editing, setEditing] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [dragging, setDragging] = useState<string | null>(null);
  const [announced, setAnnounced] = useState('');
  const { list, focusRow, release } = useRowFocus(items.map((item) => item.id).join(' '));
  useEffect(() => {
    if (!editing) return;
    // The menu releases its inert background after the editor mounts, so focus follows that release.
    const frame = requestAnimationFrame(() => list.current?.querySelector<HTMLInputElement>('.queued-edit input')?.focus());
    return () => cancelAnimationFrame(frame);
  }, [editing, list]);
  if (items.length === 0) return null;
  const hidden = open ? 0 : Math.max(0, items.length - VISIBLE);
  const shown = hidden ? items.slice(0, VISIBLE) : items;

  const drop = (to: number) => {
    if (dragging && onMove) onMove(dragging, to);
    setDragging(null);
  };

  const move = (id: string, to: number) => {
    if (!onMove || to < 0 || to >= items.length) return;
    if (to >= VISIBLE) setOpen(true);
    onMove(id, to);
    focusRow(id);
    setAnnounced(`Moved to position ${to + 1} of ${items.length}`);
  };
  const menu = (id: string, index: number) => [
    ...(onEdit ? [{ id: 'edit', label: 'Edit', onSelect: () => setEditing(id) }] : []),
    ...(onMove ? [
      { id: 'up', label: 'Move up', disabled: index === 0, onSelect: () => move(id, index - 1) },
      { id: 'down', label: 'Move down', disabled: index === items.length - 1, onSelect: () => move(id, index + 1) },
    ] : []),
    ...(onSendNow ? [{ id: 'send', label: 'Send now', onSelect: () => onSendNow(id) }] : []),
    { id: 'remove', label: 'Remove', onSelect: () => onRemove(id) },
  ];

  // Alt+Up and Alt+Down move the focused row one place, the keyboard twin of dragging.
  const nudge = (event: KeyboardEvent, id: string, index: number) => {
    if (!onMove || !event.altKey || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) return;
    event.preventDefault();
    const to = index + (event.key === 'ArrowUp' ? -1 : 1);
    move(id, to);
  };

  const finishEdit = (id: string) => {
    setEditing(null);
    focusRow(id);
  };

  return (
    <>
      <ul className="queued-rows" aria-label="Queued messages" ref={list} onBlur={release}>
        {shown.map((item, index) =>
          editing === item.id && onEdit ? (
            <li key={item.id} className="queued-item">
              <QueuedEdit
                text={item.text}
                onSave={(text) => {
                  if (text !== item.text) onEdit(item.id, text);
                  finishEdit(item.id);
                }}
                onCancel={() => finishEdit(item.id)}
              />
            </li>
          ) : (
            <li
              key={item.id}
              className="queued-item"
              data-dragging={dragging === item.id || undefined}
              draggable={!!onMove && !coarse}
              onDragStart={() => setDragging(item.id)}
              onDragEnd={() => setDragging(null)}
              onDragOver={(event) => dragging && event.preventDefault()}
              onDrop={() => drop(index)}
            >
              <ContextMenu items={menu(item.id, index)} label="Queued message actions" className="queued-menu">
                <div
                  className="queued-row"
                  data-actions-host=""
                  data-movable={!!onMove && !coarse || undefined}
                  data-queued-id={item.id}
                  role="group"
                  aria-label={`Queued message ${index + 1} of ${items.length}`}
                  aria-keyshortcuts={onMove ? 'Shift+F10 Alt+ArrowUp Alt+ArrowDown' : 'Shift+F10'}
                  tabIndex={0}
                  onKeyDown={(event) => nudge(event, item.id, index)}
                >
                  <span className="queued-mark">
                    <Icon name="clock" size="xs" />
                  </span>
                  {onMove && (
                    <span className="queued-grip" aria-hidden="true">
                      <Icon name="grip" size="xs" />
                    </span>
                  )}
                  {onEdit ? <Button className="queued-text queued-text-edit" onClick={() => setEditing(item.id)}>{plainMessage(item.text)}</Button> : <span className="queued-text">{plainMessage(item.text)}</span>}
                  <RowActions className="queued-actions">
                    {onEdit && <IconButton label="Edit queued message" icon="pencil" size="row" iconSize="xs" onClick={() => setEditing(item.id)} />}
                    <IconButton label="Remove queued message" icon="close" size="row" iconSize="xs" onClick={() => onRemove(item.id)} />
                    {coarse && <DropdownMenu items={menu(item.id, index)} label="Queued message actions" className="queued-menu">
                      <IconButton label="Message actions for queued row" icon="more" size="row" iconSize="xs" />
                    </DropdownMenu>}
                  </RowActions>
                </div>
              </ContextMenu>
            </li>
          ),
        )}
        {items.length > VISIBLE && (
          <li className="queued-item">
            <Button className="queued-more" aria-expanded={open} onClick={() => setOpen(!open)}>
              <Icon name="chevron" size="xs" motion="disclosure" />
              {open ? 'Show fewer' : `${hidden} more queued`}
            </Button>
          </li>
        )}
      </ul>
      <span className="queued-announce" role="status">
        {announced}
      </span>
    </>
  );
}
