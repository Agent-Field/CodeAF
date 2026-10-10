import { useState, type MouseEvent } from 'react';
import { Button, Icon } from '../../components/ui';
import { ReceiptLine, type DecisionReceipt, type DecisionReceiptActions } from './ReceiptLine';
import './receipt.css';

const middle = '\u00b7';

/** One action inside a grouped receipt. `summary` is the short phrase in the closed line. */
export type GroupedAction = DecisionReceipt & { summary: string };

/**
 * Consecutive actions in one turn. The specimen is
 * "Did 3 things · held Launch post, asked Software, saved a note" with a chevron, and opening it
 * shows each action's own receipt. One action is that receipt alone: "Did 1 thing" is not a sentence
 * the design draws.
 */
export function GroupedReceipt({ actions, onOpen, onWhy }: { actions: readonly GroupedAction[] } & DecisionReceiptActions) {
  const [open, setOpen] = useState(false);
  const shown = actions.filter(action => action.placeName.trim());
  if (shown.length === 0) return null;
  if (shown.length === 1) {
    const only = shown[0];
    return <ReceiptLine text={only.text} questionId={only.questionId} placeName={only.placeName} reason={only.reason} denied={only.denied} onOpen={onOpen} onWhy={onWhy}/>;
  }
  const phrases = shown.map(action => action.summary.trim()).filter(phrase => phrase.length > 0);
  const toggle = (event: MouseEvent) => { event.stopPropagation(); setOpen(value => !value); };
  // Sparkle, sentence and chevron are one flex row with an 8px gap. Newlines between them
  // would count as extra items, so the three children are adjacent.
  const summary = <Button className="decision-receipt-summary" aria-expanded={open} onClick={toggle}><Icon name="sparkle" size="tiny"/><span className="decision-receipt-text">{`Did ${shown.length} things`}{phrases.length > 0 && <>{` ${middle} `}{phrases.map((phrase, index) => <span key={`${phrase}-${index}`}>{index > 0 ? ', ' : null}<span className="decision-receipt-actor">{phrase}</span></span>)}</>}</span><span className="decision-receipt-chevron"><Icon name="chevron" size="micro" motion="disclosure"/></span></Button>;
  return <div className="decision-receipt-group" data-open={open || undefined}>{summary}{open && shown.map(action => <ReceiptLine key={action.questionId} text={action.text} questionId={action.questionId} placeName={action.placeName} reason={action.reason} denied={action.denied} onOpen={onOpen} onWhy={onWhy}/>)}</div>;
}
