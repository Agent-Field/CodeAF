import { useLayoutEffect, useRef, type ClipboardEvent, type KeyboardEvent, type RefObject } from 'react';
import { Button, Icon, IconButton, Text, TextArea, TextInput } from '../../components/ui';
import design from '../../design/tokens.json';
import type { OutgoingFile } from '../chat/engine-client';
import { AttachmentTray } from './composer/AttachmentTray';
import { toOutgoing } from './composer/attachments';
import { useAttachments } from './composer/useAttachments';
import { useFileDrop } from './composer/useFileDrop';
import './composer.css';

export type SendMode = 'submit' | 'steer' | 'queue';

export type ComposerProps = {
  draft: string;
  onDraft: (value: string) => void;
  /** `files` is passed only when something is attached; attachments clear only if this resolves true. */
  onSend: (text: string, mode: SendMode, files?: OutgoingFile[]) => Promise<boolean> | boolean;
  onStop: () => void;
  running: boolean;
  docked: boolean;
  disabledReason?: string;
  /** The one way out when the field is disabled, e.g. back to the conversation. */
  reasonAction?: { label: string; onClick: () => void };
  modelLabel?: string;
  tasksToggle?: { label: string; onClick: () => void };
  recallLast?: () => string | undefined;
  autoFocus?: boolean;
  /** Forces the drag appearance; for specimens, since real drags are global to the window. */
  dropState?: 'page' | 'over';
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
    // scrollHeight is rounded to whole pixels, so a lone line can read a hair over one.
    const lines = Math.round((field.scrollHeight - padding) / line);
    field.rows = Math.min(composerMaxRows, Math.max(composerMinRows, lines));
  }, [ref, draft]);
}

export function Composer(props: ComposerProps) {
  const { draft, onDraft, onSend, onStop, running, docked, disabledReason, autoFocus } = props;
  const field = useRef<HTMLTextAreaElement>(null);
  useAutosize(field, draft);
  const disabled = !!disabledReason;
  const attachments = useAttachments();
  const picker = useRef<HTMLInputElement>(null);
  const drop = useFileDrop(attachments.add, !disabled);
  const dropState = props.dropState ?? (drop.over ? 'over' : drop.pageDrag ? 'page' : undefined);
  const blank = draft.trim() === '' && attachments.items.length === 0;

  async function send(mode: SendMode) {
    if (blank || disabled) return;
    const { items } = attachments;
    const accepted = items.length
      ? await onSend(draft.trim(), mode, await toOutgoing(items))
      : await onSend(draft.trim(), mode);
    if (!accepted) return;
    onDraft('');
    attachments.clear();
  }

  function onPaste(event: ClipboardEvent) {
    const files = Array.from(event.clipboardData.files);
    if (files.length === 0) return;
    event.preventDefault();
    attachments.add(files);
  }

  function onPicked(input: HTMLInputElement) {
    attachments.add(Array.from(input.files ?? []));
    input.value = '';
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
    if (event.key !== 'Enter' || event.shiftKey) return;
    if (event.altKey && !running) return;
    event.preventDefault();
    void send(event.altKey ? 'queue' : running ? 'steer' : 'submit');
  }

  const stopping = running && blank;
  const steering = running && !blank;

  return (
    <div className="composer-dock" data-docked={docked}>
      <div
        className="composer"
        data-running={running}
        data-steering={steering}
        data-disabled={disabled}
        data-drop={dropState}
        {...drop.handlers}
      >
        {dropState && <Text className="composer-drop-hint">Drop to attach</Text>}
        <AttachmentTray items={attachments.items} onRemove={attachments.remove} />
        <TextArea
          ref={field}
          className="composer-field"
          aria-label="Message"
          placeholder={disabledReason ?? (running ? 'Steer, or queue a message' : 'Ask codeaf')}
          value={draft}
          disabled={disabled}
          autoFocus={autoFocus}
          rows={composerMinRows}
          onChange={event => onDraft(event.target.value)}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
        />
        {attachments.error && <Text className="composer-error" role="status">{attachments.error}</Text>}
        <div className="composer-row">
          <div className="composer-tools">
            <TextInput
              ref={picker}
              type="file"
              multiple
              hidden
              className="composer-file-input"
              tabIndex={-1}
              aria-hidden
              data-testid="composer-file-input"
              onChange={event => onPicked(event.currentTarget)}
            />
            <IconButton
              className="composer-attach"
              label="Attach files"
              icon="plus"
              iconSize="sm"
              disabled={disabled}
              onClick={() => picker.current?.click()}
            />
            {props.modelLabel && <Text className="composer-model">{props.modelLabel}</Text>}
            {props.tasksToggle && <Button className="composer-tasks" onClick={props.tasksToggle.onClick}>{props.tasksToggle.label}</Button>}
          </div>
          <div className="composer-actions">
            {stopping && (
              <Button className="composer-primary composer-stop" aria-label="Stop" title="Stop" onClick={onStop}>
                <span className="composer-stop-mark" aria-hidden="true" />
              </Button>
            )}
            {steering && (
              <>
                <Button className="composer-queue" onClick={() => void send('queue')}>Queue ⌥↵</Button>
                <Button className="composer-steer" disabled={disabled} onClick={() => void send('steer')}>
                  <Icon name="steer" size="xs" />
                  Steer
                </Button>
              </>
            )}
            {!running && (
              <IconButton
                className="composer-primary composer-send"
                label="Send"
                icon="send"
                iconSize="sm"
                disabled={blank || disabled}
                onClick={() => void send('submit')}
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
