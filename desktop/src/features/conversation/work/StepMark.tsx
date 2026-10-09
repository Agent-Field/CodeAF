import { Icon } from '../../../components/ui';
import type { RowState } from './props';

const words: Partial<Record<RowState, string>> = {
  forming: 'preparing…',
  waiting: 'waiting on you',
  failed: 'failed',
  stopped: 'stopped',
  refused: 'refused',
};

/** The mark that takes the category glyph's place while a step is not settled: a still dot or ring, never motion. */
export function StepLead({ state }: { state: RowState }) {
  if (state === 'running' || state === 'waiting') {
    return <span className="work-step-dot" data-mark={state} role="img" aria-label={state === 'running' ? 'Running' : 'Waiting on you'} />;
  }
  if (state === 'forming') return <span className="work-step-ring" role="img" aria-label="Preparing" />;
  return null;
}

/** Right edge of a step: the words that need saying, then the duration. Failed reads "failed · 4.1s" as one phrase. */
export function StepTail({ state, time, decision }: { state: RowState; time?: string; decision?: string }) {
  const word = words[state] ?? (state === 'done' ? decision : undefined);
  const failed = state === 'failed';
  return (
    <span className="work-step-tail" data-state={state}>
      {failed && <span className="work-step-dot" data-mark="failed" role="img" aria-label="Failed" />}
      {state === 'stopped' && <Icon name="cancelled" size="xs" />}
      {word && <span className="work-step-word">{failed && time ? `${word} · ${time}` : word}</span>}
      {time && !failed && <span className="work-time">{time}</span>}
    </span>
  );
}
