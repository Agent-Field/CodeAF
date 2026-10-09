import { Button, Icon, WorkStateIndicator } from '../../components/ui';
import type { TurnItem } from './types';
import './tools.css';

type Props = { item: Extract<TurnItem, { kind: 'thinking' }>; open: boolean; onToggle: () => void };

export function ThinkingItem({ item, open, onToggle }: Props) {
  if (!item.text && !item.streaming) return null;
  return (
    <div className="tool-group">
      <Button className="tool-toggle" aria-expanded={open} onClick={onToggle}>
        <Icon name="chevron" size="xs" motion="disclosure" />
        {item.streaming && <WorkStateIndicator phase="streaming" label="Thinking" />}
        <span className="tool-summary">{item.streaming ? 'Thinking…' : 'Thought for a moment'}</span>
      </Button>
      {open && item.text && <p className="thinking-text">{item.text}</p>}
    </div>
  );
}
