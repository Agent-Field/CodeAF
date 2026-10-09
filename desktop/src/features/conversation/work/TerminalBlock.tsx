import { useState } from 'react';
import { Button, CodeText } from '../../../components/ui';
import type { ToolStep } from '../types';
import type { ReadFull } from './props';
import { argString, parseArgs } from './stats';

function useFullOutput(call: ToolStep, readFull?: ReadFull) {
  const [full, setFull] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const callId = call.callId;

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
  return { output: full ?? call.output, canLoad: Boolean(callId && readFull && full === undefined), loading, error, load };
}

/** `$ command`, its output on the field background, the exit mark, and the full output on request. */
export function TerminalBlock({ call, readFull }: { call: ToolStep; readFull?: ReadFull }) {
  const full = useFullOutput(call, readFull);
  const command = argString(parseArgs(call.args), 'command');
  if (!command && !full.output) return null;
  return (
    <div className="work-excerpt">
      <pre className="work-pre work-terminal" aria-label="Terminal">
        {command && <CodeText className="work-terminal-command">{`$ ${command}`}</CodeText>}
        {full.output && <CodeText>{`\n${full.output}`}</CodeText>}
      </pre>
      {full.error && <p className="work-note" data-tone="warning" role="status">{full.error}</p>}
      {full.canLoad && (
        <Button className="work-more" loading={full.loading} onClick={() => void full.load()}>
          {full.loading ? 'Loading…' : 'Show full output'}
        </Button>
      )}
    </div>
  );
}
