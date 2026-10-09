import { Icon, IconButton } from '../../../components/ui';
import './queued.css';

export type QueuedItem = { id: string; text: string };

/** Messages waiting for their turn, drawn just above the composer. */
export function QueuedRows({ items, onRemove }: { items: QueuedItem[]; onRemove: (id: string) => void }) {
  if (items.length === 0) return null;
  return (
    <ul className="queued-rows" aria-label="Queued messages">
      {items.map((item) => (
        <li key={item.id} className="queued-row">
          <span className="queued-mark">
            <Icon name="queued" size="sm" />
          </span>
          <span className="queued-text">{item.text}</span>
          <IconButton label="Remove queued message" icon="close" iconSize="sm" onClick={() => onRemove(item.id)} />
        </li>
      ))}
    </ul>
  );
}
