import { useEffect, useRef, type KeyboardEvent } from 'react';
import { Button, TextArea, TextInput } from '../../../components/ui';
import { canSend } from './answers';
import { type FormProps } from './InputForms';
import './clarify.css';

type ClarifyProps = FormProps & {
  onSend: () => void;
  onLater?: () => void;
  onDecide?: () => void;
};

const sendsOnKey = (event: KeyboardEvent) => event.key === 'Enter' && (event.metaKey || event.ctrlKey);

/** Clarification that needs words: the field takes focus, Send sits inside it, ⌘↵ sends.
 * Later folds the question into the header count; You decide hands it back to the asker.
 * A secret stays a masked single line: Enter sends, nothing is echoed. */
export function ClarifyForm({ question, draft, edit, locked, onSend, onLater, onDecide }: ClarifyProps) {
  const field = useRef<HTMLTextAreaElement & HTMLInputElement>(null);
  const prompt = question.input?.prompt || 'Your answer';
  const secret = Boolean(question.input?.secret);
  useEffect(() => field.current?.focus(), []);
  const keyDown = (event: KeyboardEvent) => {
    if (!sendsOnKey(event) && !(secret && event.key === 'Enter')) return;
    event.preventDefault();
    onSend();
  };
  const common = {
    ref: field,
    className: 'clarify-input',
    'aria-label': prompt,
    placeholder: prompt,
    disabled: locked,
    value: draft.change,
    onChange: (event: { target: { value: string } }) => edit({ change: event.target.value }),
    onKeyDown: keyDown,
  };
  return (
    <div className="clarify">
      <div className="clarify-field">
        {secret ? (
          <TextInput {...common} type="password" autoComplete="off" />
        ) : (
          <TextArea {...common} rows={1} />
        )}
        <Button variant="primary" className="clarify-send" disabled={locked || !canSend(question, draft)} onClick={onSend}>
          Send
        </Button>
      </div>
      {(onLater || onDecide) && (
        <div className="clarify-links">
          {onLater && <Button className="clarify-link" disabled={locked} onClick={onLater}>Later</Button>}
          {onDecide && <Button className="clarify-link" disabled={locked} onClick={onDecide}>You decide</Button>}
        </div>
      )}
    </div>
  );
}
