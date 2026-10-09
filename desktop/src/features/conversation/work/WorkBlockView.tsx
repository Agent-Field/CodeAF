import './work.css';
import { useState } from 'react';
import { Button, Icon, WorkStateIndicator } from '../../../components/ui';
import type { WorkBlock, WorkStep } from '../types';
import { summaryParts } from './format';
import type { WorkRender } from './props';
import { ThinkingView } from './ThinkingView';
import { useNow, useWorkOpen } from './useNow';
import { WorkNotes } from './WorkNotes';
import { WorkStepView } from './WorkStepView';

// `open`/`onToggle` are optional: without them the block follows `live` on its own.
type Props = WorkRender & { block: WorkBlock; open?: boolean; onToggle?: () => void; now?: number };

const LIVE_STATES: WorkStep['state'][] = ['preparing', 'running', 'waiting'];

/** Steps unfold by default while they are the live edge or need the person; a failed last step stays open. */
function openByDefault(step: WorkStep, last: boolean): boolean {
  return LIVE_STATES.includes(step.state) || (last && step.state === 'failed');
}

function Summary({ block, now }: { block: WorkBlock; now: number }) {
  const seconds = block.live && block.startedAt ? Math.floor((now - block.startedAt) / 1000) : block.summary.seconds;
  const parts = summaryParts({ ...block.summary, seconds }, block.live);
  return (
    <span className="work-summary-text">
      {parts.map((part, at) => (
        <span key={at} data-tone={part.tone}>
          {at > 0 && ' · '}
          {part.text}
        </span>
      ))}
    </span>
  );
}

/** Everything between two conversation items, in the quieter work rhythm. */
export function WorkBlockView({ block, open, onToggle, now: given, ...render }: Props) {
  const auto = useWorkOpen(block.live);
  const now = useNow(block.live, given);
  const [stepOpen, setStepOpen] = useState<Record<string, boolean>>({});
  const isOpen = open ?? auto.open;
  const thinking = block.thinking;
  if (block.steps.length === 0 && !thinking && block.notes.length === 0) return null;
  const thought = thinking && <ThinkingView text={thinking.text} streaming={thinking.streaming} seconds={thinking.seconds} />;
  return (
    <div className="work-block" data-live={block.live || undefined}>
      <Button className="work-toggle" aria-expanded={isOpen} onClick={onToggle ?? auto.toggle}>
        <Icon name="chevron" size="xs" motion="disclosure" />
        {block.live && <WorkStateIndicator phase="working" label="Working" />}
        <Summary block={block} now={now} />
      </Button>
      {isOpen && (
        <div className="work-body">
          {thinking && !thinking.streaming && thought}
          {block.steps.map((step, at) => {
            const stepIsOpen = stepOpen[step.id] ?? openByDefault(step, at === block.steps.length - 1);
            return (
              <WorkStepView
                key={step.id}
                step={step}
                open={stepIsOpen}
                onToggle={() => setStepOpen((value) => ({ ...value, [step.id]: !stepIsOpen }))}
                now={now}
                {...render}
              />
            );
          })}
          <WorkNotes notes={block.notes} />
          {thinking?.streaming && thought}
        </div>
      )}
    </div>
  );
}
