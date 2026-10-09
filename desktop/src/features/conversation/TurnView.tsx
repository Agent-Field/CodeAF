import type { ReactNode } from 'react';
import { Button, Icon, IconButton, Markdown, WorkStateIndicator } from '../../components/ui';
import { ErrorItem } from './ErrorItem';
import { NoteItem } from './NoteItem';
import type { Turn, TurnItem } from './types';
import { UserMessage } from './UserMessage';
import './conversation.css';

type TurnViewProps = {
  turn: Turn;
  folded: boolean;
  onToggleFold: () => void;
  renderItem: (item: TurnItem) => ReactNode;
};

function BuiltInItem({ item }: { item: TurnItem }) {
  if (item.kind === 'text') return item.text ? <Markdown>{item.text}</Markdown> : null;
  if (item.kind === 'note') return <NoteItem text={item.text} />;
  if (item.kind === 'error') return <ErrorItem text={item.text} />;
  return null;
}

const builtIn = new Set<TurnItem['kind']>(['text', 'note', 'error']);

function Item({ item, renderItem }: { item: TurnItem; renderItem: TurnViewProps['renderItem'] }) {
  return <div className="turn-item">{builtIn.has(item.kind) ? <BuiltInItem item={item} /> : renderItem(item)}</div>;
}

function FoldedTurn({ turn, onToggleFold }: Pick<TurnViewProps, 'turn' | 'onToggleFold'>) {
  return (
    <Button className="turn-folded" aria-expanded={false} aria-label="Unfold" onClick={onToggleFold}>
      <span className="turn-folded-chevron" aria-hidden="true">
        <Icon name="chevronRight" size="sm" />
      </span>
      <span className="turn-folded-user">{turn.user}</span>
      {turn.digest && <span className="turn-folded-digest">{turn.digest}</span>}
    </Button>
  );
}

function Working() {
  return (
    <p className="turn-working">
      <WorkStateIndicator phase="working" label="Working" />
      <span>Working…</span>
    </p>
  );
}

export function TurnView({ turn, folded, onToggleFold, renderItem }: TurnViewProps) {
  if (folded) {
    return (
      <section className="turn" data-folded="true">
        <FoldedTurn turn={turn} onToggleFold={onToggleFold} />
      </section>
    );
  }
  const waiting = turn.state === 'working' && turn.items.length === 0;
  return (
    <section className="turn">
      <IconButton
        className="turn-fold"
        icon="chevron"
        iconSize="sm"
        label="Fold"
        aria-expanded={true}
        onClick={onToggleFold}
      />
      <UserMessage text={turn.user} />
      {turn.items.map((item) => (
        <Item key={item.id} item={item} renderItem={renderItem} />
      ))}
      {waiting && <Working />}
    </section>
  );
}
