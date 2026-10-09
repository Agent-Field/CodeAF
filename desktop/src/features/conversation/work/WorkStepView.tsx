import './work-step.css';
import { useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import type { WorkStep } from '../types';
import { duration, elapsed } from './format';
import type { RowState, WorkRender } from './props';
import { StepLead, StepTail } from './StepMark';
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

const UNSETTLED: RowState[] = ['forming', 'running', 'waiting'];
const isSettled = (state: RowState) => !UNSETTLED.includes(state);

/** One batch of parallel calls: chevron, category glyph (a still mark while live), title, then state and time on the right. */
export function WorkStepView({ step, open, onToggle, now, ...render }: Props) {
  const [openCalls, setOpenCalls] = useState<Record<string, boolean>>({});
  const toggleCall = (id: string) => setOpenCalls((value) => ({ ...value, [id]: !value[id] }));
  const state = stepState[step.state];
  const time = stepTime(step, now);
  return (
    <div className="work-step" data-state={state}>
      <Button className="work-step-head" aria-expanded={open} onClick={onToggle}>
        <Icon name="chevron" size="xs" motion="disclosure" />
        <StepLead state={state} />
        {isSettled(state) && <Icon name={categoryIcon(step.category)} size="xs" />}
        <span className="work-step-title">{step.title}</span>
        <StepTail state={state} time={time} decision={step.calls.length === 1 ? step.calls[0].decision : undefined} />
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
