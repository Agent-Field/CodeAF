// Messages waiting for their turn. Edit and reorder are offered only when the
// caller can really do them. The engine today can do neither, so the dock
// passes remove alone and the rows then show only that.

import { useState } from 'react';
import { Button, Icon, IconButton } from '../../../components/ui';
import { plainMessage } from '../composer/pastedText';
import { QueuedEdit } from './QueuedEdit';
import './queued.css';

export type QueuedItem = { id: string; text: string };

type Props = {
  items: QueuedItem[];
  onRemove: (id: string) => void;
  onEdit?: (id: string, text: string) => void;
  /** Move the item to the position `to`, an index into `items`. */
  onMove?: (id: string, to: number) => void;
};

const VISIBLE = 2;

/** Messages waiting for their turn, drawn just above the composer. */
export function QueuedRows({ items, onRemove, onEdit, onMove }: Props) {
  const [editing, setEditing] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [dragging, setDragging] = useState<string | null>(null);
  if (items.length === 0) return null;
  const hidden = open ? 0 : Math.max(0, items.length - VISIBLE);
  const shown = hidden ? items.slice(0, VISIBLE) : items;

  const drop = (to: number) => {
    if (dragging && onMove) onMove(dragging, to);
    setDragging(null);
  };

  return (
    <ul className="queued-rows" aria-label="Queued messages">
      {shown.map((item, index) =>
        editing === item.id && onEdit ? (
          <li key={item.id} className="queued-item">
            <QueuedEdit
              text={item.text}
              onSave={(text) => {
                onEdit(item.id, text);
                setEditing(null);
              }}
              onCancel={() => setEditing(null)}
            />
          </li>
        ) : (
          <li
            key={item.id}
            className="queued-item"
            data-dragging={dragging === item.id || undefined}
            draggable={!!onMove}
            onDragStart={() => setDragging(item.id)}
            onDragEnd={() => setDragging(null)}
            onDragOver={(event) => dragging && event.preventDefault()}
            onDrop={() => drop(index)}
          >
            <div className="queued-row" data-movable={!!onMove || undefined}>
              <span className="queued-mark">
                <Icon name="clock" size="xs" />
              </span>
              {onMove && (
                <span className="queued-grip">
                  <Icon name="grip" size="xs" />
                </span>
              )}
              <span className="queued-text">{plainMessage(item.text)}</span>
              <span className="queued-actions">
                {onEdit && <IconButton label="Edit queued message" icon="pencil" iconSize="xs" className="queued-action" onClick={() => setEditing(item.id)} />}
                <IconButton label="Remove queued message" icon="close" iconSize="xs" className="queued-action" onClick={() => onRemove(item.id)} />
              </span>
            </div>
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
  );
}
