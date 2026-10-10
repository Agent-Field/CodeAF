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
  // That the reply is waiting is said once, in the tray head. Held-up tasks are one muted line here
  // (Components edge: "Holding up 2 tasks: …"), never a second heading.
  const tasks = question.blocking?.tasks ?? [];
  if (!tasks.length) return null;
  const noun = tasks.length === 1 ? 'task' : 'tasks';
  return `Holding up ${tasks.length} ${noun}: ${tasks.join(', ')}`;
}

/** Clock, Hold, Later, You decide: everything about time and delegation, never about the answer. */
export function CardFooter({ question, now, held, locked, canDecide, onHold, onLater, onDecide, clock: showClock = true, note: trailing }: FooterProps) {
  const ticking = clockText(question, now);
  const clock = showClock ? ticking : null;
  const holding = waitingNote(question);
  const note = clock && !held ? clock : holding;
  const showLater = onLater && !question.blocking?.turn && !ticking;
  const shown = held && clock ? 'On hold — take your time' : note;
  if (!shown && !showLater && !canDecide && !trailing) return null;
  return (
    <div className="tray-footer">
      {shown && (
        <Text className={shown === holding ? 'tray-holding' : 'tray-caption'} role={clock ? 'timer' : undefined}>
          {shown}
        </Text>
      )}
      <div className="tray-footer-actions">
        {clock && !held && <Button disabled={locked} onClick={onHold}>Hold</Button>}
        {showLater && <Button disabled={locked} onClick={onLater}>Later</Button>}
        {canDecide && <Button disabled={locked} onClick={onDecide}>You decide</Button>}
        {trailing}
      </div>
    </div>
  );
}
