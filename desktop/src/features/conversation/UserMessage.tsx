import { useLayoutEffect, useRef, useState } from 'react';
import { Button } from '../../components/ui';

/** True when the clamped text is taller than its visible box. */
function isClipped(element: HTMLElement): boolean {
  return element.scrollHeight > element.clientHeight;
}

export function UserMessage({ text }: { text: string }) {
  const body = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [overflowing, setOverflowing] = useState(false);

  useLayoutEffect(() => {
    const element = body.current;
    if (!element) return;
    if (!expanded) setOverflowing(isClipped(element));
  }, [text, expanded]);

  if (!text) return null;
  return (
    <div className="user-message">
      <div className="user-message-bubble">
        <div ref={body} className="user-message-text" data-clamped={!expanded}>
          {text}
        </div>
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
