import { useState } from 'react';
import type { EngineTaskRow } from '../../chat/engine-client';
import { TaskPanel } from '../TaskPanel';

/** A fixed clock so the specimen reads the same every time it is opened. */
export const SPECIMEN_NOW = Date.parse('2026-10-09T10:05:00Z');

const at = (secondsAgo: number) => new Date(SPECIMEN_NOW - secondsAgo * 1000).toISOString();

export const panelRows: EngineTaskRow[] = [
  { ID: 't1', Title: 'Audit the repository', Status: 'running', Started: at(260), Tokens: 41200, Total: 9 },
  { ID: 't2', Title: 'List files', Status: 'done', Parent: 't1', Started: at(250), Ended: at(238) },
  {
    ID: 't3',
    Title: 'Count words in every Markdown file',
    Status: 'running',
    Parent: 't1',
    Started: at(120),
    Live: { Step: 4, Command: 'cd /run/copy && wc -w docs/*.md', Since: at(12) },
    LiveParts: [
      { Command: 'cd /run/copy', Separator: '&&', Start: 0, End: 12, SepEnd: 15, RunCopyPrefix: true },
      { Command: 'wc -w docs/*.md', Start: 16, End: 31 },
    ],
  },
  { ID: 't3a', Title: 'Skip generated pages', Status: 'running', Parent: 't3', Started: at(40), Live: { Step: 2, Command: 'rg -l "generated" docs', Since: at(3) } },
  { ID: 't3b', Title: 'Sum the totals', Status: 'pending', Parent: 't3', Waits: ['t3a'] },
  { ID: 't4', Title: 'Check broken links', Status: 'failed', Parent: 't1', Started: at(200), Ended: at(150) },
  { ID: 't5', Title: 'Pick the changelog format', Status: 'paused', Parent: 't1' },
  { ID: 't6', Title: 'Hold the release notes', Status: 'running', Parent: 't1', Paused: true, Started: at(90) },
  { ID: 't7', Title: 'Write the summary', Status: 'pending', Parent: 't1', Waits: ['t3', 't2'] },
];

export function TaskPanelSpecimen() {
  const [current, setCurrent] = useState('t3');
  return (
    <div className="specimen-panel">
    <TaskPanel
      variant="column"
      tasks={panelRows}
      now={SPECIMEN_NOW}
      currentTaskId={current}
      onOpenTask={(id) => setCurrent(id)}
      onPause={() => undefined}
      onResume={() => undefined}
      onStop={() => undefined}
      onClose={() => undefined}
    />
    </div>
  );
}
