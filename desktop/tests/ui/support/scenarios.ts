import type { EngineEntry, EngineQuestion, EngineTaskPage, EngineTaskRow } from '../../../src/features/chat/engine-client';
import type { Scenario } from './mock-engine';

// Entry shapes follow the Go wire format (session.DisplayEntry, PlanTaskRow, PlanTaskPage).
// DisplayEntry also carries TaskIDs on asides; the client type does not name it yet.
type WireEntry = EngineEntry & { TaskIDs?: string[] };

const user = (Text: string): WireEntry => ({ Role: 'user', Text });
const answer = (Text: string): WireEntry => ({ Role: 'assistant', Text, Answer: true });

const MARKDOWN_REPLY = [
  'Here is the short version.',
  '',
  '```ts',
  'export const add = (a: number, b: number) => a + b;',
  '```',
  '',
  '| Name | Value |',
  '| --- | --- |',
  '| alpha | 1 |',
  '| beta | 2 |',
].join('\n');

/** user -> markdown answer with a code block and a table. Reattaching shows the finished exchange. */
export function plainReply(prompt = 'Explain the add helper'): Scenario {
  return {
    initial: {
      title: 'Add helper',
      entries: [user(prompt), answer(MARKDOWN_REPLY)],
      usage: { Input: 120, Output: 80, CostUSD: 0.002, Duration: 3, Turns: 1 },
    },
    turns: [{ entries: [answer(MARKDOWN_REPLY)] }],
  };
}

const LONG_OUTPUT = Array.from({ length: 60 }, (_, i) => `line ${i + 1}: build output`).join('\n');

const call = (CallID: string, Tool: string, Hint: string, Args: string, Output: string): WireEntry => ({
  Role: 'tool', Text: '', Tool, Hint, CallID, Answered: true, Args, Output,
});

/** Three tool calls paired by CallID; call-3 has long inline output and a full read at tools/call-3. */
export function toolsReply(prompt = 'Check the build'): Scenario {
  const reply: WireEntry[] = [
    call('call-1', 'read', 'package.json', '{"path":"package.json"}', '{"name":"demo"}'),
    call('call-2', 'findFiles', '*.ts', '{"pattern":"*.ts"}', 'src/a.ts\nsrc/b.ts'),
    call('call-3', 'terminal', 'npm run build', '{"command":"npm run build"}', LONG_OUTPUT.slice(0, 400)),
    answer('The build passes.'),
  ];
  return {
    initial: { title: 'Build check', entries: [user(prompt), ...reply] },
    tools: { 'call-3': { output: LONG_OUTPUT, full: true } },
    turns: [{ entries: reply }],
  };
}

const at = '2026-10-09T10:00:00Z';
const row = (ID: string, Title: string, Status: string, extra: Partial<EngineTaskRow> = {}): EngineTaskRow => ({
  ID, Title, Status, Waits: [], Seat: 'worker', Steps: 0, Started: at, ...extra,
});

export const taskRows: EngineTaskRow[] = [
  row('2', 'Migrate the settings screen', 'running', { Done: 1, Running: 1, Queued: 0, Failed: 1, Total: 3 }),
  row('2.1', 'Read the current screen', 'done', { Parent: '2', Steps: 4, Ended: at }),
  row('2.2', 'Port the form fields', 'running', {
    Parent: '2', Steps: 7, Live: { Step: 8, Command: 'npm run typecheck', Since: at },
  }),
  row('2.3', 'Update the snapshot tests', 'failed', { Parent: '2', Steps: 3, Note: 'Two snapshots differ.' }),
];

export const taskPage: EngineTaskPage = {
  Row: taskRows[0],
  Description: 'Move the settings screen onto the shared form primitives.',
  Result: 'Two of three parts are finished; the tests need a decision.',
  Checks: ['Typecheck passes', 'Snapshots match'],
  Steps: [
    { step: 1, command: 'cat src/settings.tsx', observation: 'form with 6 fields' },
    { step: 2, command: 'npm run typecheck', observation: 'ok' },
  ],
  Notes: [{ Author: 'worker-1', Body: 'Started on the form fields.', At: at }],
  Children: taskRows.slice(1),
  Folder: '/mock-workspace',
};

/** An aside announcing task 2, plan rows (parent + three children) and a page for task 2. */
export function withTasks(prompt = 'Migrate the settings screen'): Scenario {
  const aside: WireEntry = { Role: 'aside', Text: 'Task 2 started: Migrate the settings screen.', TaskIDs: ['2'] };
  return {
    initial: {
      title: 'Settings migration',
      entries: [user(prompt), aside, answer('I split the work into three parts.')],
      tasks: taskRows,
    },
    taskPages: { '2': taskPage },
  };
}

export const question: EngineQuestion = {
  id: 7,
  kind: 'choice',
  ask: 'Which database should the app use?',
  head: 'Pick a database',
  options: [
    { key: 'sqlite', label: 'SQLite', body: 'One file, no server.' },
    { key: 'postgres', label: 'Postgres', body: 'Shared server.' },
  ],
};

/** The engine needs a person: one question with two options. Answering clears it. */
export function pendingQuestion(prompt = 'Set up storage'): Scenario {
  return {
    initial: { title: 'Storage', entries: [user(prompt)], needsPerson: true, running: true, questions: [question] },
    turns: [{ entries: [answer('Using the option you picked.')] }],
  };
}

/** Text events stream first; the final snapshot carries the whole reply. Use manual to hold it back. */
export function streaming(prompt = 'Say hello'): Scenario {
  return {
    initial: { title: 'Greeting', entries: [], running: false },
    turns: [{
      stream: ['Hello there. ', 'This reply ', 'arrived in pieces.'],
      entries: [answer('Hello there. This reply arrived in pieces.')],
    }],
  };
}
