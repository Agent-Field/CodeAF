import './work.css';
import './tool-call.css';
import { Button, CodeText, Icon, type IconName } from '../../../components/ui';
import type { ToolStep } from '../types';
import { toolIcon } from '../tool-family';
import { CallDetail } from './CallDetail';
import { elapsed, duration } from './format';
import type { RowState, WorkRender } from './props';
import { callStat, callTarget, targetText, type CallStat, type Target } from './target';

type Props = WorkRender & {
  call: ToolStep;
  open: boolean;
  onToggle: () => void;
  phase?: 'forming' | 'waiting' | 'refused'; // what the step knows that the call alone does not
  now?: number;
};

function StatText({ stat }: { stat: CallStat }) {
  if (stat.added === undefined && !stat.text) return null;
  return (
    <span className="work-stat">
      {stat.added !== undefined && <span className="work-add">{`+${stat.added}${stat.capped ? '+' : ''}`}</span>}
      {stat.removed !== undefined && <span className="work-remove">{`−${stat.removed}`}</span>}
      {stat.text && <span>{stat.text}</span>}
    </span>
  );
}

function TargetView({ target, call, stat, render, state }: { target: Target; call: ToolStep; stat: CallStat; render: WorkRender; state: RowState }) {
  if (state === 'forming') return <span className="work-call-text">Preparing…</span>;
  if (target.kind === 'file') {
    const chip = render.renderFile?.(target.path, stat.removed === undefined && stat.added === undefined ? undefined : stat);
    return <span className="work-call-file">{chip ?? <CodeText className="work-path">{target.path}</CodeText>}</span>;
  }
  if (target.kind === 'link' && render.renderLink) return <span className="work-call-file">{render.renderLink(target.url)}</span>;
  const mono = target.kind === 'command' || target.kind === 'link';
  const text = target.kind === 'command' ? `$ ${target.text}` : targetText(target);
  return <span className="work-call-text" data-mono={mono || undefined} title={call.hint || undefined}>{text}</span>;
}

/** Done keeps the tool's own icon; waiting and running swap it for a dot, the rest for their mark. */
const markIcons: Partial<Record<RowState, IconName>> = { failed: 'triangleAlert', stopped: 'cancelled', refused: 'cancelled' };

function Lead({ call, state }: { call: ToolStep; state: RowState }) {
  if (state === 'waiting' || state === 'running') return <span className="work-call-dot" data-state={state} aria-hidden="true" />;
  return <Icon name={markIcons[state] ?? toolIcon(call.tool)} size="xs" />;
}

function timeOf(call: ToolStep, state: RowState, now?: number): string | undefined {
  if (state === 'running') return elapsed(call.startedAt, now ?? Date.now());
  return call.tookMs ? duration(call.tookMs) : undefined;
}

const words: Partial<Record<RowState, string>> = { waiting: 'waiting on you', failed: 'Failed', stopped: 'Stopped', refused: 'refused' };

/** The trailing column: a word where the state needs one, then the time. A done call is only its time. */
function Status({ state, time, decision }: { state: RowState; time?: string; decision?: string }) {
  const word = words[state] ?? (state === 'done' ? decision : undefined);
  const shown = state === 'waiting' || state === 'refused' || state === 'stopped' ? undefined : time;
  if (!word && !shown) return null;
  return (
    <span className="work-call-status" data-state={state}>
      {word}
      {word && shown && ' · '}
      {shown && <span className="work-call-time">{shown}</span>}
    </span>
  );
}

/** One tool call: target, stat, state and time on a line; the detail unfolds below. */
export function ToolCallRow({ call, open, onToggle, phase, now, ...render }: Props) {
  const state: RowState = phase ?? call.state;
  const target = callTarget(call);
  const stat = callStat(call);
  const time = timeOf(call, state, now);
  return (
    <div className="work-call" data-state={state}>
      <div className="work-call-head">
        <Button className="work-call-toggle" aria-expanded={open} aria-label={`${call.tool.replace(/_/g, ' ')} ${targetText(target)}`} onClick={onToggle}>
          <Lead call={call} state={state} />
        </Button>
        <span className="work-call-main">
          <TargetView target={target} call={call} stat={stat} render={render} state={state} />
          {call.tool !== 'bash' && <StatText stat={stat} />}
        </span>
        <Status state={state} time={time} decision={call.decision} />
      </div>
      {open && state !== 'forming' && <CallDetail call={call} {...render} />}
    </div>
  );
}
