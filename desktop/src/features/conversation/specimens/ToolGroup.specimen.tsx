import { useState } from 'react';
import { ThinkingItem } from '../ThinkingItem';
import { ToolGroup } from '../ToolGroup';
import type { ToolStep, TurnItem } from '../types';
import '../tools.css';

type Group = Extract<TurnItem, { kind: 'tools' }>;
type Thinking = Extract<TurnItem, { kind: 'thinking' }>;

const step = (id: string, tool: string, hint: string, state: ToolStep['state'], output = ''): ToolStep => ({
  id,
  callId: id,
  tool,
  hint,
  args: `{"command":"${hint}"}`,
  output,
  state,
});

const running: Group = {
  kind: 'tools',
  id: 'running',
  steps: [step('r1', 'read', 'Read package.json', 'done', '{ "name": "codeaf" }'), step('r2', 'bash', 'npm test', 'running')],
};
const done: Group = {
  kind: 'tools',
  id: 'done',
  steps: [
    step('d1', 'grep', 'Search for readToolResult', 'done', 'engine-client.ts:100'),
    step('d2', 'read', 'Read engine-client.ts', 'done', 'export async function readToolResult'),
    step('d3', 'edit', 'Edit ToolGroup.tsx', 'done'),
    step('d4', 'bash', 'npm run build', 'failed', 'error TS2322: Type string is not assignable'),
  ],
};
const thought = (id: string, streaming: boolean): Thinking => ({
  kind: 'thinking',
  id,
  streaming,
  text: 'The grouping should stay quiet.\nExpanded rows carry the detail.',
});

async function readFull(): Promise<{ output: string; full: boolean }> {
  await new Promise((resolve) => setTimeout(resolve, 600));
  return { output: 'error TS2322: Type string is not assignable\n  at ToolRow.tsx:12\n  at build.ts:40', full: true };
}

export function ToolGroupSpecimen() {
  const [open, setOpen] = useState<Record<string, boolean>>({ done: true, 'think-open': true });
  const toggle = (id: string) => () => setOpen((value) => ({ ...value, [id]: !value[id] }));
  return (
    <div className="specimen-stack">
      <ToolGroup item={running} open={Boolean(open.running)} onToggle={toggle('running')} readFull={readFull} />
      <ToolGroup item={done} open={Boolean(open.done)} onToggle={toggle('done')} readFull={readFull} />
      <ThinkingItem item={thought('think-closed', false)} open={Boolean(open['think-closed'])} onToggle={toggle('think-closed')} />
      <ThinkingItem item={thought('think-open', false)} open={Boolean(open['think-open'])} onToggle={toggle('think-open')} />
      <ThinkingItem item={thought('think-live', true)} open={false} onToggle={toggle('think-live')} />
    </div>
  );
}
