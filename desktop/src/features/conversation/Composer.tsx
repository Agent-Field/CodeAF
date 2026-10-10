import { useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent, type RefObject } from 'react';
import { Button, ContextMenu, Icon, IconButton, Text, TextArea, TextInput } from '../../components/ui';
import design from '../../design/tokens.json';
import { composerShortcuts } from '../../design/keyboard';
import type { OutgoingFile } from '../chat/engine-client';
import { AtPicker, useAtPicker } from './composer/AtPicker';
import { PasteCard } from './composer/PasteCard';
import { encodePasted, countLines, isLongPaste, splitPasted } from './composer/pastedText';
import { LinkChip } from './assets';
import { AttachmentTray } from './composer/AttachmentTray';
import { ModelPicker } from './composer/ModelPicker';
import type { ConversationModel } from './composer/useConversationModel';
import { toOutgoing } from './composer/attachments';
import { useAttachments } from './composer/useAttachments';
import { useFileDrop } from './composer/useFileDrop';
import { useKeyboardFocus } from './composer/useKeyboardFocus';
import './composer.css';

export type SendMode = 'submit' | 'steer' | 'queue';

export type ComposerProps = {
  /** Places Home uses the same send, attachment and model controls in one inline row. */
  variant?: 'home';
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
  /** The chip's word for modelLabel when the model is pinned under a short name. */
  modelShort?: string;
  /** The engine's model for the Conversation role; when present it replaces the read-only chip. */
  model?: ConversationModel;
  tasksToggle?: { label: string; onClick: () => void };
  recallLast?: () => string | undefined;
  autoFocus?: boolean;
  /** Files a hand-off (a web page's picture) puts in the tray once, on mount. Nothing is sent. */
  offeredFiles?: File[];
  onOfferedFiles?: () => void;
  /**
   * The page chat-plus attached: its address and title, drawn as a link and not
   * sent. The composer attaches files only, so this is the existing link chip
   * rather than a second attachment kind.
   */
  offeredLink?: { url: string; title: string };
  onOfferedLink?: () => void;
  /** Forces the drag appearance; for specimens, since real drags are global to the window. */
  dropState?: 'page' | 'over';
  /** The idle prompt in the empty field when the surface has its own words ("Start something in codeaf" on a place's Home). */
  placeholder?: string;
  /**
   * The conversation's engine session. Typing @ lists workspace files through it.
   * With none yet (a chat that has not been sent) the list stays absent.
   */
  sessionId?: string;
  /**
   * Conversation 1b: the queued rows have folded into this chip because the reader
   * is more than a viewport from the end. `count` is the real queue length.
   * `onOpen` returns to the bottom, which is what unfolds the rows.
   */
  queueChip?: { count: number; onOpen: () => void };
};

const START_PLACEHOLDER = 'Ask codeaf, or type @ to reference a file';

const { composerMinRows, composerMaxRows } = design.interaction;

function isComposing(event: KeyboardEvent) {
  return event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229;
}

/** Grows to the row cap, then scrolls; `faded` is true only once text has scrolled under the top. */
function useAutosize(ref: RefObject<HTMLTextAreaElement | null>, draft: string) {
  const [faded, setFaded] = useState(false);
  useLayoutEffect(() => {
    const field = ref.current;
    if (!field) return;
    field.rows = composerMinRows;
    const style = getComputedStyle(field);
    const padding = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
    const line = style.lineHeight === 'normal' ? field.clientHeight - padding : parseFloat(style.lineHeight);
    if (!line) return;
    // scrollHeight is rounded to whole pixels, so a lone line can read a hair over one.
    const lines = Math.round((field.scrollHeight - padding) / line);
    field.rows = Math.min(composerMaxRows, Math.max(composerMinRows, lines));
    setFaded(field.scrollTop > 0);
  }, [ref, draft]);
  return { faded, onScroll: () => setFaded((ref.current?.scrollTop ?? 0) > 0) };
}

