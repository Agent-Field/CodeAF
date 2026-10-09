import { useLayoutEffect, useRef, type KeyboardEvent, type RefObject } from 'react';
import { Button, DropdownMenu, IconButton, Text, TextArea, type MenuEntry } from '../../components/ui';
import design from '../../design/tokens.json';
import './composer.css';

export type SendMode = 'submit' | 'steer' | 'queue';

export type ComposerProps = {
  draft: string;
  onDraft: (value: string) => void;
  onSend: (text: string, mode: SendMode) => Promise<boolean> | boolean;
  onStop: () => void;
  running: boolean;
  docked: boolean;
  disabledReason?: string;
  /** The one way out when the field is disabled, e.g. back to the conversation. */
  reasonAction?: { label: string; onClick: () => void };
  modelLabel?: string;
  onAttach?: () => void;
  tasksToggle?: { label: string; onClick: () => void };
  recallLast?: () => string | undefined;
  autoFocus?: boolean;
};

const { composerMinRows, composerMaxRows } = design.interaction;

function isComposing(event: KeyboardEvent) {
  return event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229;
}

function useAutosize(ref: RefObject<HTMLTextAreaElement | null>, draft: string) {
  useLayoutEffect(() => {
    const field = ref.current;
    if (!field) return;
    field.rows = composerMinRows;
    const style = getComputedStyle(field);
    const line = parseFloat(style.lineHeight);
    if (!line) return;
    const padding = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
    const lines = Math.ceil((field.scrollHeight - padding) / line);
    field.rows = Math.min(composerMaxRows, Math.max(composerMinRows, lines));
  }, [ref, draft]);
}

export function Composer(props: ComposerProps) {
  const { draft, onDraft, onSend, onStop, running, docked, disabledReason, autoFocus } = props;
  const field = useRef<HTMLTextAreaElement>(null);
  useAutosize(field, draft);
  const disabled = !!disabledReason;
  const blank = draft.trim() === '';

  async function send(mode: SendMode) {
    if (blank || disabled) return;
    const accepted = await onSend(draft.trim(), mode);
    if (accepted) onDraft('');
  }

  function recall(event: KeyboardEvent) {
    const last = props.recallLast?.();
    if (last === undefined) return;
    event.preventDefault();
    onDraft(last);
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (isComposing(event)) return;
    if (event.key === 'Escape') return event.currentTarget.blur();
    if (event.key === 'ArrowUp' && draft === '') return recall(event);
    const plainEnter = event.key === 'Enter' && !event.shiftKey && !event.altKey;
    if (!plainEnter) return;
    event.preventDefault();
    void send(running ? 'steer' : 'submit');
  }

  const queueItems: MenuEntry[] = [
    { id: 'queue', label: 'Queue for after this turn', onSelect: () => void send('queue') },
  ];
  const stopping = running && blank;

  return (
    <div className="composer-dock" data-docked={docked}>
      <div className="composer" data-running={running} data-disabled={disabled}>
        <TextArea
          ref={field}
          className="composer-field"
          aria-label="Message"
          placeholder={disabledReason ?? 'Message codeaf'}
          value={draft}
          disabled={disabled}
          autoFocus={autoFocus}
          rows={composerMinRows}
          onChange={event => onDraft(event.target.value)}
          onKeyDown={onKeyDown}
        />
        <div className="composer-row">
          <div className="composer-tools">
            {props.onAttach && <IconButton label="Attach file" icon="attach" iconSize="sm" disabled={disabled} onClick={props.onAttach} />}
            {props.modelLabel && <Text className="composer-model">{props.modelLabel}</Text>}
            {props.tasksToggle && <Button className="composer-tasks" onClick={props.tasksToggle.onClick}>{props.tasksToggle.label}</Button>}
          </div>
          <div className="composer-actions">
            {running && !blank && (
              <DropdownMenu label="Send options" items={queueItems}>
                <IconButton label="More send options" icon="more" iconSize="sm" />
              </DropdownMenu>
            )}
            {stopping ? (
              <IconButton className="composer-primary" label="Stop" icon="stop" iconSize="sm" onClick={onStop} />
            ) : (
              <IconButton
                className="composer-primary"
                label={running ? 'Steer' : 'Send'}
                icon="send"
                iconSize="sm"
                disabled={blank || disabled}
                onClick={() => void send(running ? 'steer' : 'submit')}
              />
            )}
          </div>
        </div>
      </div>
      {props.reasonAction && (
        <div className="composer-reason">
          <Button onClick={props.reasonAction.onClick}>{props.reasonAction.label}</Button>
        </div>
      )}
    </div>
  );
}
