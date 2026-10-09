import type { ReactNode } from 'react';
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
  /** False when the form draws the clock itself (the choice cards). */
  clock?: boolean;
  /** Said here only when no row of answers above could carry it. */
  note?: ReactNode;
};

function waitingNote(question: Question): string | null {
  // That the reply is waiting is said once, in the tray head; only held-up tasks are named here.
  const tasks = question.blocking?.tasks ?? [];
  return tasks.length ? `Holding up ${tasks.join(', ')}` : null;
}

/** Clock, Hold, Later, You decide: everything about time and delegation, never about the answer. */
export function CardFooter({ question, now, held, locked, canDecide, onHold, onLater, onDecide, clock: showClock = true, note: trailing }: FooterProps) {
  const ticking = clockText(question, now);
  const clock = showClock ? ticking : null;
  const note = clock && !held ? clock : waitingNote(question);
  const showLater = onLater && !question.blocking?.turn && !ticking;
  if (!note && !clock && !showLater && !canDecide && !trailing) return null;
  return (
    <div className="tray-footer">
      <Text className="tray-caption" role={clock ? 'timer' : undefined}>
        {held && clock ? 'On hold — take your time' : note}
      </Text>
      <div className="tray-footer-actions">
        {clock && !held && <Button disabled={locked} onClick={onHold}>Hold</Button>}
        {showLater && <Button disabled={locked} onClick={onLater}>Later</Button>}
        {canDecide && <Button disabled={locked} onClick={onDecide}>You decide</Button>}
        {trailing}
      </div>
    </div>
  );
}
