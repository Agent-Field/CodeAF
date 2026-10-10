import { useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import { LogStep, type ReadFile } from './LogStep';
import { logSummary } from './logLines';
import type { LogLine } from './taskTypes';
import './task-log.css';

type Props = {
  steps: LogLine[];
  live?: LogLine;
  /** The task is over: the log folds to one line. */
  ended: boolean;
  now: number;
  readFile?: ReadFile;
};

/** The worker's commands as a terminal log. The header names the counts; the rows stay open while the task runs and fold once it is over. */
export function WorkLog({ steps, live, ended, now, readFile }: Props) {
  const [chosen, setChosen] = useState<boolean>();
  const summary = logSummary(steps, live);
  if (!summary) return null;
  // The header stays while the task runs, above the one live row. Folding is only the resting state of a finished log.
  const open = chosen ?? !ended;
  return (
    <section className="task-log" aria-label="Work log">
      <Button className="task-log-toggle" aria-expanded={open} onClick={() => setChosen(!open)}>
        <Icon name="chevron" size="xs" motion="disclosure" />
        <span>{summary}</span>
      </Button>
      {open && (
        <ol className="task-log-list">
          {[...steps, ...(live ? [live] : [])].map((line) => (
            <LogStep key={line.id} line={line} now={now} readFile={readFile} />
          ))}
        </ol>
      )}
    </section>
  );
}
