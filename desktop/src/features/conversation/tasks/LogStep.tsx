import { useState, type ReactNode } from 'react';
import { Button, CodeText } from '../../../components/ui';
import { sinceMs, spanText } from './taskClock';
import type { LogLine } from './taskTypes';

export type ReadFile = (path: string) => Promise<string>;

type Props = { line: LogLine; now: number; readFile?: ReadFile };

/** A state dot with a spoken name; colour only reinforces it. */
function Dot({ label }: { label: string }) {
  return <span className="task-log-dot" role="img" aria-label={label} />;
}

/** The right edge of a line: how it ended, or how long it has been running. */
function Outcome({ line, now }: Pick<Props, 'line' | 'now'>) {
  if (line.state === 'refused') return <span className="task-log-outcome" data-state="refused">refused</span>;
  if (line.state === 'running') {
    const clock = spanText(sinceMs(line.since, now));
    return (
      <span className="task-log-outcome" data-state="running">
        <Dot label="Running" />
        {clock && <span>{clock}</span>}
      </span>
    );
  }
  return (
    <span className="task-log-outcome" data-state={line.state}>
      {line.state === 'failed' && <Dot label="Failed" />}
      {line.took && <span>{line.took}</span>}
    </span>
  );
}

function useFullOutput(line: LogLine, readFile?: ReadFile) {
  const [full, setFull] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const path = line.fullOutput;
  async function load() {
    if (!path || !readFile) return;
    setLoading(true);
    setError('');
    try {
      setFull(await readFile(path));
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : 'Could not read the full output.');
    } finally {
      setLoading(false);
    }
  }
  return { text: full ?? line.observation, canLoad: Boolean(path && readFile && full === undefined), loading, error, load };
}

function Observation({ line, readFile }: { line: LogLine; readFile?: ReadFile }) {
  const full = useFullOutput(line, readFile);
  return (
    <div className="task-log-detail">
      {full.text && (
        <pre className="task-log-output">
          <CodeText>{full.text}</CodeText>
        </pre>
      )}
      {full.error && <p className="task-log-error" role="status">{full.error}</p>}
      {full.canLoad && (
        <Button className="task-log-more" loading={full.loading} onClick={() => void full.load()}>
          {full.loading ? 'Loading…' : 'Show full output'}
        </Button>
      )}
    </div>
  );
}

function Head({ line, now }: Pick<Props, 'line' | 'now'>): ReactNode {
  return (
    <>
      <CodeText className="task-log-command">
        <span className="task-log-prompt" aria-hidden="true">$ </span>
        <span className="task-log-text">{line.command}</span>
      </CodeText>
      <Outcome line={line} now={now} />
    </>
  );
}

/** One command, one line. A finished line with output opens to its observation. */
export function LogStep({ line, now, readFile }: Props) {
  const [open, setOpen] = useState(false);
  const expandable = line.state !== 'running' && line.state !== 'refused' && Boolean(line.observation || line.fullOutput);
  return (
    <li className="task-log-step" data-state={line.state}>
      {expandable ? (
        <Button className="task-log-line" aria-expanded={open} onClick={() => setOpen(!open)}>
          <Head line={line} now={now} />
        </Button>
      ) : (
        <div className="task-log-line">
          <Head line={line} now={now} />
        </div>
      )}
      {open && expandable && <Observation line={line} readFile={readFile} />}
    </li>
  );
}
