import { useState } from 'react';
import { Button, CodeText, Icon, WorkStateIndicator } from '../../components/ui';
import type { ToolStep } from './types';
import { argsRestateHint, prettyArgs, rowHint, toolIcon } from './tool-family';

type ReadFull = (callId: string) => Promise<{ output: string; full: boolean }>;
type Props = { step: ToolStep; open: boolean; onToggle: () => void; readFull?: ReadFull };

function StateMark({ state }: { state: ToolStep['state'] }) {
  if (state === 'running') return <WorkStateIndicator phase="working" label="Running" />;
  if (state === 'stopped') {
    return (
      <span className="tool-mark" role="img" aria-label="Stopped">
        <Icon name="cancelled" size="xs" />
      </span>
    );
  }
  if (state !== 'failed') return null;
  return (
    <span className="tool-mark tool-mark-failed" role="img" aria-label="Failed">
      <Icon name="failed" size="xs" />
    </span>
  );
}

function Section({ text }: { text: string }) {
  if (!text) return null;
  return (
    <pre className="tool-pre">
      <CodeText>{text}</CodeText>
    </pre>
  );
}

function useFullOutput(step: ToolStep, readFull?: ReadFull) {
  const [full, setFull] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const callId = step.callId;
  const canLoad = Boolean(callId && readFull && full === undefined);

  async function load() {
    if (!callId || !readFull) return;
    setLoading(true);
    setError('');
    try {
      setFull((await readFull(callId)).output);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'Could not read the full output.');
    } finally {
      setLoading(false);
    }
  }
  return { output: full ?? step.output, canLoad, loading, error, load };
}

/** The call's input: shown only when it says more than the hint, and behind a quiet toggle. */
function Input({ step }: { step: ToolStep }) {
  const [shown, setShown] = useState(false);
  if (argsRestateHint(step.args, step.hint)) return null;
  return (
    <>
      <Button className="tool-more" aria-expanded={shown} onClick={() => setShown(!shown)}>
        Input
      </Button>
      {shown && <Section text={prettyArgs(step.args)} />}
    </>
  );
}

function Details({ step, readFull }: { step: ToolStep; readFull?: ReadFull }) {
  const full = useFullOutput(step, readFull);
  return (
    <div className="tool-details">
      <Section text={full.output} />
      {full.error && (
        <p className="tool-error" role="status">
          {full.error}
        </p>
      )}
      {full.canLoad && (
        <Button className="tool-more" loading={full.loading} onClick={() => void full.load()}>
          {full.loading ? 'Loading…' : 'Show full output'}
        </Button>
      )}
      <Input step={step} />
    </div>
  );
}

export function ToolRow({ step, open, onToggle, readFull }: Props) {
  const label = rowHint(step.tool, step.hint);
  return (
    <div className="tool-row" data-state={step.state}>
      <Button className="tool-row-head" aria-expanded={open} onClick={onToggle}>
        <Icon name={toolIcon(step.tool)} size="sm" />
        <span className="tool-hint">{label}</span>
        <StateMark state={step.state} />
      </Button>
      {open && <Details step={step} readFull={readFull} />}
    </div>
  );
}
