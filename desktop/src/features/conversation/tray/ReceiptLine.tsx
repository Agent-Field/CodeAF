import { useState, type ReactNode } from 'react';
import { Button, Icon } from '../../../components/ui';
import './receipt.css';

export type ReceiptState = 'waiting' | 'answered' | 'withdrawn';

const SEPARATOR = ' · ';

/** Backtick spans are commands: set them in mono. */
function withCode(text: string): ReactNode[] {
  return text.split('`').map((part, at) => (at % 2 ? <span key={at} className="receipt-code">{part}</span> : part));
}

/** "Allowed once · you · 14:02": the verdict reads stronger than who and when. */
function answeredText(text: string): ReactNode {
  const at = text.indexOf(SEPARATOR);
  if (at < 0) return <span className="receipt-label">{text}</span>;
  return (
    <>
      <span className="receipt-label">{text.slice(0, at)}</span>{' '}
      <span className="receipt-rest">{text.slice(at + SEPARATOR.length)}</span>
    </>
  );
}

const MARKS: Record<ReceiptState, ReactNode> = {
  waiting: <span className="receipt-dot" />,
  answered: <Icon name="check" size="xs" />,
  withdrawn: <span className="receipt-ring" />,
};

function body(state: ReceiptState, text: string): ReactNode {
  return state === 'answered' ? answeredText(text) : withCode(text);
}

/** The quiet line left in the conversation where a question was asked. Waiting ones focus the tray; the others open to show the whole answer. */
export function ReceiptLine({ state, text, onFocus }: { state: ReceiptState; text: string; onFocus?: () => void }) {
  const [open, setOpen] = useState(false);
  const content = (
    <>
      <span className="receipt-mark" aria-hidden="true">{MARKS[state]}</span>
      <span className="receipt-text" data-split={state === 'answered' || undefined}>{body(state, text)}</span>
    </>
  );
  if (state === 'waiting') {
    return onFocus ? <Button className="receipt" data-state={state} onClick={onFocus}>{content}</Button> : <div className="receipt" data-state={state}>{content}</div>;
  }
  return (
    <Button className="receipt" data-state={state} aria-expanded={open} onClick={() => setOpen(!open)}>
      {content}
    </Button>
  );
}
