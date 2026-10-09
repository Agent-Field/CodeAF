import { invoke, isTauri } from '@tauri-apps/api/core';
import type { WorkSection } from './work-model';
import type { PlanTaskRow, PlanTaskPage } from '../tasks/plan-model';

export const ENGINE_MODEL = 'deepseek/deepseek-v4.1-flash';
export type EngineQuestionBlock = { kind: string; title?: string; body?: string; rows?: string[][]; path?: string };
export type EngineQuestion = {
 id: number; ref?: string; kind: string; ask: string; form?: string; head: string; reason?: string;
 options?: { key: string; label: string; body?: string; consequence?: string; safe?: boolean; widening?: boolean; blocks?: EngineQuestionBlock[] }[];
 input?: { kind?: string; prompt?: string; secret?: boolean; blanks?: { label: string; kind?: string; default?: string; choices?: string[] }[]; dial?: unknown };
 attach?: EngineQuestionBlock[]; scope?: string[]; asked?: string; deadline?: string;
};
export type EngineAnswer = { kind: string; id: number; ref?: string; key: string; picked?: string[]; change?: string; blanks?: Record<string,string>; scope?: string };
export type EngineEntry = {
 Role: 'user' | 'assistant' | 'tool' | 'note' | 'aside'; Text: string;
 Answer?: boolean; Addressed?: boolean; Interrupted?: boolean;
 Tool?: string; Hint?: string; CallID?: string; Answered?: boolean;
 Args?: string; Output?: string; Caption?: string; CaptionCategory?: string;
};
export type EngineTaskRow = PlanTaskRow & { Depth?: number; USD?: number; Model?: string; Tokens?: number; LiveParts?: unknown[]; Folder?: string; TrajectoryPath?: string };
export type EngineSnapshot = {
 id: string; sessionFile: string; workspace: string; model: string; persistent: boolean;
 running: boolean; needsPerson: boolean; questions?: EngineQuestion[]; planError?: string; entries: EngineEntry[]; tasks: EngineTaskRow[];
 usage: { Input: number; Output: number; CostUSD: number; Duration: number; Turns: number; Calls?: number; CacheRead?: number; CacheWrite?: number };
 title: string; seq: number; updatedAt?: string;
};
export type EngineEvent = { kind: string; text: string; tool: string; hint: string; error?: string; raw: Record<string, unknown> };
export type EngineTaskPage = PlanTaskPage & { Folder?: string; Live?: PlanTaskRow['Live']; Children?: EngineTaskRow[] | null; WaitRows?: EngineTaskRow[] | null; Program?: unknown };
export class EngineError extends Error {
 readonly status: number;
 constructor(message: string, status = 0) { super(message); this.name = 'EngineError'; this.status = status; }
}
type Connection = { url: string; token: string; model: string };
async function endpoint(path: string): Promise<{ url: string; headers: Headers }> {
 const headers = new Headers({ Accept: 'application/json' });
 if (!isTauri()) return { url: `/api/engine${path}`, headers };
 const connection = await invoke<Connection>('engine_connection');
 if (connection.model !== ENGINE_MODEL) throw new EngineError('The engine is not using the required DeepSeek v4.1 Flash model.');
 const base = new URL(connection.url);
 if (base.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname)) throw new EngineError('The local engine announced an invalid connection.');
 headers.set('Authorization', `Bearer ${connection.token}`);
 return { url: `${base.origin}/api/engine${path}`, headers };
}
async function fetchEngine(path: string, init?: RequestInit): Promise<Response> {
 const target = await endpoint(path);
 const headers = target.headers;
 new Headers(init?.headers).forEach((value, name) => headers.set(name, value));
 if (init?.body) headers.set('Content-Type', 'application/json');
 let response: Response;
 try { response = await fetch(target.url, { ...init, headers, cache: 'no-store' }); }
 catch (error) {
  if (init?.signal?.aborted) throw error;
  throw new EngineError('Cannot reach the CodeAF engine. Reconnect this conversation to its saved session.');
 }
 if (!response.ok) {
  const body = await response.json().catch(() => null) as { error?: unknown } | null;
  throw new EngineError(typeof body?.error === 'string' ? body.error : `The engine request failed (${response.status}).`, response.status);
 }
 return response;
}
function snapshotFrom(value: unknown): EngineSnapshot {
 if (!value || typeof value !== 'object') throw new EngineError('The engine returned an invalid session.');
 const s = value as EngineSnapshot;
 if (s.model !== ENGINE_MODEL) throw new EngineError('The conversation is not using the required DeepSeek v4.1 Flash model.');
 if (typeof s.id !== 'string' || !s.id || typeof s.sessionFile !== 'string' || typeof s.workspace !== 'string' || typeof s.running !== 'boolean' || typeof s.needsPerson !== 'boolean' || typeof s.persistent !== 'boolean' || typeof s.title !== 'string' || !Number.isSafeInteger(s.seq) || s.seq < 0 || (s.entries !== null && !Array.isArray(s.entries)) || (s.tasks !== null && !Array.isArray(s.tasks)) || !s.usage || typeof s.usage !== 'object') throw new EngineError('The engine returned an invalid session.');
 if (!s.persistent || !s.sessionFile) throw new EngineError('The engine session is not persistent; it cannot safely survive closing this tab.');
 const entries = s.entries ?? [];
 if (!entries.every(entry => entry && ['user', 'assistant', 'tool', 'note', 'aside'].includes(entry.Role) && typeof entry.Text === 'string')) throw new EngineError('The engine returned invalid conversation history.');
 const tasks = s.tasks ?? [];
 if (!tasks.every(row => row && typeof row.ID === 'string' && typeof row.Title === 'string' && typeof row.Status === 'string' && (row.Waits == null || (Array.isArray(row.Waits) && row.Waits.every(id => typeof id === 'string'))))) throw new EngineError('The engine returned an invalid task plan.');
 if (s.planError !== undefined && typeof s.planError !== 'string') throw new EngineError('The engine returned an invalid plan read status.');
 const questions = s.questions ?? [];
 if (!Array.isArray(questions) || !questions.every(q => q && Number.isSafeInteger(q.id) && q.id >= 0 && (q.id > 0 || typeof q.ref === 'string' && !!q.ref) && typeof q.kind === 'string' && typeof q.ask === 'string' && typeof q.head === 'string' && (q.options == null || Array.isArray(q.options) && q.options.every(option => option && typeof option.key === 'string' && typeof option.label === 'string')))) throw new EngineError('The engine returned an invalid pending question.');
 return { ...s, questions, entries, tasks: tasks.map(row => ({ ...row, Waits: row.Waits ?? [] })) };
}
const sessionPath = (id: string) => `/sessions/${encodeURIComponent(id)}`;
export async function connectEngine(sessionFile?: string): Promise<EngineSnapshot> {
 const response = await fetchEngine('/sessions', { method: 'POST', body: JSON.stringify(sessionFile ? { sessionFile } : {}) });
 return snapshotFrom(await response.json());
}
export async function readEngine(id: string): Promise<EngineSnapshot> {
 return snapshotFrom(await (await fetchEngine(sessionPath(id))).json());
}
async function action(id: string, kind: 'turn' | 'stop' | 'answer', body?: unknown): Promise<EngineSnapshot> {
 const response = await fetchEngine(`${sessionPath(id)}/${kind}`, { method: 'POST', body: JSON.stringify(body ?? {}) });
 const result: unknown = await response.json();
 if (!result || typeof result !== 'object' || !('accepted' in result) || result.accepted !== true) throw new EngineError('The engine did not accept this action.');
 return readEngine(id);
}
export function sendEngine(id: string, text: string, mode: 'submit' | 'steer' | 'queue' = 'submit'): Promise<EngineSnapshot> {
 return action(id, 'turn', { text, mode });
}
export function stopEngine(id: string): Promise<EngineSnapshot> { return action(id, 'stop'); }
/** Identity and explicit canonical option keys cross unchanged; only the engine resolves a question. */
export function answerEngine(id: string, answer: EngineAnswer): Promise<EngineSnapshot> { return action(id, 'answer', answer); }
export async function readTaskPage(id: string, taskId: string): Promise<EngineTaskPage> {
 return (await (await fetchEngine(`${sessionPath(id)}/tasks/${encodeURIComponent(taskId)}`)).json()) as EngineTaskPage;
}

