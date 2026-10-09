import { Button, Text } from '../../../components/ui';
import { clockText } from './clock';
import type { Question } from './form';

type FooterProps = {
  question: Question;
  now: number;
  held: boolean;
  locked: boolean;
  canDecide: boolean;
  onHold: () => void;
  onLater?: () => void;
  onDecide: () => void;
};

function waitingNote(question: Question): string | null {
  if (question.blocking?.turn) return 'The reply is waiting on this';
  const tasks = question.blocking?.tasks ?? [];
  return tasks.length ? `Holding up ${tasks.join(', ')}` : null;
}

function stakesNote(question: Question): string | null {
  return question.stakes === 'irreversible' ? 'This cannot be undone' : null;
}

/** Clock, Hold, Later, You decide: everything about time and delegation, never about the answer. */
export function CardFooter({ question, now, held, locked, canDecide, onHold, onLater, onDecide }: FooterProps) {
  const clock = clockText(question, now);
  const note = clock && !held ? clock : (waitingNote(question) ?? stakesNote(question));
  const showLater = onLater && !question.blocking?.turn && !clock;
  if (!note && !clock && !showLater && !canDecide) return null;
  return (
    <div className="tray-footer">
      <Text className="tray-caption" role={clock ? 'timer' : undefined}>
        {held && clock ? 'On hold — take your time' : note}
      </Text>
      <div className="tray-footer-actions">
        {clock && !held && <Button disabled={locked} onClick={onHold}>Hold</Button>}
        {showLater && <Button disabled={locked} onClick={onLater}>Later</Button>}
        {canDecide && <Button disabled={locked} onClick={onDecide}>You decide</Button>}
      </div>
    </div>
  );
}
