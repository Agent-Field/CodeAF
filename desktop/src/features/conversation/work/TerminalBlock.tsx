import { useState } from 'react';
import { Button, CodeText } from '../../../components/ui';
import type { ToolStep } from '../types';
import type { ReadFull } from './props';
import { argString, bashExit, parseArgs } from './stats';

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

/** Output on the terminal field; a footer carries the exit code and the full output on request. */
export function TerminalBlock({ call, readFull }: { call: ToolStep; readFull?: ReadFull }) {
  const full = useFullOutput(call, readFull);
  const command = argString(parseArgs(call.args), 'command').trim();
  const exit = bashExit(call.output);
  if (!command && !full.output) return null;
  return (
    <div className="work-excerpt">
      <div className="work-term">
        <pre className="work-term-body" aria-label="Terminal" tabIndex={0}>
          {command && <CodeText className="work-term-command">{`$ ${command}`}</CodeText>}
          {full.output && <CodeText>{command ? `\n${full.output}` : full.output}</CodeText>}
        </pre>
        {(exit !== undefined || full.canLoad) && (
          <div className="work-term-foot">
            {exit !== undefined && <span>{`exit ${exit}`}</span>}
            {full.canLoad && (
              <Button className="work-more" loading={full.loading} onClick={() => void full.load()}>
                {full.loading ? 'Loading…' : 'Show full output'}
              </Button>
            )}
          </div>
        )}
      </div>
      {full.error && <p className="work-note" data-tone="warning" role="status">{full.error}</p>}
    </div>
  );
}
