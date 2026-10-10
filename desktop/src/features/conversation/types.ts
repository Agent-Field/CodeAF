// The conversation view model. Every component in this folder renders these
// shapes; only model/ builds them, from canonical engine records.
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
  outputOmitted?: boolean; // the engine left the output out of the snapshot; CallDetail fetches it when the call is expanded
  covered?: boolean; // the read went to a quick task whose answer rides on another call of the batch; output is empty
  state: 'running' | 'done' | 'failed' | 'stopped'; // stopped: cancelled by the person's Stop
  entryIndex?: number; // record position; lets the live overlay tell a call is already recorded
  tookMs?: number; // the engine's Took for this call; absent when unknown
  startedAt?: number; // epoch ms the call began, live only; lets a row show elapsed time
  decision?: string; // what the person decided about this call, in the past tense: "allowed once"; absent when nothing was asked
};

/** How an engine note is drawn: a compaction marker or a retry in flight; absent is a plain session note. */
export type NoteTone = 'compaction' | 'retry';

/** What sits inside one assistant reply, in record order. */
export type TurnItem =
  | { kind: 'text'; id: string; text: string; streaming: boolean } // Markdown
  | { kind: 'task'; id: string; taskId?: string; title: string; status: string; summary: string; body: string } // a task's completion aside
  | { kind: 'aside'; id: string; aside: 'job' | 'watch'; title: string; body: string } // a background job or watch notice; body is literal
  | { kind: 'note'; id: string; text: string; long?: boolean; tone?: NoteTone; time?: string; undoReceipts?: string[] } // engine note; long: model-directed text, drawn collapsed
  | { kind: 'steer'; id: string; text: string; landing?: string; consumed?: boolean } // the person's words typed into the running turn; literal
  | { kind: 'error'; id: string; text: string };

// ---- Contract v2 (docs/ELEMENTS.md). Lanes build against these; model/ fills them. ----

/** Where an item sits: the person's conversation, or the folded machinery under it. */
export type Rhythm = 'conversation' | 'work';

/** One batch of parallel tool calls (ELEMENTS §3.2). */
export type WorkStep = {
  id: string;
  title: string; // narration line > engine Caption > composed ("Read 3 files")
  titleSource: 'narration' | 'caption' | 'composed';
  category: string; // CaptionCategory: search|read|edit|create|run|test|browse|transfer|communicate|coordinate|plan|wait|work
  calls: ToolStep[];
  tookMs?: number; // the longest known call: a batch's calls run side by side; absent when unknown
  state: 'preparing' | 'running' | 'waiting' | 'done' | 'failed' | 'stopped';
};

/** Everything between two conversation items (ELEMENTS §3.1). */
export type WorkBlock = {
  kind: 'work';
  id: string;
  steps: WorkStep[];
  thinking?: { text: string; streaming: boolean; seconds?: number }; // live only; absent after reload
  notes: TurnItem[]; // note | aside | steer items that happened inside the work
  live: boolean; // the turn is still producing this block
  startedAt?: number; // epoch ms the block began, live only; lets the header show elapsed time
  summary: { seconds?: number; thoughtSeconds?: number; steps: number; calls: number; failed: number };
};

/** A file the turn produced or touched (ELEMENTS §5.1, §5.4). */
export type FileRef = { path: string; added?: number; removed?: number; capped?: boolean; source: 'write' | 'edit' | 'read' | 'image' | 'task' | 'attachment' | 'markdown' };

/** A deliverable promoted into the conversation (ELEMENTS §5.3–5.5). */
export type Deliverable =
  | { kind: 'image'; id: string; path: string; caption: string; meta: string }
  | { kind: 'changes'; id: string; files: FileRef[] }
  | { kind: 'media'; id: string; path: string; media: 'audio' | 'video'; caption: string };

/** Conversation-rhythm items of a turn, in order (ELEMENTS §1–2). */
export type TurnBlock =
  | WorkBlock
  | { kind: 'context-note'; id: string; text: string; undoReceipts?: string[] }
  | { kind: 'update'; id: string; text: string; cut: boolean; streaming: boolean } // Addressed interim update
  | { kind: 'answer'; id: string; text: string; streaming: boolean } // Answer final reply (Markdown)
  | { kind: 'task'; id: string; taskId?: string; title: string; status: string; summary: string; body: string; live?: string }
  | { kind: 'deliverable'; id: string; deliverable: Deliverable }
  | { kind: 'receipt'; id: string; questionKey: string; text: string; state: 'waiting' | 'decided' | 'withdrawn' } // question receipt in the flow
  | { kind: 'error'; id: string; text: string };

/** One exchange: the person's message, their attachments, and every block that answered it. */
export type TurnV2 = {
  id: string;
  user: string;
  attachments: FileRef[]; // pictures (ImageRefs) and attached files
  steer: { id: string; text: string; landing?: string; consumed: boolean }[];
  blocks: TurnBlock[];
  state: 'streaming' | 'working' | 'done' | 'stopped' | 'failed';
  digest: string; // first meaningful line of the final answer, plain text, '' when none
};

export type ConversationModel = {
  title: string; // engine title, '' until generated
  turns: TurnV2[];
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
