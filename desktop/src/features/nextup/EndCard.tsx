import { useEffect } from 'react';
import { Button } from '../../components/ui/Button';
import { Icon } from '../../components/ui/Icon';
import type { WorldRow } from '../world/types';
import './end-card.css';

export type EndCardProps = {
 answeredCount?: number;
 /** The walk supplies the whole world feed, including conversations without tabs here. */
 conversations?: readonly Pick<WorldRow, 'tasksRunning'>[];
 originLabel: string;
 onReturn: () => void;
};

/** Completing the walk returns to its origin without cancelling the work it released. */
export function EndCard({ answeredCount, conversations, originLabel, onReturn }: EndCardProps) {
 useEffect(() => {
  function escape(event: KeyboardEvent) {
   if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing) return;
   event.preventDefault();
   onReturn();
  }
  window.addEventListener('keydown', escape);
  return () => window.removeEventListener('keydown', escape);
 }, [onReturn]);

 // A missing or zero count stays absent rather than implying work the engine never reported.
 const running = conversations?.reduce((sum, row) => sum + row.tasksRunning, 0);
 const answered = answeredCount !== undefined && answeredCount > 0 ? `${answeredCount} answered.` : '';
 const tasks = running !== undefined && running > 0 ? `${running} ${running === 1 ? 'task is' : 'tasks are'} running.` : '';
 const note = [answered, tasks].filter(Boolean).join(' ');
 return <section className="nextup-end-card" aria-label="You're clear">
  <span className="nextup-end-card-check"><Icon name="check" /></span>
  <div className="nextup-end-card-copy">
   <span className="nextup-end-card-title">You're clear</span>
   {note && <span className="nextup-end-card-note" role="status">{note}</span>}
  </div>
  <Button variant="primary" className="nextup-end-card-return" onClick={onReturn}>Back to {originLabel}</Button>
 </section>;
}
