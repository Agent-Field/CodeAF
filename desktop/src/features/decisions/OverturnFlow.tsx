import { useState } from 'react';
import { Button } from '../../components/ui/Button';
import { toasts as windowToasts, type Toasts } from '../../design/toasts';
import './overturn.css';

/** What to do about the work that built on the decision. Absent means the person chose nothing, which cascades to nobody. */
export type DependentsChoice = 'notify' | 'pause' | 'leave';

export type OverturnRequest = { choice?: DependentsChoice };

export type OverturnFlowProps = {
 /** How many tasks used the decision; the engine's count, so zero or unknown draws no dependents block at all. */
 dependents?: number;
 /** Sends the overturn. Resolves with a way to take it back, when the engine offers one. */
 onOverturn: (request: OverturnRequest) => Promise<{ undo?: () => void | Promise<void> } | void>;
 onCancel: () => void;
 /** Closes the confirmation after a successful overturn; the toast outlives it. */
 onDone?: () => void;
 channel?: Toasts;
};

const CHOICES: readonly { value: DependentsChoice; label: string }[] = [
 { value: 'notify', label: 'Notify them' },
 { value: 'pause', label: 'Pause them' },
 { value: 'leave', label: 'Leave them' },
];

const used = (n: number) => `${n} ${n === 1 ? 'task' : 'tasks'} used this`;

/** The confirmation behind Overturn: names the tasks that built on the decision and asks what to do about them. NOTHING is
 * preselected, because an overturn must never cascade to other work the person did not choose to touch. */
export function OverturnFlow({ dependents, onOverturn, onCancel, onDone, channel = windowToasts }: OverturnFlowProps) {
 const [choice, setChoice] = useState<DependentsChoice>();
 const [busy, setBusy] = useState(false);
 const [failure, setFailure] = useState<string>();

 const confirm = async () => {
  setBusy(true);
  setFailure(undefined);
  try {
   const result = await onOverturn(choice ? { choice } : {});
   channel.show({ key: 'overturn', message: ['Decision overturned.'], undo: result?.undo });
   onDone?.();
  } catch (error) {
   // A refusal stays on the card so the person can try again or cancel; it is never swallowed.
   setFailure(error instanceof Error && error.message ? error.message : 'That did not work.');
   setBusy(false);
  }
 };

 return <div className="overturn" role="dialog" aria-label="Overturn this decision">
  <span className="overturn-title">Overturn this decision?</span>
  {dependents !== undefined && dependents > 0 && <>
   <span className="overturn-used">{used(dependents)}</span>
   <div className="overturn-choices" role="radiogroup" aria-label="Tasks that used this">
    {CHOICES.map(c => <Button key={c.value} role="radio" aria-checked={choice === c.value} variant={choice === c.value ? 'quiet' : 'ghost'} className="overturn-button" onClick={() => setChoice(c.value)}>{c.label}</Button>)}
   </div>
  </>}
  {failure && <span className="overturn-failure" role="alert">{failure}</span>}
  <div className="overturn-buttons">
   <Button variant="quiet" className="overturn-button" autoFocus loading={busy} onClick={confirm}>Overturn</Button>
   <Button variant="ghost" className="overturn-button" disabled={busy} onClick={onCancel}>Cancel</Button>
  </div>
 </div>;
}
