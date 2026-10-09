import { useState } from 'react';
import type { EngineTaskRow } from '../../chat/engine-client';
import { TaskPanel } from '../TaskPanel';

const rows: EngineTaskRow[] = [
  { ID: 't1', Title: 'Audit the repository', Status: 'running' },
  { ID: 't2', Title: 'List files', Status: 'done', Parent: 't1' },
  { ID: 't3', Title: 'Count words in every Markdown file', Status: 'running', Parent: 't1', Live: { Step: 4, Command: 'wc -w docs/*.md' } },
  { ID: 't4', Title: 'Check broken links', Status: 'failed', Parent: 't1' },
  { ID: 't5', Title: 'Rewrite the changelog', Status: 'cancelled', Stopped: true },
  { ID: 't6', Title: 'Write the summary', Status: 'pending' },
];

export function TaskPanelSpecimen() {
  const [current, setCurrent] = useState('t3');
  return (
    <TaskPanel
      variant="column"
      tasks={rows}
      currentTaskId={current}
      onOpenTask={(id) => setCurrent(id)}
      onClose={() => undefined}
    />
  );
}