export async function readToolResult(id: string, callId: string): Promise<{output:string;full:boolean}> {
 const result:unknown=await (await fetchEngine(`${sessionPath(id)}/tools/${encodeURIComponent(callId)}`)).json();
 if(!result || typeof result!=='object' || !('output' in result) || typeof result.output!=='string' || !('full' in result) || typeof result.full!=='boolean')throw new EngineError('The engine returned an invalid tool result.');
 return result as {output:string;full:boolean};
}

// Fetch supports the native Bearer header; EventSource cannot. Aborting only
// detaches this reader. Stop is a separate, explicit POST.
export async function watchEngine(snapshot: EngineSnapshot, onSnapshot: (snapshot: EngineSnapshot) => void, onEvent: (event: EngineEvent) => void, signal: AbortSignal): Promise<void> {
 let after = snapshot.seq;
 const response = await fetchEngine(`${sessionPath(snapshot.id)}/events?after=${after}`, { signal, headers: { Accept: 'text/event-stream' } });
 if (!response.body || !response.headers.get('content-type')?.includes('text/event-stream')) throw new EngineError('The engine did not open a conversation stream.');
 const reader = response.body.getReader();
 const decoder = new TextDecoder();
 let buffer = '';
 let data: string[] = [];
 function dispatch() {
  if (signal.aborted || !data.length) { data = []; return; }
  const text = data.join('\n'); data = [];
  let record: unknown;
  try { record = JSON.parse(text); } catch { throw new EngineError('The engine sent an invalid stream record.'); }
  if (!record || typeof record !== 'object') throw new EngineError('The engine sent an invalid stream record.');
  const item = record as { seq: number; type: string; snapshot?: unknown; event?: EngineEvent };
  if (!Number.isSafeInteger(item.seq) || item.seq < 0) throw new EngineError('The engine sent an invalid stream sequence.');
  if (item.seq <= after) return;
  if (item.type === 'snapshot') {
   const next = snapshotFrom(item.snapshot);
   if (next.id !== snapshot.id || next.sessionFile !== snapshot.sessionFile) throw new EngineError('The engine stream changed conversations.');
   onSnapshot(next);
  } else if (item.type === 'event' && item.event && typeof item.event.kind === 'string') onEvent(item.event);
  else throw new EngineError('The engine sent an unknown stream record.');
  after = item.seq;
 }
 function consume(final = false) {
  let index: number;
  while ((index = buffer.indexOf('\n')) >= 0) {
   const line = buffer.slice(0, index).replace(/\r$/, ''); buffer = buffer.slice(index + 1);
   if (!line) dispatch();
   else if (line === 'data') data.push('');
   else if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''));
  }
  if (final && buffer) { const line = buffer.replace(/\r$/, ''); if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, '')); buffer = ''; }
  if (final) dispatch();
 }
 try {
  while (!signal.aborted) {
   const chunk = await reader.read();
   if (chunk.done) { buffer += decoder.decode(); consume(true); if (!signal.aborted) throw new EngineError('The engine connection closed. Reconnect this saved conversation.'); break; }
   buffer += decoder.decode(chunk.value, { stream: true }); consume();
  }
 } catch (error) { if (!signal.aborted) throw error; }
 finally { await reader.cancel().catch(() => undefined); reader.releaseLock(); }
}

