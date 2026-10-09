import type { ReactNode } from 'react';
import { Button, CopyButton, Icon, IconButton, Markdown, WorkStateIndicator } from '../../components/ui';
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
  onRetry?: () => void;
  /** Task-authored descriptions may be Markdown; a person's words never are. */
  userMarkdown?: boolean;
};

type ItemProps = { item: TurnItem; renderItem: TurnViewProps['renderItem']; onRetry?: () => void };

function BuiltInItem({ item, onRetry }: Omit<ItemProps, 'renderItem'>) {
  if (item.kind === 'text') return item.text ? <Markdown>{item.text}</Markdown> : null;
  if (item.kind === 'note') return <NoteItem text={item.text} long={item.long} />;
  if (item.kind === 'error') return <ErrorItem text={item.text} onRetry={onRetry} />;
  if (item.kind === 'steer') return <UserMessage text={item.text} />;
  return null;
}

const builtIn = new Set<TurnItem['kind']>(['text', 'note', 'error', 'steer']);

function Item({ item, renderItem, onRetry }: ItemProps) {
  const content = builtIn.has(item.kind) ? <BuiltInItem item={item} onRetry={onRetry} /> : renderItem(item);
  return <div className="turn-item">{content}</div>;
}

function FoldedTurn({ turn, onToggleFold }: Pick<TurnViewProps, 'turn' | 'onToggleFold'>) {
  return (
    <Button className="turn-folded" aria-expanded={false} aria-label="Unfold" onClick={onToggleFold}>
      <span className="turn-folded-chevron" aria-hidden="true">
        <Icon name="chevronRight" size="sm" />
      </span>
      <span className="turn-folded-text">
        <span className="turn-folded-user">{turn.user}</span>
        {turn.digest && (
          <>
            <span className="turn-folded-sep" aria-hidden="true">
              {' — '}
            </span>
            <span className="turn-folded-digest">{turn.digest}</span>
          </>
        )}
      </span>
    </Button>
  );
}

function Stopped() {
  return (
    <div className="turn-stopped" role="status">
      <Icon name="cancelled" size="sm" />
      <span>Stopped</span>
    </div>
  );
}

function Working() {
  return (
    <div className="turn-working">
      <WorkStateIndicator phase="working" label="Working" />
      <span>Working…</span>
    </div>
  );
}

/** The reply's own words, for Copy; empty while it is still being written. */
function replyText(turn: Turn): string {
  if (turn.state === 'working' || turn.state === 'streaming') return '';
  const texts = turn.items.flatMap((item) => (item.kind === 'text' && item.text.trim() ? [item.text] : []));
  return texts.join('\n\n');
}

export function TurnView({ turn, folded, onToggleFold, renderItem, onRetry, userMarkdown }: TurnViewProps) {
  if (folded) {
    return (
      <section className="turn" data-folded="true">
        <FoldedTurn turn={turn} onToggleFold={onToggleFold} />
      </section>
    );
  }
  const waiting = turn.state === 'working' && turn.items.length === 0;
  const reply = replyText(turn);
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
      <UserMessage text={turn.user} markdown={userMarkdown} />
      {turn.items.map((item) => (
        <Item key={item.id} item={item} renderItem={renderItem} onRetry={onRetry} />
      ))}
      {waiting && <Working />}
      {turn.state === 'stopped' && <Stopped />}
      {reply && (
        <div className="turn-actions">
          <CopyButton text={reply} label="Copy reply" />
        </div>
      )}
    </section>
  );
}
