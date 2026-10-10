import type { MouseEvent } from 'react';
import { Button, Icon } from '../../components/ui';
import './receipt.css';

const middle = '\u00b7';

/** One automatic decision, as the transcript aside names it. The tray id is what a click opens. */
export type DecisionReceipt = {
  questionId: string;
  /** The journal sentence wins when supplied; older specimens compose their own line. */
  text?: string;
  /** The place that decided. A blank name draws nothing: the line would have no actor. */
  placeName: string;
  /** The clause between the place and Why?. Omitted when the engine sent none. */
  reason?: string;
  /** Policy refused the action. Same line and the same ink; the lead says it was not allowed. */
  denied?: boolean;
};

export type DecisionReceiptActions = {
  /** Opens that question in the tray. Why? does not. */
  onOpen: (questionId: string) => void;
  /** Asks for the reason. The explanation itself is the Why? popover, not this line. */
  onWhy: (questionId: string) => void;
};

function lead(denied: boolean | undefined): string {
  // The specimen's allowed sentence is fixed. A denial has no sentence in the design, so the
  // shipped lead is the one recorded in DESIGN-QUESTIONS RCPT-212.
  return denied ? 'Not allowed by' : 'Allowed automatically by';
}

/**
 * The quiet line an automatic decision leaves in the flow.
 * "Allowed automatically by Config parser · you always allow this here · Why?"
 * The place and Why? are ink-2. Clicking the line opens that question; Why? is its own button
 * so the two clicks do not share a control.
 */
export function ReceiptLine({ questionId, placeName, reason, denied, text, onOpen, onWhy }: DecisionReceipt & DecisionReceiptActions) {
  const place = placeName.trim();
  if (!place) return null;
  const clause = reason?.trim();
  const open = (event: MouseEvent) => { event.stopPropagation(); onOpen(questionId); };
  const why = (event: MouseEvent) => { event.stopPropagation(); onWhy(questionId); };
  const recorded = text?.replace(/\s*·\s*Why\?\s*$/, '').trim();
  const at = recorded?.indexOf(place) ?? -1;
  const words = recorded ? at >= 0 ? <>{recorded.slice(0, at)}<span className="decision-receipt-actor">{place}</span>{recorded.slice(at + place.length)}</> : recorded : <>{lead(denied)} <span className="decision-receipt-actor">{place}</span>{clause ? ` ${middle} ${clause}` : ''}</>;
  // The children sit in one flex row. A newline between them would be its own flex item and
  // open a gap the specimen does not have, so they are adjacent on purpose.
  const sentence = <Button className="decision-receipt-open" onClick={open}><Icon name="sparkle" size="tiny"/><span className="decision-receipt-text">{words}</span></Button>;
  const ask = <Button className="decision-receipt-why" onClick={why}>Why?</Button>;
  return <div className="decision-receipt" data-outcome={denied ? 'denied' : 'allowed'}>{sentence}<span className="decision-receipt-sep">{` ${middle} `}</span>{ask}</div>;
}
