import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Button, ContextMenu, CopyButton, Markdown, RowActions } from '../../components/ui';
import './user-message.css';
import { PasteCard } from './composer/PasteCard';
import { splitPasted } from './composer/pastedText';
import { messageMenu } from './menus/messageMenu';

/** True when the clamped text is taller than its visible box. */
function isClipped(element: HTMLElement): boolean {
  return element.scrollHeight > element.clientHeight;
}

type UserMessageProps = { text: string; markdown?: boolean; attachments?: ReactNode; /** Written, not yet recorded by the engine: drawn at 60% until it is. */ sending?: boolean };

/** The person's message: soft bubble, clamped when long, hover actions below. */
export function UserMessage({ text: message, markdown = false, attachments, sending = false }: UserMessageProps) {
  const { pastes, rest: text } = splitPasted(message);
  const body = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflowing, setOverflowing] = useState(false);

  useLayoutEffect(() => {
    const element = body.current;
    if (!element) return;
    if (!expanded) setOverflowing(isClipped(element));
  }, [text, expanded]);

  if (!text && !attachments && pastes.length === 0) return null;
  return (
    <div className="user-message" data-actions-host="" data-sending={sending || undefined}>
      <ContextMenu items={messageMenu(text)} label="Message actions">
        <div className="user-message-bubble" tabIndex={0} aria-label="User message" data-attached={Boolean(attachments) || undefined} data-pasted={pastes.length > 0 || undefined}>
          {attachments}
          {pastes.map((paste, index) => (
            <PasteCard key={index} variant="sent" lines={paste.lines} text={paste.text} />
          ))}
          {text && (
            <div ref={body} className="user-message-text" data-clamped={!expanded} data-faded={(!expanded && overflowing) || undefined} data-markdown={markdown || undefined}>
              {markdown ? <Markdown>{text}</Markdown> : text}
            </div>
          )}
          {overflowing && (
            <Button className="user-message-toggle" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
              {expanded ? 'Show less' : 'Show more'}
            </Button>
          )}
        </div>
      </ContextMenu>
      {text && !sending && (
        <RowActions className="user-message-actions">
          <CopyButton text={text} label="Copy message" size="message" iconSize="xs" />
        </RowActions>
      )}
    </div>
  );
}
