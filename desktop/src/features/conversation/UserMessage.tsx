import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Button, Markdown } from '../../components/ui';
import { PasteCard } from './composer/PasteCard';
import { splitPasted } from './composer/pastedText';

/** True when the clamped text is taller than its visible box. */
function isClipped(element: HTMLElement): boolean {
  return element.scrollHeight > element.clientHeight;
}

type UserMessageProps = { text: string; markdown?: boolean; attachments?: ReactNode };

export function UserMessage({ text: message, markdown = false, attachments }: UserMessageProps) {
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
    <div className="user-message">
      <div className="user-message-bubble" data-pasted={pastes.length > 0 || undefined}>
        {attachments}
        {pastes.map((paste, index) => (
          <PasteCard key={index} variant="sent" lines={paste.lines} text={paste.text} />
        ))}
        {text && (
          <div ref={body} className="user-message-text" data-clamped={!expanded} data-markdown={markdown || undefined}>
            {markdown ? <Markdown>{text}</Markdown> : text}
          </div>
        )}
        {overflowing && (
          <Button
            className="user-message-toggle"
            aria-expanded={expanded}
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? 'Show less' : 'Show more'}
          </Button>
        )}
      </div>
    </div>
  );
}