export function Composer(props: ComposerProps) {
  const { draft, onDraft, onSend, onStop, running, docked, disabledReason, autoFocus } = props;
  const field = useRef<HTMLTextAreaElement>(null);
  const box = useRef<HTMLDivElement>(null);
  // The strip owns focus while its arrow keys select a conversation. Native autofocus
  // can run after that selection and take the keyboard away from the next tab key.
  useLayoutEffect(() => {
    if (!autoFocus || document.activeElement?.closest('[role="tab"]')) return;
    field.current?.focus();
  }, [autoFocus]);
  const autosize = useAutosize(field, draft);
  const focus = useKeyboardFocus();
  const [pastes, setPastes] = useState<string[]>([]);
  const [pasteError, setPasteError] = useState('');
  const selection = useRef({ start: 0, end: 0 });
  const [pasteCaret, setPasteCaret] = useState<number | null>(null);
  useLayoutEffect(() => {
    if (pasteCaret === null || !field.current) return;
    field.current.focus();
    field.current.setSelectionRange(pasteCaret, pasteCaret);
    setPasteCaret(null);
  }, [draft, pasteCaret]);
  const latestDraft = useRef(draft);
  latestDraft.current = draft;
  const disabled = !!disabledReason;
  const attachments = useAttachments();
  const picker = useRef<HTMLInputElement>(null);
  const drop = useFileDrop(attachments.add, !disabled);
  // A ref, not state: StrictMode replays effects and a remounted tray must not take the same offer twice.
  const offerTaken = useRef(false);
  useEffect(() => {
    if (offerTaken.current || !props.offeredFiles?.length) return;
    offerTaken.current = true;
    attachments.add(props.offeredFiles);
    props.onOfferedFiles?.();
  }, [props.offeredFiles]);
  // Kept in state so settling the offer (the parent drops the prop) does not take the chip with it.
  const [pageLink, setPageLink] = useState(props.offeredLink);
  const linkTaken = useRef(false);
  useEffect(() => {
    if (linkTaken.current || !pageLink) return;
    linkTaken.current = true;
    props.onOfferedLink?.();
  }, [pageLink]);
  const dropState = props.dropState ?? (drop.over ? 'over' : drop.pageDrag ? 'page' : undefined);
  const blank = draft.trim() === '' && attachments.items.length === 0 && pastes.length === 0;
  const atPicker = useAtPicker(field, props.sessionId, draft, onDraft, setPasteCaret);

  async function send(mode: SendMode) {
    if (blank || disabled) return;
    const { items } = attachments;
    const sent = draft;
    const text = encodePasted(pastes, draft.trim());
    const accepted = items.length ? await onSend(text, mode, await toOutgoing(items)) : await onSend(text, mode);
    if (!accepted) return;
    // Typing while the send was in flight must survive: clear only the text that went out.
    if (latestDraft.current === sent) {
      onDraft('');
      setPastes([]);
      attachments.clear();
      setPageLink(undefined);
    }
  }

  function onPaste(event: ClipboardEvent) {
    const files = Array.from(event.clipboardData.files);
    const text = event.clipboardData.getData('text/plain');
    if (files.length === 0 && !isLongPaste(text)) return;
    event.preventDefault();
    if (files.length) attachments.add(files);
    else setPastes(current => [...current, text]);
  }

  function rememberSelection(open: boolean) {
    if (!open || !field.current) return;
    // The menu takes focus, so preserve the selection before its items receive it.
    selection.current = { start: field.current.selectionStart, end: field.current.selectionEnd };
    setPasteError('');
  }

  async function pastePlainText() {
    const target = field.current;
    const before = latestDraft.current;
    const { start, end } = selection.current;
    try {
      const text = await navigator.clipboard.readText();
      // A delayed clipboard read must not replace newer typing or another pane's draft.
      if (!text || target !== field.current || !target || target.disabled || latestDraft.current !== before) return;
      setPasteCaret(start + text.length);
      onDraft(before.slice(0, start) + text + before.slice(end));
      setPasteError('');
    } catch {
      if (target === field.current && target) setPasteError('Clipboard text could not be read. Paste with your keyboard instead.');
    }
  }

  function onPicked(input: HTMLInputElement) {
    attachments.add(Array.from(input.files ?? []));
    input.value = '';
  }

  function recall(event: KeyboardEvent) {
    const last = props.recallLast?.();
    if (last === undefined) return;
    event.preventDefault();
    // A recalled paste comes back as its card, never as the tagged text it travelled as.
    const { pastes: recalled, rest } = splitPasted(last);
    setPastes(recalled.map((paste) => paste.text));
    onDraft(rest);
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (isComposing(event)) return;
    // The file list owns these keys while it is up: arrows move, Enter and Tab insert, Esc closes.
    if (atPicker.onKeyDown(event)) return;
    if (event.key === 'Escape') return event.currentTarget.blur();
    if (event.key === 'ArrowUp' && draft === '') return recall(event);
    if (event.key !== 'Enter' || event.shiftKey) return;
    if (event.altKey && !running) return;
    event.preventDefault();
    void send(event.altKey ? 'queue' : running ? 'steer' : 'submit');
  }

  const stopping = running && blank;
  const steering = running && !blank;
  const queueMenu = [{
    id: 'queue-instead', label: 'Queue instead', shortcut: composerShortcuts.queue,
    disabled: !running || draft.trim() === '' || disabled,
    onSelect: () => { void send('queue'); },
  }];

  return (
    <div className="composer-dock" data-docked={docked} data-variant={props.variant}>
      <div
        ref={box}
        className="composer"
        data-attached={attachments.items.length > 0 || pageLink ? true : undefined}
        data-running={running}
        data-steering={steering}
        data-disabled={disabled}
        data-drop={dropState}
        {...drop.handlers}
      >
        {dropState && <Text className="composer-drop-hint">Drop to attach</Text>}
        {pageLink && (
          <ul className="attachment-tray" aria-label="Attached page">
            <li className="attachment">
              <LinkChip href={pageLink.url} title={pageLink.title || undefined} />
            </li>
          </ul>
        )}
        <AttachmentTray items={attachments.items} onRemove={attachments.remove} />
        {pastes.map((text, index) => (
          <PasteCard
            key={index}
            lines={countLines(text)}
            text={text}
            onRemove={() => setPastes(current => current.filter((_, at) => at !== index))}
          />
        ))}
        <ContextMenu className="composer-menu" label="Message menu" onOpenChange={rememberSelection} items={[{
          id: 'paste-plain', label: 'Paste as plain text', disabled,
          onSelect: () => { void pastePlainText(); },
        }]}>
          <TextArea
            ref={field}
            className="composer-field"
            data-faded={autosize.faded || undefined}
            data-keyboard={focus.keyboard || undefined}
            onScroll={autosize.onScroll}
            aria-label="Message"
            aria-autocomplete={atPicker.open ? 'list' : undefined}
            aria-controls={atPicker.open ? atPicker.listId : undefined}
            aria-activedescendant={atPicker.open ? atPicker.activeId : undefined}
            placeholder={disabledReason ?? (running ? 'Steer, or queue a message' : props.placeholder ?? (docked ? 'Ask codeaf' : START_PLACEHOLDER))}
            value={draft}
            disabled={disabled}
            rows={composerMinRows}
            onChange={event => { onDraft(event.target.value); atPicker.sync(event.target); }}
            onSelect={event => atPicker.sync(event.currentTarget)}
            onKeyUp={event => atPicker.sync(event.currentTarget)}
            onClick={event => atPicker.sync(event.currentTarget)}
            onFocus={event => { focus.onFocus(); atPicker.sync(event.currentTarget); }}
            onBlur={() => { focus.onBlur(); atPicker.sync(null); }}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
          />
        </ContextMenu>
        <AtPicker
          anchor={box}
          files={atPicker.files}
          active={atPicker.active}
          listId={atPicker.listId}
          onHighlight={atPicker.highlight}
          onPick={atPicker.pick}
          onClose={atPicker.close}
        />
        {pasteError && <Text className="composer-error" role="status">{pasteError}</Text>}
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
            {props.variant !== 'home' && <IconButton
              className="composer-attach"
              label="Attach files"
              icon="plus"
              iconSize="sm"
              disabled={disabled}
              onClick={() => picker.current?.click()}
            />}
            {props.model ? (
              <ModelPicker models={props.model.models} selectedId={props.model.selectedId} onSelect={props.model.readOnly ? undefined : props.model.onSelect} effort={props.model.readOnly ? undefined : props.model.effort} pinnedCount={props.model.pinnedCount} hint={props.model.hint} />
            ) : props.modelLabel && (
              <ModelPicker models={[{ id: props.modelLabel, label: props.modelLabel, short: props.modelShort }]} selectedId={props.modelLabel} />
            )}
            {props.queueChip && props.queueChip.count > 0 && (
              <Button className="composer-queue-chip" onClick={props.queueChip.onOpen}>{`${props.queueChip.count} queued`}</Button>
            )}
            {props.tasksToggle && <Button className="composer-tasks" onClick={props.tasksToggle.onClick}>{props.tasksToggle.label}</Button>}
          </div>
          <div className="composer-actions">
            {stopping && (
              <ContextMenu className="composer-menu" label="Send menu" items={queueMenu}>
                <Button className="composer-primary composer-stop" aria-label="Stop" title="Stop" onClick={onStop}>
                  <span className="composer-stop-mark" aria-hidden="true" />
                </Button>
              </ContextMenu>
            )}
            {steering && (
              <>
                <Button className="composer-queue" onClick={() => void send('queue')}>Queue {composerShortcuts.queue}</Button>
                <ContextMenu className="composer-menu" label="Send menu" items={queueMenu}>
                  <Button className="composer-steer" disabled={disabled} onClick={() => void send('steer')}>
                    <Icon name="steer" size="xs" />
                    Steer
                  </Button>
                </ContextMenu>
              </>
            )}
            {!running && (
              <ContextMenu className="composer-menu" label="Send menu" items={queueMenu}>
                <IconButton
                  className="composer-primary composer-send"
                  label="Send"
                  icon="send"
                  iconSize="sm"
                  disabled={blank || disabled}
                  onClick={() => void send('submit')}
                />
              </ContextMenu>
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