// Only a person's canonical user entry creates an instruction section. System
// asides, notes and tool outputs never become instructions or assistant answers.
export function projectEngineSections(snapshot: EngineSnapshot, existingSections: readonly WorkSection[]): WorkSection[] {
 const existing = new Map(existingSections.map(section => [section.id, section]));
 type ProjectedSection = WorkSection & { recordedSteps?: { callId?: string; tool: string; hint: string; args: string; output: string; answered: boolean }[]; engineNotes?: string[] };
 const sections: ProjectedSection[] = [];
 const pendingNotes: string[] = [];
 snapshot.entries.forEach((entry, index) => {
  if (entry.Role === 'user') {
   const id = `engine:${snapshot.sessionFile}:${index}`;
   const previous = existing.get(id);
   sections.push({ id, title: entry.Text.split(/\r?\n/)[0], original: entry.Text, digest: '', blocks: [], amendments: previous?.amendments ?? [], folded: previous?.folded ?? false, originalOpen: previous?.originalOpen ?? false, stepsOpen: previous?.stepsOpen ?? false, sample: false, context: previous?.context, toolGroupsOpen: previous?.toolGroupsOpen, timeline: [], recordedSteps: [], engineNotes: pendingNotes.splice(0) });
  } else if (entry.Role === 'tool' && entry.Tool && sections.length) {
   const section=sections[sections.length-1];
   section.timeline?.push({kind:'tool',index:section.recordedSteps?.length??0});
   section.recordedSteps?.push({ ...(entry.CallID?{callId:entry.CallID}:{}), tool: entry.Tool ?? '', hint: entry.Hint ?? entry.Caption ?? '', args: entry.Args ?? '', output: entry.Output ?? '', answered: entry.Answered === true });
  } else if ((entry.Role === 'note' || entry.Role === 'aside') && entry.Text) {
   if (sections.length) {const section=sections[sections.length-1];section.timeline?.push({kind:'note',index:section.engineNotes?.length??0});section.engineNotes?.push(entry.Text);} else pendingNotes.push(entry.Text);
  } else if (entry.Role === 'assistant' && entry.Text && sections.length) {
   const section = sections[sections.length - 1];
   section.timeline?.push({kind:'text',index:section.blocks.length});
   section.blocks.push({ kind: 'paragraph', text: entry.Text });
   if (entry.Answer || entry.Addressed || !section.digest) section.digest = (entry.Text.split(/\r?\n/).find(line => line.trim()) ?? '').replace(/^#{1,6}\s+/, '').replace(/\[([^\]]+)\]\([^)]*\)/g, '$1').replace(/[*_`]/g, '');
  }
 });
 return sections;
}
