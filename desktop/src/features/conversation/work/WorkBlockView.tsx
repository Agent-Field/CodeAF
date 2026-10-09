import './work.css';
import './workBlock.css';
import { Fragment, useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import type { WorkBlock, WorkStep } from '../types';
import { spoken, summaryParts } from './format';
import type { WorkRender } from './props';
import { ThinkingView } from './ThinkingView';
import { useNow, useWorkOpen } from './useNow';
import { WorkNotes } from './WorkNotes';
import { WorkStepView } from './WorkStepView';

// `open`/`onToggle` are optional: without them the block follows `live` on its own.
type Props = WorkRender & { block: WorkBlock; open?: boolean; onToggle?: () => void; now?: number };

const LIVE_STATES: WorkStep['state'][] = ['preparing', 'waiting'];

/** Steps unfold by default while one is forming or needs the person; a failed last step stays open.
 * A running step stays one row with its live command under it (design v3 Work block · Live). */
function openByDefault(step: WorkStep, last: boolean): boolean {
  return LIVE_STATES.includes(step.state) || (last && step.state === 'failed');
}

/** Live: "Working" and the running clock. Settled: only the parts that exist, set apart by a dot. */
function Summary({ block, now }: { block: WorkBlock; now: number }) {
  if (block.live) {
    const seconds = block.startedAt ? Math.floor((now - block.startedAt) / 1000) : undefined;
    return (
      <span className="work-summary-text">
        <span>Working</span>
        {seconds !== undefined && <span className="work-live-time">{spoken(seconds)}</span>}
      </span>
    );
  }
  return (
    <span className="work-summary-text">
      {summaryParts(block.summary).map((part, at) => (
        <Fragment key={at}>
          {at > 0 && <span className="work-summary-sep" aria-hidden="true">·</span>}
          <span data-tone={part.tone}>{part.text}</span>
        </Fragment>
      ))}
    </span>
  );
}

/** The summary as one spoken name: the visible dots are drawn apart, so the name carries its own. */
function summaryLabel(block: WorkBlock, now: number): string {
  if (!block.live) return summaryParts(block.summary).map((part) => part.text).join(' · ');
  const seconds = block.startedAt ? Math.floor((now - block.startedAt) / 1000) : undefined;
  return seconds === undefined ? 'Working' : `Working ${spoken(seconds)}`;
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
      <Button className="work-toggle" aria-expanded={isOpen} aria-label={summaryLabel(block, now)} onClick={onToggle ?? auto.toggle}>
        <Icon name="chevron" size="xs" motion="disclosure" />
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
