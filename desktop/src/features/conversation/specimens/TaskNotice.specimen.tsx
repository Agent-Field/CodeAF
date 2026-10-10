import { useState } from 'react';
import type { EngineTaskRow } from '../../chat/engine-client';
import { TaskNotice } from '../TaskNotice';
import type { TurnItem } from '../types';

type TaskItem = Extract<TurnItem, { kind: 'task' }>;

const fixtures: TaskItem[] = [
  {
    kind: 'task',
    id: 'n1',
    taskId: 't-1',
    title: 'Benchmark the tokenizer',
    status: 'done',
    summary: '8% faster on large arrays, no change on small',
    body: 'Ran the tokenizer benchmark on both array sizes.',
  },
  { kind: 'task', id: 'n2', taskId: 't-2', title: 'Update fixtures', status: 'running', summary: 'step 7', body: '' },
  { kind: 'task', id: 'n3', taskId: 't-3', title: 'Decide strict-mode default', status: 'paused', summary: 'Your call', body: '' },
];

const LIVE: Record<string, string> = { n2: '$ go test ./internal/parse/... · 12s' };

// The menu reads these the way a conversation's plan rows are read. The running and paused
// notices are children, so Pause and Resume are available; the finished one is not.
const rows: EngineTaskRow[] = [
  { ID: 't-1', Title: 'Benchmark the tokenizer', Status: 'done' },
  { ID: 't-2', Title: 'Update fixtures', Status: 'running', Parent: 'ship' },
  { ID: 't-3', Title: 'Decide strict-mode default', Status: 'paused', Parent: 'ship' },
];

export function TaskNoticeSpecimen() {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  return (
    <div className="task-notice-specimen">
      {fixtures.map((item) => (
        <TaskNotice
          key={item.id}
          item={item}
          live={LIVE[item.id]}
          open={Boolean(open[item.id])}
          onToggle={() => setOpen((prev) => ({ ...prev, [item.id]: !prev[item.id] }))}
          onOpenTask={() => undefined}
          tasks={rows}
          onPause={() => undefined}
          onResume={() => undefined}
          onStop={() => undefined}
        />
      ))}
    </div>
  );
}
