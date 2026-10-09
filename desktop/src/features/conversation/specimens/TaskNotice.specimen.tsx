import { useState } from 'react';
import { TaskNotice } from '../TaskNotice';
import type { TurnItem } from '../types';

type TaskItem = Extract<TurnItem, { kind: 'task' }>;

const fixtures: TaskItem[] = [
  {
    kind: 'task',
    id: 'n1',
    taskId: 't-1',
    title: 'List files',
    status: 'done',
    summary: 'Found 4 files in the repository root.',
    body: 'Listed the repository root.\nREADME.md, go.mod, go.sum, main.go',
  },
  {
    kind: 'task',
    id: 'n2',
    taskId: 't-2',
    title: 'Run the tests',
    status: 'failed',
    summary: 'Two tests failed in the parser package.',
    body: 'parser_test.go:41: expected 3 tokens, got 2\nparser_test.go:77: unexpected end of input',
  },
  {
    kind: 'task',
    id: 'n3',
    taskId: 't-3',
    title: 'Count words',
    status: 'running',
    summary: 'Reading README.md',
    body: '',
  },
];

export function TaskNoticeSpecimen() {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  return (
    <div>
      {fixtures.map((item) => (
        <TaskNotice
          key={item.id}
          item={item}
          open={Boolean(open[item.id])}
          onToggle={() => setOpen((prev) => ({ ...prev, [item.id]: !prev[item.id] }))}
          onOpenTask={() => undefined}
        />
      ))}
    </div>
  );
}
