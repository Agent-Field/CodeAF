// The conversation view model. Every component in this folder renders these
// shapes; only transcript.ts builds them, from canonical engine records.
// Nothing here is invented: a field is empty when the engine has not said it.

import type { EngineQuestion, EngineTaskRow } from '../chat/engine-client';

/** One tool call, paired by its canonical CallID. */
export type ToolStep = {
  id: string; // CallID, or a stable index key when the record has none
  callId?: string;
  tool: string; // registered tool name, e.g. "bash"
  hint: string; // the engine's own one-line gloss of the call
  args: string;
  output: string; // compact inline output; full output is fetched on demand
  state: 'running' | 'done' | 'failed' | 'stopped'; // stopped: cancelled by the person's Stop
  entryIndex?: number; // record position; lets the live overlay tell a call is already recorded
};

/** What sits inside one assistant reply, in record order. */
export type TurnItem =
  | { kind: 'text'; id: string; text: string; streaming: boolean } // Markdown
  | { kind: 'thinking'; id: string; text: string; streaming: boolean }
  | { kind: 'tools'; id: string; steps: ToolStep[] } // consecutive calls grouped
  | { kind: 'task'; id: string; taskId?: string; title: string; status: string; summary: string; body: string } // a task's completion aside
  | { kind: 'aside'; id: string; aside: 'job' | 'watch'; title: string; body: string } // a background job or watch notice; body is literal
  | { kind: 'note'; id: string; text: string; long?: boolean } // engine note (e.g. compaction); long: model-directed text, drawn collapsed
  | { kind: 'steer'; id: string; text: string } // the person's words typed into the running turn; literal
  | { kind: 'error'; id: string; text: string };

/** One exchange: the person's message and everything that answered it. */
export type Turn = {
  id: string; // stable across snapshots: `${sessionFile}:${entryIndex}`
  user: string; // the literal words; never Markdown-rendered
  items: TurnItem[];
  state: 'streaming' | 'working' | 'done' | 'stopped' | 'failed';
  digest: string; // first meaningful line of the final answer, plain text, '' when none
};

export type ConversationModel = {
  title: string; // engine title, '' until generated
  turns: Turn[];
  preface: TurnItem[]; // notes recorded before the first user message
  running: boolean;
  questions: EngineQuestion[];
  tasks: EngineTaskRow[];
  planError?: string;
};

/** Per-tab, view-only state. Persisted with the tab; never sent to the engine. */
export type ConversationViewState = {
  folded: Record<string, boolean>; // turn id -> folded
  open: Record<string, boolean>; // tool group / thinking / task notice id -> expanded
};
