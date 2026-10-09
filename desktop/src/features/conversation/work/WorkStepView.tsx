import { useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import type { WorkStep } from '../types';
import { duration, elapsed } from './format';
import { StateMark } from './Marks';
import type { RowState, WorkRender } from './props';
import { categoryIcon } from './target';
import { ToolCallRow } from './ToolCallRow';

type Props = WorkRender & { step: WorkStep; open: boolean; onToggle: () => void; now?: number };

const stepState: Record<WorkStep['state'], RowState> = {
  preparing: 'forming',
  running: 'running',
  waiting: 'waiting',
  done: 'done',
  failed: 'failed',
  stopped: 'stopped',
};

function stepTime(step: WorkStep, now?: number): string | undefined {
  if (step.state === 'running') return elapsed(step.calls.find((call) => call.startedAt)?.startedAt, now ?? Date.now());
  return step.tookMs ? duration(step.tookMs) : undefined;
}

/** A call inherits the step's "not yet" state only while the call itself is still in flight. */
function phaseOf(step: WorkStep, call: WorkStep['calls'][number]): 'forming' | 'waiting' | undefined {
  if (call.state !== 'running') return undefined;
  if (step.state === 'preparing') return 'forming';
  return step.state === 'waiting' ? 'waiting' : undefined;
}

/** One batch of parallel calls: category icon, title, time, state; unfolds to its calls. */
export function WorkStepView({ step, open, onToggle, now, ...render }: Props) {
  const [openCalls, setOpenCalls] = useState<Record<string, boolean>>({});
  const toggleCall = (id: string) => setOpenCalls((value) => ({ ...value, [id]: !value[id] }));
  const state = stepState[step.state];
  const time = stepTime(step, now);
  return (
    <div className="work-step" data-state={state}>
      <Button className="work-step-head" aria-expanded={open} onClick={onToggle}>
        <Icon name={categoryIcon(step.category)} size="sm" />
        <span className="work-step-title">{step.title}</span>
        <StateMark state={state} />
        {time && <span className="work-time">{time}</span>}
      </Button>
      {open && (
        <div className="work-calls">
          {step.calls.map((call) => (
            <ToolCallRow
              key={call.id}
              call={call}
              phase={phaseOf(step, call)}
              open={Boolean(openCalls[call.id])}
              onToggle={() => toggleCall(call.id)}
              now={now}
              {...render}
            />
          ))}
        </div>
      )}
    </div>
  );
}
