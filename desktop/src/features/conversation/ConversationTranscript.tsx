import type { ReactNode } from 'react';
import type { EngineAnswer } from '../chat/engine-client';
import { ErrorItem } from './ErrorItem';
import { NoteItem } from './NoteItem';
import { QuestionCard } from './QuestionCard';
import { TurnView } from './TurnView';
import type { ConversationModel, TurnItem } from './types';
import type { FailedSend } from './useConversation';

type Props = {
  model: ConversationModel;
  folded: Record<string, boolean>;
  onToggleFold: (turnId: string) => void;
  renderItem: (item: TurnItem) => ReactNode;
  failed?: FailedSend;
  onRetry: () => void;
  answering: boolean;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
};

function Preface({ items, renderItem }: { items: TurnItem[]; renderItem: Props['renderItem'] }) {
  if (!items.length) return null;
  return (
    <div className="conversation-preface">
      {items.map((item) => (
        <div key={item.id}>{item.kind === 'note' ? <NoteItem text={item.text} /> : renderItem(item)}</div>
      ))}
    </div>
  );
}

/** The conversation itself: notes, turns, then anything waiting on the person. */
export function ConversationTranscript({ model, folded, onToggleFold, renderItem, failed, onRetry, answering, onAnswer }: Props) {
  const lastId = model.turns[model.turns.length - 1]?.id;
  return (
    <>
      <Preface items={model.preface} renderItem={renderItem} />
      {model.turns.map((turn) => (
        <TurnView
          key={turn.id}
          turn={turn}
          folded={Boolean(folded[turn.id])}
          onToggleFold={() => onToggleFold(turn.id)}
          renderItem={renderItem}
          onRetry={turn.id === lastId ? onRetry : undefined}
        />
      ))}
      {failed && (
        <div className="conversation-failure">
          <ErrorItem text={failed.message} onRetry={failed.text ? onRetry : undefined} />
        </div>
      )}
      {model.questions.map((question) => (
        <QuestionCard key={`${question.kind}:${question.id}:${question.ref ?? ''}`} question={question} busy={answering} onAnswer={onAnswer} />
      ))}
    </>
  );
}
