import { Icon, WorkStateIndicator, type IconName } from '../../../components/ui';
import type { RowState } from './props';

type Mark = { icon?: IconName; phase?: 'working' | 'waiting'; word: string; tone?: 'warning' };

const marks: Partial<Record<RowState, Mark>> = {
  forming: { word: 'Preparing…' },
  waiting: { phase: 'waiting', word: 'Waiting on you' },
  running: { phase: 'working', word: 'Running' },
  failed: { icon: 'failed', word: 'Failed', tone: 'warning' },
  stopped: { icon: 'cancelled', word: 'Stopped' },
  refused: { icon: 'cancelled', word: 'Refused' },
};

/** One still mark plus words. Running shows the mark only; its elapsed time is the motion. */
export function StateMark({ state }: { state: RowState }) {
  const mark = marks[state];
  if (!mark) return null;
  const showWord = state !== 'running';
  return (
    <span className="work-mark" data-tone={mark.tone}>
      {mark.phase && <WorkStateIndicator phase={mark.phase} label={mark.word} />}
      {mark.icon && (
        <span role="img" aria-label={mark.word}>
          <Icon name={mark.icon} size="xs" />
        </span>
      )}
      {showWord && <span className="work-mark-word">{mark.word}</span>}
    </span>
  );
}
