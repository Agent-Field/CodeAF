// Fixtures for the model tests: realistic engine entries and snapshots.

import type { EngineEntry, EngineEvent, EngineSnapshot } from '../../chat/engine-client.ts';
import type { RichEntry } from './entry.ts';

export const entry = (e: Partial<RichEntry> & { Role: EngineEntry['Role'] }): RichEntry => ({ Text: '', ...e }) as RichEntry;

export const user = (Text: string, extra: Partial<RichEntry> = {}) => entry({ Role: 'user', Text, ...extra });

export const final = (Text: string) => entry({ Role: 'assistant', Text, Answer: true });

export const update = (Text: string, extra: Partial<RichEntry> = {}) =>
  entry({ Role: 'assistant', Text, Answer: true, Addressed: true, ...extra });

export const narrate = (Text: string) => entry({ Role: 'assistant', Text, Answer: false });

export const tool = (Tool: string, CallID: string, args: object, extra: Partial<RichEntry> = {}) =>
  entry({ Role: 'tool', Tool, CallID, Hint: `${Tool} ${CallID}`, Args: JSON.stringify(args), Answered: true, ...extra });

export const snap = (entries: RichEntry[], extra: Partial<EngineSnapshot> = {}): EngineSnapshot =>
  ({ id: 's1', sessionFile: 'f.jsonl', entries, running: false, tasks: [], title: 'T', questions: [], ...extra }) as unknown as EngineSnapshot;

export const event = (kind: string, fields: Partial<EngineEvent> = {}): EngineEvent => ({
  kind,
  text: '',
  tool: '',
  hint: '',
  raw: {},
  ...fields,
});

export const toolEvent = (kind: string, CallID: string, extra: Record<string, unknown> = {}, tool = 'bash') =>
  event(kind, { tool, hint: `${tool} ${CallID}`, raw: { CallID, ...extra } });
