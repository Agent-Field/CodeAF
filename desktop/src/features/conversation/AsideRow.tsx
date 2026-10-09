import { Button, Icon } from '../../components/ui';
import type { TurnItem } from './types';
import './notice.css';

type AsideItem = Extract<TurnItem, { kind: 'aside' }>;
type AsideRowProps = { item: AsideItem; open: boolean; onToggle: () => void };

const LABEL = { job: 'Background job finished', watch: 'Watch fired' } as const;

/** A job or watch notice: a fixed label, the engine's title when it gave one, the literal body on demand. */
export function AsideRow({ item, open, onToggle }: AsideRowProps) {
  const bodyId = `${item.id}-body`;
  return (
    <div className="task-notice aside-row" data-open={open || undefined}>
      <div className="task-notice-line">
        <Button className="task-notice-row" aria-expanded={open} aria-controls={bodyId} onClick={onToggle}>
          <span className="task-notice-title">{LABEL[item.aside]}</span>
          {item.title && <span className="task-notice-summary">{item.title}</span>}
          <Icon name="chevronRight" size="xs" />
        </Button>
      </div>
      {open && item.body && (
        <div id={bodyId} className="task-notice-body">
          {item.body}
        </div>
      )}
    </div>
  );
}
