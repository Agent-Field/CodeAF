import { useState, type KeyboardEvent } from 'react';
import { Button, DropdownMenu, IconButton, TextArea, type MenuEntry } from '../../../components/ui';
import '../composer.css';
import './task-composer.css';

export type TaskComposerProps = {
  /** The task is over: there is nobody left to hear a note. */
  ended: boolean;
  paused: boolean;
  /** Resolves when the note is delivered; a rejection keeps the draft and shows its message. */
  onNote: (text: string) => Promise<void> | void;
  onAmend: (text: string) => Promise<void> | void;
  onPause?: () => void;
  onResume?: () => void;
  onStop?: () => void;
  /** The way out of an ended task, back to the conversation that ran it. */
  onMessageConversation?: () => void;
};

type Mode = 'note' | 'amend';

const placeholders: Record<Mode, string> = { note: 'Note to this task', amend: 'New brief for this task' };

function Finished({ onMessageConversation }: Pick<TaskComposerProps, 'onMessageConversation'>) {
  return (
    <div className="task-composer task-composer-ended">
      <span>This task has finished</span>
      {onMessageConversation && <Button onClick={onMessageConversation}>Message the conversation</Button>}
    </div>
  );
}

function menuItems(props: TaskComposerProps, amend: () => void): MenuEntry[] {
  const { paused, onPause, onResume, onStop } = props;
  const pause = paused ? { id: 'resume', label: 'Resume', icon: 'play' as const, onSelect: () => onResume?.() } : { id: 'pause', label: 'Pause', icon: 'pause' as const, onSelect: () => onPause?.() };
  const entries: (MenuEntry | false)[] = [
    { id: 'amend', label: 'Change the brief', icon: 'edit', onSelect: amend },
    Boolean(paused ? onResume : onPause) && pause,
    Boolean(onStop) && { id: 'stop', label: 'Stop', icon: 'stop', danger: true, onSelect: () => onStop?.() },
  ];
  return entries.filter((entry): entry is MenuEntry => Boolean(entry));
}

/** Talks to a running task: Enter sends a note it reads at its next step. */
export function TaskComposer(props: TaskComposerProps) {
  const [draft, setDraft] = useState('');
  const [mode, setMode] = useState<Mode>('note');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  if (props.ended) return <Finished onMessageConversation={props.onMessageConversation} />;
  const blank = draft.trim() === '';

  async function send() {
    if (blank || sending) return;
    setSending(true);
    setError('');
    try {
      await (mode === 'amend' ? props.onAmend : props.onNote)(draft.trim());
      setDraft('');
      setMode('note');
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'Could not send that.');
    } finally {
      setSending(false);
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
    if (event.key === 'Escape' && mode === 'amend') return setMode('note');
    if (event.key !== 'Enter' || event.shiftKey || event.altKey) return;
    event.preventDefault();
    void send();
  }

  const rows = Math.min(draft.split('\n').length, 6);
  return (
    <div className="task-composer">
      <div className="composer" data-disabled={sending || undefined}>
        <TextArea
          className="composer-field"
          aria-label={placeholders[mode]}
          placeholder={placeholders[mode]}
          value={draft}
          rows={rows}
          readOnly={sending}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={onKeyDown}
        />
        <div className="composer-row">
          <div className="composer-tools">
            {mode === 'amend' && <Button className="task-composer-mode" onClick={() => setMode('note')}>Changing the brief</Button>}
          </div>
          <div className="composer-actions">
            <DropdownMenu label="Task actions" items={menuItems(props, () => setMode('amend'))}>
              <IconButton label="More task actions" icon="more" iconSize="sm" />
            </DropdownMenu>
            <IconButton className="composer-primary" label="Send note" icon="send" iconSize="sm" disabled={blank || sending} onClick={() => void send()} />
          </div>
        </div>
      </div>
      {error && <p className="task-composer-error" role="alert">{error}</p>}
    </div>
  );
}
