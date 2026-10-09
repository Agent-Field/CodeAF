import './work.css';
import { useState } from 'react';
import { Button, Icon, WorkStateIndicator } from '../../../components/ui';
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
      <div className="work-thinking" data-live="true">
        <span className="work-thinking-label">
          <WorkStateIndicator phase="streaming" label="Thinking" />
          Thinking
        </span>
        {text && <p className="work-thinking-text">{lastLines(text)}</p>}
      </div>
    );
  }
  if (!text && seconds === undefined) return null;
  const label = seconds === undefined ? 'Thought' : `Thought for ${spoken(seconds)}`;
  return (
    <div className="work-thinking">
      <Button className="work-step-head" aria-expanded={open} disabled={!text} onClick={() => setOpen(!open)}>
        <Icon name="thinking" size="sm" />
        <span className="work-step-title">{label}</span>
      </Button>
      {open && text && <p className="work-thinking-body">{text}</p>}
    </div>
  );
}
