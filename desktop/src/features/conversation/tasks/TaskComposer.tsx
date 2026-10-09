import { useState, type KeyboardEvent } from 'react';
import { Button, IconButton, TextArea } from '../../../components/ui';
import './task-composer.css';

export type TaskComposerProps = {
  /** The task is over: there is nobody left to hear a note. */
  ended: boolean;
  /** Resolves when the note is delivered; a rejection keeps the draft and shows its message. */
  onNote: (text: string) => Promise<void> | void;
  /** The way out of an ended task, back to the conversation that ran it. */
  onMessageConversation?: () => void;
};

const PLACEHOLDER = 'Note to this task';
const MAX_ROWS = 6;

function Finished({ onMessageConversation }: Pick<TaskComposerProps, 'onMessageConversation'>) {
  return (
    <div className="task-composer task-composer-ended">
      <span>This task has finished</span>
      {onMessageConversation && <Button onClick={onMessageConversation}>Message the conversation</Button>}
    </div>
  );
}

/** Talks to a running task: Enter sends a note it reads at its next step. */
export function TaskComposer({ ended, onNote, onMessageConversation }: TaskComposerProps) {
  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  if (ended) return <Finished onMessageConversation={onMessageConversation} />;
  const blank = draft.trim() === '';

  async function send() {
    if (blank || sending) return;
    setSending(true);
    setError('');
    try {
      await onNote(draft.trim());
      setDraft('');
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'Could not send that.');
    } finally {
      setSending(false);
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
    if (event.key !== 'Enter' || event.shiftKey || event.altKey) return;
    event.preventDefault();
    void send();
  }

  return (
    <div className="task-composer">
      <div className="task-composer-bar" data-disabled={sending || undefined}>
        <TextArea
          className="task-composer-field"
          aria-label={PLACEHOLDER}
          placeholder={PLACEHOLDER}
          value={draft}
          rows={Math.min(draft.split('\n').length, MAX_ROWS)}
          readOnly={sending}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={onKeyDown}
        />
        <IconButton className="task-composer-send" label="Send note" icon="send" iconSize="sm" disabled={blank || sending} onClick={() => void send()} />
      </div>
      {error && <p className="task-composer-error" role="alert">{error}</p>}
    </div>
  );
}
