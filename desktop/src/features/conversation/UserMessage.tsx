import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Button, CopyButton, Markdown } from '../../components/ui';
import './user-message.css';

/** True when the clamped text is taller than its visible box. */
function isClipped(element: HTMLElement): boolean {
  return element.scrollHeight > element.clientHeight;
}

type UserMessageProps = { text: string; markdown?: boolean; attachments?: ReactNode };

/** The person's message: soft bubble, clamped when long, hover actions below. */
export function UserMessage({ text, markdown = false, attachments }: UserMessageProps) {
  const body = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflowing, setOverflowing] = useState(false);

  useLayoutEffect(() => {
    const element = body.current;
    if (!element) return;
    if (!expanded) setOverflowing(isClipped(element));
  }, [text, expanded]);

  if (!text && !attachments) return null;
  return (
    <div className="user-message">
      <div className="user-message-bubble" data-attached={Boolean(attachments) || undefined}>
        {attachments}
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
      {text && (
        <div className="user-message-actions">
          <CopyButton text={text} label="Copy message" />
        </div>
      )}
    </div>
  );
}
