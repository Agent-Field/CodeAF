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

/** The worker's commands as a terminal log. While the task runs the rows stand alone; once it is over they fold under one summary line. */
export function WorkLog({ steps, live, ended, now, readFile }: Props) {
  const [chosen, setChosen] = useState<boolean>();
  const summary = logSummary(steps, live, ended);
  if (!summary) return null;
  const open = chosen ?? !ended;
  return (
    <section className="task-log" aria-label="Work log">
      {ended && (
        <Button className="task-log-toggle" aria-expanded={open} onClick={() => setChosen(!open)}>
          <Icon name="chevron" size="xs" motion="disclosure" />
          <span>{summary}</span>
        </Button>
      )}
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
