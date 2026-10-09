import './thinking.css';
import { useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import { spoken } from './format';

type Props = { text: string; streaming: boolean; seconds?: number };

const LIVE_LINES = 3;

function lastLines(text: string): string {
  return text.split('\n').filter((line) => line.trim() !== '').slice(-LIVE_LINES).join('\n');
}

/** Live: the last three lines, fading in from the top. Settled: "Thought for 6s", open to read it. */
export function ThinkingView({ text, streaming, seconds }: Props) {
  const [open, setOpen] = useState(false);
  if (streaming) {
    return (
      <div className="thinking" data-live="true">
        <div className="thinking-live">
          <span className="thinking-mark" role="img" aria-label="Thinking" />
          <span className="thinking-shimmer">Thinking</span>
        </div>
        {text && <p className="thinking-text">{lastLines(text)}</p>}
      </div>
    );
  }
  if (!text && seconds === undefined) return null;
  const label = seconds === undefined ? 'Thought' : `Thought for ${spoken(seconds)}`;
  return (
    <div className="thinking">
      <Button className="thinking-row" aria-expanded={open} disabled={!text} onClick={() => setOpen(!open)}>
        <Icon name="chevronRight" size="xs" />
        {label}
      </Button>
      {open && text && <p className="thinking-body">{text}</p>}
    </div>
  );
}
