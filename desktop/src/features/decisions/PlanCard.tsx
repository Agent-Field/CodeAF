import { useState } from 'react';
import { Button, IconButton } from '../../components/ui/Button';
import { Icon, type IconName } from '../../components/ui/Icon';
import { TextInput } from '../../components/ui/TextInput';
import './plan-card.css';

/** Mirrors session.PlanCardStep on the wire; the display words ride beside it, never inside it. */
export type PlanCardStepKind = 'hold' | 'steer' | 'start' | 'stop' | 'ask-place' | 'remember';
export type PlanCardStepData = {
 kind: PlanCardStepKind;
 target: { chat?: string; task?: string; place?: string };
 text: string;
 /** The target's name as the sentence spells it; it is set in the heavier weight when found in the text. */
 targetLabel?: string;
 /** The target no longer exists, so the step reads as skipped rather than as an action. */
 gone?: boolean;
};

/** The design names three glyphs; the other kinds borrow the nearest verb already in the vocabulary. */
const GLYPH: Record<PlanCardStepKind, IconName> = {
 hold: 'cornerDownRight', 'ask-place': 'messagesSquare', remember: 'bookmark', steer: 'steer', start: 'play', stop: 'stop',
};

export type PlanCardProps = {
 steps: readonly PlanCardStepData[];
 /** Go runs the list as shown, so an edited list is sent first. */
 onEdit: (steps: PlanCardStepData[]) => Promise<void> | void;
 onGo: () => Promise<void> | void;
 /** Dismisses the card; the engine takes no action. */
 onCancel: () => void;
};

function StepText({ step }: { step: PlanCardStepData }) {
 const { text, targetLabel, gone } = step;
 // A deleted target is said once, plainly; the step's original sentence would promise work that will not happen.
 if (gone) return <span className="plan-card-skipped">{targetLabel ? `${targetLabel} was deleted, so this step was skipped` : 'This step was skipped'}</span>;
 const at = targetLabel ? text.indexOf(targetLabel) : -1;
 if (!targetLabel || at < 0) return <span>{text}</span>;
 return <span>{text.slice(0, at)}<b className="plan-card-target">{targetLabel}</b>{text.slice(at + targetLabel.length)}</span>;
}

/** A plan that reaches beyond this chat waits here for a person's answer. */
export function PlanCard({ steps, onEdit, onGo, onCancel }: PlanCardProps) {
 const [draft, setDraft] = useState<PlanCardStepData[] | null>(null);
 const [busy, setBusy] = useState(false);
 const editing = draft !== null;
 const shown = draft ?? steps;

 async function commit(): Promise<boolean> {
  if (!draft) return true;
  try { await onEdit(draft); setDraft(null); return true; } catch { return false; }
 }
 async function run(action: () => Promise<void> | void) {
  setBusy(true);
  try { await action(); } finally { setBusy(false); }
 }
 const change = (index: number, patch: Partial<PlanCardStepData>) => setDraft(shown.map((s, i) => i === index ? { ...s, ...patch } : s));

 return <section className="plan-card" aria-label="Plan">
  <span className="plan-card-head">Plan · reaches beyond this chat</span>
  <ul className="plan-card-steps">
   {shown.map((step, i) => <li className="plan-card-step" key={i}>
    <span className="plan-card-glyph"><Icon name={GLYPH[step.kind]}/></span>
    {editing && !step.gone
     ? <TextInput appearance="field" className="plan-card-edit-input" aria-label={`Step ${i + 1}`} value={step.text} onChange={e => change(i, { text: e.target.value })}/>
     : <StepText step={step}/>}
    {editing && <IconButton label={`Remove step ${i + 1}`} icon="close" size="row" onClick={() => setDraft(shown.filter((_, j) => j !== i))}/>}
   </li>)}
  </ul>
  <div className="plan-card-buttons">
   <Button variant="primary" disabled={busy || shown.length === 0} onClick={() => run(async () => { if (await commit()) await onGo(); })}>Go</Button>
   <Button variant="quiet" disabled={busy} onClick={() => editing ? run(async () => { await commit(); }) : setDraft([...steps])}>{editing ? 'Done' : 'Edit'}</Button>
   <Button variant="ghost" disabled={busy} onClick={onCancel}>Cancel</Button>
  </div>
 </section>;
}
