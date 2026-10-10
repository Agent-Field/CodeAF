import { useEffect, useId, useRef, useState } from 'react';
import { Button } from '../../components/ui/Button';
import './why.css';

export type WhyNextTime = 'ask' | 'keep';

export type WhyPopoverProps = {
 /** The place that decided. */
 by?: string;
 /** The reason, already worded for a person; reversibility rides inside it ("Reversible."). */
 because?: string;
 /** Confidence from 0 to 1. Unknown renders as no row at all, never "0%". */
 sure?: number;
 /** What the person last chose for the next question of this kind; absent until they choose. */
 nextTime?: WhyNextTime;
 onNextTime: (choice: WhyNextTime) => void;
 onOverturn: () => void;
 onOpenDecision: () => void;
};

const CHOICES: readonly { value: WhyNextTime; label: string }[] = [
 { value: 'ask', label: 'Always ask me' },
 { value: 'keep', label: 'Fine, keep deciding' },
];

/** The "Why?" link on an automatic decision's receipt, and the card it opens: by, because, sure, then what to do about it. */
export function WhyPopover({ by, because, sure, nextTime, onNextTime, onOverturn, onOpenDecision }: WhyPopoverProps) {
 const [open, setOpen] = useState(false);
 const root = useRef<HTMLSpanElement>(null);
 const trigger = useRef<HTMLButtonElement>(null);
 const id = useId();

 // Escape and a press outside both close it; only Escape hands focus back, because a press elsewhere has already chosen where focus goes.
 useEffect(() => {
  if (!open) return;
  const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') { setOpen(false); trigger.current?.focus(); } };
  const onDown = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false); };
  document.addEventListener('keydown', onKey);
  document.addEventListener('pointerdown', onDown);
  return () => { document.removeEventListener('keydown', onKey); document.removeEventListener('pointerdown', onDown); };
 }, [open]);

 const facts: [string, string][] = [];
 if (by) facts.push(['By', by]);
 if (because) facts.push(['Because', because]);
 if (sure !== undefined) facts.push(['Sure', `${Math.round(sure * 100)}%`]);

 // Closing first means a slow engine answer cannot leave the card hanging over the receipt it changed.
 const act = (action: () => void) => () => { setOpen(false); trigger.current?.focus(); action(); };

 return <span className="why" ref={root}>
  <Button ref={trigger} variant="ghost" className="why-trigger" aria-expanded={open} aria-controls={open ? id : undefined} aria-haspopup="dialog" onClick={() => setOpen(o => !o)}>Why?</Button>
  {open && <div id={id} className="why-card" role="dialog" aria-label="Decided automatically">
   <span className="why-title">Decided automatically</span>
   {facts.length > 0 && <dl className="why-facts">
    {facts.map(([term, text]) => <div className="why-fact" key={term}><dt>{term}</dt><dd>{text}</dd></div>)}
   </dl>}
   <div className="why-next" role="radiogroup" aria-label="Next time…">
    <span className="why-next-label">Next time…</span>
    <div className="why-next-choices">
     {CHOICES.map(c => <Button key={c.value} role="radio" aria-checked={nextTime === c.value} variant={nextTime === c.value ? 'quiet' : 'ghost'} className="why-button" onClick={() => onNextTime(c.value)}>{c.label}</Button>)}
    </div>
   </div>
   <div className="why-buttons">
    <Button variant="quiet" className="why-button" autoFocus onClick={act(onOverturn)}>Overturn</Button>
    <Button variant="ghost" className="why-button why-open" onClick={act(onOpenDecision)}>Open the decision</Button>
   </div>
  </div>}
 </span>;
}
