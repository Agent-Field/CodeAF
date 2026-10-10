import type { SnapshotTail } from '../../../src/features/chat/snapshotMerge';
import type { Page, Route } from '@playwright/test';
import { historyRoutes, type HistoryHandle, type MockHistory } from './history-engine';
import type { AttentionItem, WorldRow } from '../../../src/features/chat/world-client';
import type { EngineEntry, EngineEvent, EngineFile, EngineFileDiff, EngineSnapshot, EngineTaskPage, TerminalInfo } from '../../../src/features/chat/engine-client';

export const MODEL = 'deepseek/deepseek-v4.1-flash';
export const GLM_FLASH = 'z-ai/glm-5.3-flash';
export const GLM = 'z-ai/glm-5.3';
/** The segment words the engine reports for the three default pins. */
const PIN_LABELS: Record<string, string> = { [GLM_FLASH]: 'GLM Flash', [MODEL]: 'DS Flash', [GLM]: 'GLM 5.3' };

export type StreamRecord = { seq: number; type: 'snapshot' | 'event'; snapshot?: EngineSnapshot | SnapshotTail; event?: EngineEvent };

/** What one scripted turn does after the person's message is recorded. */
export type ScriptedTurn = {
  entries?: EngineEntry[];
  patch?: Partial<EngineSnapshot>;
  /** Text deltas streamed (as `text` events) before the final snapshot. */
  stream?: string[];
  /** Live events streamed before the final snapshot, after any text. */
  events?: EngineEvent[];
};

/** A workspace file the mock serves through GET /files and reports through POST /files/stat. */
export type MockFile = Omit<EngineFile, 'name' | 'size' | 'hash'> & { dir?: boolean; modTime?: string };

/** A changed file the mock reports through GET /changes and /diff; counts derive from the hunks. */
export type MockDiff = Pick<EngineFileDiff, 'hunks'> & Partial<Pick<EngineFileDiff, 'status' | 'lines' | 'binary' | 'truncated'>>;
/** A terminal or job the mock engine already holds; `output` is its kept log (raw terminal text). */
export type MockTerminal = Partial<TerminalInfo> & { id: string; output?: string };

/** An engine background job the mock lists under /jobs; `log` is the text its log route returns, `cut` marks a trimmed front. */
export type MockJob = { id: number; name?: string; command?: string; state?: 'running' | 'done' | 'failed' | 'stopped'; startedAt?: string; elapsedMs?: number; exitCode?: number; log?: string; cut?: boolean };

export type Scenario = {
  /** Engine background jobs served under /jobs (list, log tail, stop). A stop marks the job stopped. */
  jobs?: MockJob[];
  /** Conversations the History routes serve (list, recap, messages, search, archive). */
  history?: MockHistory;
  /** Terminals and jobs served under /terminals; a POST /terminals adds more. */
  terminals?: MockTerminal[];
  /** State returned by POST /sessions and GET /sessions/{id}. */
  initial: Partial<EngineSnapshot>;
  /** One element is consumed per accepted turn; the last one repeats. */
  turns?: ScriptedTurn[];
  taskPages?: Record<string, EngineTaskPage>;
  tools?: Record<string, { output: string; full: boolean }>;
  /** Workspace files by path, relative or absolute; anything else stats as missing. */
  files?: Record<string, MockFile>;
  /** Changed files by workspace-relative path; GET /changes lists them and GET /diff returns the hunks. */
  diffs?: Record<string, MockDiff>;
  /** When true a turn only records the message; call engine.advance() to apply the reply. */
  manual?: boolean;
  /** Models the provider offers; defaults to the one default model. */
  models?: { id: string; name: string; efforts?: string[] }[];
  /** false: the engine serves no /places/policy route (an engine before the Places organization settings). */
  placesPolicy?: false;
  /** The engine-wide world feed (GET /world, GET /events). Absent: the engine serves no feed and both routes answer 404. */
  world?: { rows: WorldRow[]; items: AttentionItem[]; jobs?: { chatId: string; running: number; jobs: unknown[] }[] };
  /** Forced HTTP failure per endpoint, e.g. { turn: 409 }. */
  fail?: Partial<Record<'create' | 'read' | 'turn' | 'stop' | 'answer' | 'events' | 'task', number>>;
};

export type Call = { method: string; path: string; body: Record<string, unknown> };

export type MockEngine = {
  calls: Call[];
  /** What the History archive route changed, in order. */
  history: HistoryHandle;
  /** The conversation model in force at the moment each accepted turn arrived. */
  turnModels: string[];
  snapshot: () => EngineSnapshot;
  /** Replaces the world feed's rows and/or attention items and streams the new state to readers. Needs `scenario.world`. */
  setWorld: (next: { rows?: WorldRow[]; items?: AttentionItem[]; jobs?: { chatId: string; running: number; jobs: unknown[] }[] }) => void;
  /** Apply the next scripted turn reply (for scenarios with manual: true). */
  advance: () => void;
  /** Merge fields into the snapshot and publish a snapshot record to stream readers. */
  update: (patch: Partial<EngineSnapshot>) => void;
  /** Push one live event, the way a running turn would. */
  push: (event: EngineEvent) => void;
  /** Replace one file's diff so the next GET /diff answers with it. */
  replaceDiff: (path: string, diff: MockDiff) => void;
  /** Replace one file's bytes and stat with no event. */
  replaceFile: (path: string, file: MockFile) => void;
  /** Drops (true) or restores (false) the connection: every engine route fails until restored. `how` picks a refused connection or a 503. */
  setOffline: (offline: boolean, how?: 'refused' | '503') => void;
  /** Streams a `retrying` event; `delaySeconds` travels in raw.Retry.DelaySeconds the way the engine sends it. */
  retrying: (delaySeconds?: number, text?: string) => void;
  /** Streams a `compacting` event, the engine's start of a summarization (it draws nothing). */
  compacting: () => void;
  /** Streams a `compacted` event, after which the Earlier-messages-summarized divider draws. */
  compacted: () => void;
};

/** Output bodies over this many bytes are omitted from snapshots (d5-be-incremental-snapshot's cap); the full body is one GET /tools/{callId} away. */
export const OUTPUT_CAP_BYTES = 16 * 1024;

/** Names, one-line jobs, sections and states of the roles, as the engine reports them (a sample of each section). */
const ROLES = [
  { id: 'conversation', name: 'Conversation', controls: 'Answers what you type in a chat and decides when to start a task.', category: 'conversation', live: true },
  { id: 'tasks', name: 'Tasks', controls: 'Does the steps of a task: reading, editing, running commands.', category: 'conversation', live: true },
  { id: 'naming', name: 'Chat titles', controls: 'Names each chat from its opening exchange.', category: 'naming', live: true },
  { id: 'summaries', name: 'Summaries', controls: 'Writes the short recap of a conversation that History shows.', category: 'naming', live: true, inherits: 'naming' },
  { id: 'placefiling', name: 'Chat filing', controls: 'Picks which of your places a chat belongs in, to offer it after the first reply. Never creates a place.', category: 'places', live: false },
  { id: 'memory', name: 'Memory', controls: 'Reads each turn for things worth remembering and tidies what has been remembered.', category: 'memory', live: true },
];
const ROLE_CATEGORIES = [
  { id: 'conversation', name: 'Conversation and tasks' },
  { id: 'naming', name: 'Naming and summaries' },
  { id: 'places', name: 'Places organization' },
  { id: 'memory', name: 'Memory, routing and safety' },
];
/**
 * A sample of internal/placegraph's policy table, plus the four hierarchy caps.
 * Those four rows match policy.go: defaults, bounds, and the depth explain that
 * says a person's own places can go deeper.
 */
const PLACES_POLICY = [
  { key: 'clusterOffers', group: 'offers', name: 'Offer new places', explain: 'When enough chats in no place belong together, offer to put them in a place. You approve every new place.', kind: 'switch', default: true, design: true },
  { key: 'autoFile', group: 'offers', name: 'File chats without asking', explain: 'Put a chat in a place you already have when codeaf is very sure, and say so.', kind: 'switch', default: false, design: false },
  { key: 'maxAiTopLevel', group: 'limits', name: 'Top-level places codeaf may create', explain: "Counts only places created from codeaf's offers. Places you make yourself are never limited.", kind: 'number', default: 6, min: 0, max: 50, unit: 'places', design: false },
  { key: 'maxAiSiblings', group: 'limits', name: 'Places codeaf may create under one parent', explain: "Counts only places created from codeaf's offers.", kind: 'number', default: 8, min: 0, max: 100, unit: 'places', design: false },
  { key: 'maxAiDepth', group: 'limits', name: 'Deepest level for a new place', explain: "A top-level place is level 1. Places you make yourself can go deeper; this only limits places created from codeaf's offers.", kind: 'number', default: 3, min: 1, max: 6, unit: 'levels', design: false },
  { key: 'maxAiPlaces', group: 'limits', name: 'Places codeaf may create in all', explain: "Counts active places created from codeaf's offers.", kind: 'number', default: 30, min: 0, max: 500, unit: 'places', design: false },
] as const;

const emptyUsage = { Input: 0, Output: 0, CostUSD: 0, Duration: 0, Turns: 0 };

function baseSnapshot(initial: Partial<EngineSnapshot>): EngineSnapshot {
  return {
    id: 'mock-1',
    sessionFile: 'mock-session-1.jsonl',
    workspace: '/mock-workspace',
    model: MODEL,
    persistent: true,
    running: false,
    needsPerson: false,
    entries: [],
    tasks: [],
    usage: emptyUsage,
    title: '',
    seq: 0,
    ...initial,
  };
}

/** The snapshot as the engine serves it: an over-cap Output becomes "" with OutputOmitted and OutputBytes set. */
function elideOutputs(entries: EngineEntry[]): EngineEntry[] {
  return entries.map(entry => {
    const bytes = entry.Output ? Buffer.byteLength(entry.Output) : 0;
    return bytes > OUTPUT_CAP_BYTES ? { ...entry, Output: '', OutputOmitted: true, OutputBytes: bytes } as EngineEntry : entry;
  });
}

const json = (route: Route, value: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });

const sse = (records: StreamRecord[]) =>
  ': connected\n\n' + records.map(r => `id: ${r.seq}\ndata: ${JSON.stringify(r)}\n\n`).join('');

/**
 * Stream behaviour: route.fulfill delivers one finite body, so the client sees
 * the available records and then a closed stream (it reports "connection
 * closed"). With nothing new to say the request is held open, which is what an
 * idle real stream looks like. The client re-reads GET /sessions/{id} after
 * every action, so specs should rely on that for state and use the stream only
 * for streaming text.
 */
export async function installMockEngine(page: Page, scenario: Scenario): Promise<MockEngine> {
  let state = baseSnapshot(scenario.initial);
  const log: StreamRecord[] = [];
  let publishedEntries = state.entries.length;
  const calls: Call[] = [];
  const turnModels: string[] = [];
  let turnIndex = 0;
  let pending: ScriptedTurn | undefined;
  let closed = false;
  page.on('close', () => { closed = true; });

  // The world feed: every change is one full `reset` record, which the client applies at any cursor.
  let world = scenario.world ? structuredClone(scenario.world) : undefined;
  let worldSeq = 1;
  // Inactive tabs read this mirror instead of holding a session stream. It speaks the
  // world client's row (chatId, numeric needsYou) and carries the questions the preview answers.
  // It has no `session` field: the inbox's older feed lists a running row with one as another
  // window's work, and this mirror is the same conversation the window already has open.
  let sessionWorldSeq = 1;
  const sessionRow = () => ({
    chatId: state.id || 'mock-1', sessionFile: state.sessionFile, sessionId: state.id, sessionSeq: state.seq,
    title: state.title, workspace: state.workspace, running: state.running,
    needsYou: state.needsPerson ? Math.max(state.questions?.length ?? 0, 1) : 0,
    failed: 0, tasksRunning: (state.tasks ?? []).filter(task => task.Status === 'running').length,
    tasksTotal: (state.tasks ?? []).length, attached: true, archived: false, questions: state.questions ?? [],
  });
  const sessionWorldRecord = () => ({ epoch: 'mock-session', seq: sessionWorldSeq, type: 'reset', at: new Date().toISOString(), payload: { rows: [sessionRow()], items: [] } });
  const worldRecord = () => ({ epoch: 'mock-world', seq: worldSeq, type: 'reset', at: new Date().toISOString(), payload: structuredClone(world!) });
  const worldEvents = async (route: Route, after: number) => {
    const deadline = Date.now() + 60_000;
    while (!closed && Date.now() < deadline) {
      if (worldSeq > after) {
        const record = worldRecord();
        await route.fulfill({ status: 200, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body: `: connected\n\nid: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n` });
        return;
      }
      await new Promise(r => setTimeout(r, 25));
    }
    await route.abort().catch(() => undefined);
  };
  const sessionEvents = async (route: Route, after: number) => {
    const deadline = Date.now() + 60_000;
    while (!closed && Date.now() < deadline) {
      if (sessionWorldSeq > after) {
        const record = sessionWorldRecord();
        await route.fulfill({ status: 200, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body: `: connected\n\nid: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n` });
        return;
      }
      await new Promise(r => setTimeout(r, 25));
    }
    await route.abort().catch(() => undefined);
  };

  const publish = () => {
    state = { ...state, seq: state.seq + 1, updatedAt: new Date().toISOString() };
    sessionWorldSeq += 1;
    // The stream carries the same omission as a read, so a window never receives an over-cap body it would not get from GET.
    // A record is a tail from the last published length; a shorter transcript is a reset, matching the bridge.
    const { entries, ...header } = structuredClone(state);
    const reset = publishedEntries > entries.length;
    const from = reset ? 0 : publishedEntries;
    log.push({ seq: state.seq, type: 'snapshot', snapshot: { header: { ...header, entryCount: entries.length }, from, entries: elideOutputs(entries.slice(from)), ...(reset ? { reset: true } : {}) } });
    publishedEntries = entries.length;
  };
  const emitEvent = (event: EngineEvent) => {
    state = { ...state, seq: state.seq + 1 };
    log.push({ seq: state.seq, type: 'event', event });
  };
  const emit = (text: string) => emitEvent({ kind: 'text', text, tool: '', hint: '', raw: {} });
  const update = (patch: Partial<EngineSnapshot>) => {
    state = { ...state, ...patch };
    publish();
  };
  const nextTurn = (): ScriptedTurn | undefined => {
    const turns = scenario.turns ?? [];
    return turns[Math.min(turnIndex++, turns.length - 1)];
  };
  // A queued message waits in the snapshot's queue and is recorded as the person's next
  // message once the turn ends; until then it can be edited, moved and removed, and the
  // engine refuses (409) any change to one whose turn has started.
  let queued: { id: string; text: string }[] = [];
  let queuedSeq = 0;
  const publishQueue = () => update({ queue: queued.map(item => ({ ...item })) });
  const apply = (turn: ScriptedTurn | undefined) => {
    turn?.stream?.forEach(emit);
    turn?.events?.forEach(emitEvent);
    const entries = [...state.entries, ...(turn?.entries ?? []), ...queued.map((item): EngineEntry => ({ Role: 'user', Text: item.text }))];
    queued = [];
    state = { ...state, ...turn?.patch, running: false, entries, queue: [] };
    publish();
  };
  const advance = () => {
    apply(pending);
    pending = undefined;
  };

  // Offline drops every engine route, the way a stopped sidecar or a lost network does.
  let offline: 'refused' | '503' | undefined;
  const retrying = (delaySeconds?: number, text = 'the provider was busy') =>
    emitEvent({ kind: 'retrying', text, tool: '', hint: '', raw: delaySeconds ? { Retry: { DelaySeconds: delaySeconds } } : {} });
  const notice = (kind: 'compacting' | 'compacted') => emitEvent({ kind, text: '', tool: '', hint: '', raw: {} });

  // GET /sessions/{id}?since=N: the header plus entries[N:]. A `since` past the end (a rewritten transcript) answers in full with reset:true.
  const snapshotView = (since: string | null) => {
    const { entries, ...header } = state;
    if (since === null) return { ...header, entries: elideOutputs(entries) };
    const from = Number(since);
    const reset = !Number.isInteger(from) || from < 0 || from > entries.length;
    return { header: { ...header, entryCount: entries.length }, from: reset ? 0 : from, ...(reset ? { reset: true } : {}), entries: elideOutputs(reset ? entries : entries.slice(from)) };
  };

  const events = async (route: Route, after: number) => {
    const deadline = Date.now() + 60_000;
    while (!closed && Date.now() < deadline) {
      const fresh = log.filter(r => r.seq > after);
      if (fresh.length) {
        await route.fulfill({ status: 200, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body: sse(fresh) });
        return;
      }
      await new Promise(r => setTimeout(r, 25));
    }
    await route.abort().catch(() => undefined);
  };

  const turn = (route: Route, body: Record<string, unknown>) => {
    const text = String(body.text ?? '');
    turnModels.push(state.model);
    if (state.running && body.mode === 'queue') {
      queued.push({ id: `q${++queuedSeq}`, text });
      publishQueue();
      return json(route, { accepted: true });
    }
    if (state.running && body.mode === 'steer') {
      state = { ...state, entries: [...state.entries, { Role: 'user', Text: text, Steer: { Consumed: false, Landing: 'waiting for the running step' } }] };
      publish();
      return json(route, { accepted: true });
    }
    const files = (body.files as { name: string }[] | undefined) ?? [];
    const attached = files.map(f => ({ Path: `.codeaf/attachments/${f.name}`, Name: f.name }));
    const user = { Role: 'user', Text: text, ...(files.length ? { Attachments: attached } : {}) } as EngineEntry;
    state = { ...state, running: true, entries: [...state.entries, user] };
    publish();
    if (scenario.manual) pending = nextTurn();
    else apply(nextTurn());
    return json(route, { accepted: true });
  };

  const queueAction = (route: Route, action: string, body: Record<string, unknown>) => {
    const from = queued.findIndex(item => item.id === body.id);
    if (from < 0) return json(route, { error: 'that message has already been sent' }, 409);
    if (action === 'queue-edit') {
      const text = String(body.text ?? '').trim();
      if (!text) return json(route, { error: 'a queued message cannot be empty' }, 400);
      queued[from] = { ...queued[from], text };
    } else if (action === 'queue-remove') {
      queued.splice(from, 1);
    } else {
      // `to` is where the message ends up in the queue that remains without it.
      const [item] = queued.splice(from, 1);
      queued.splice(Math.min(Math.max(Number(body.to) || 0, 0), queued.length), 0, item);
    }
    publishQueue();
    return json(route, { accepted: true });
  };

  // Send now takes a queued message out of the queue: steered into the running turn, or submitted when idle.
  const queueSend = (route: Route, body: Record<string, unknown>) => {
    const from = queued.findIndex(item => item.id === body.id);
    if (from < 0) return json(route, { error: 'that message has already been sent' }, 409);
    const [item] = queued.splice(from, 1);
    publishQueue();
    return turn(route, { text: item.text, mode: state.running ? 'steer' : 'submit' });
  };

  // Answering records the decision the way the engine's recentOutcomes reports it.
  const answer = (body: Record<string, unknown>) => {
    const asked = state.questions?.find(q => q.id === body.id && q.kind === body.kind);
    const words = asked?.options?.find(o => o.key === body.key)?.label ?? String(body.key ?? '');
    const outcome = { kind: String(body.kind), token: String(body.ref || body.id), head: asked?.head, outcome: 'decided', words, by: String(body.decidedBy ?? 'person'), at: new Date().toISOString() };
    const questions = (state.questions ?? []).filter(q => q !== asked);
    const before = (state as { recentOutcomes?: unknown[] }).recentOutcomes ?? [];
    update({ needsPerson: questions.length > 0, questions, recentOutcomes: [outcome, ...before] } as Partial<EngineSnapshot>);
  };

  // Terminals keep their bytes, so a stream resumes from any offset like the real one.
  type Held = { info: TerminalInfo; bytes: Uint8Array };
  const enc = new TextEncoder();
  const concat = (a: Uint8Array, b: Uint8Array) => { const out = new Uint8Array(a.length + b.length); out.set(a); out.set(b, a.length); return out; };
  const held = new Map<string, Held>();
  const hold = (t: MockTerminal): Held => {
    const bytes = enc.encode(t.output ?? '');
    const info: TerminalInfo = { kind: t.command ? 'job' : 'terminal', title: t.command ?? 'zsh', cwd: `${state.workspace}`, shell: '/bin/zsh', state: 'running', startedAt: new Date().toISOString(), durationMs: 0, cols: 100, rows: 30, ...t, bytes: bytes.length };
    delete (info as { output?: string }).output;
    const entry = { info, bytes };
    held.set(t.id, entry);
    return entry;
  };
  scenario.terminals?.forEach(hold);
  let terminalCount = 0;
  const b64 = (bytes: Uint8Array) => Buffer.from(bytes).toString('base64');
  const termStream = async (route: Route, t: Held, after: number) => {
    const deadline = Date.now() + 60_000;
    while (!closed && Date.now() < deadline) {
      const fresh = t.bytes.length > after;
      if (fresh || t.info.state !== 'running') {
        let body = ': connected\n\n';
        if (fresh) body += `id: ${t.bytes.length}\ndata: ${JSON.stringify({ seq: t.bytes.length, type: 'output', dataBase64: b64(t.bytes.slice(after)) })}\n\n`;
        if (t.info.state !== 'running') body += `data: ${JSON.stringify({ seq: t.bytes.length, type: 'exit', info: t.info })}\n\n`;
        await route.fulfill({ status: 200, headers: { 'Cache-Control': 'no-cache' }, contentType: 'text/event-stream', body });
        return;
      }
      await new Promise(r => setTimeout(r, 25));
    }
    await route.abort().catch(() => undefined);
  };
  const plain = (bytes: Uint8Array) => new TextDecoder().decode(bytes).replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '').replace(/\r\n/g, '\n').replace(/\n+$/, '');
  const terminals = (route: Route, parts: string[], body: Record<string, unknown>, url: URL) => {
    const [, , , tid, act] = parts;
    if (!tid) {
      if (route.request().method() === 'GET') return json(route, [...held.values()].map(h => h.info));
      const id = `mock-term-${++terminalCount}`;
      const command = typeof body.command === 'string' && body.command ? body.command : undefined;
      const made = hold({ id, command, title: typeof body.title === 'string' && body.title ? body.title : command ?? 'zsh', output: command ? '' : 'mock$ ' });
      return json(route, made.info);
    }
    const t = held.get(tid);
    if (!t) return json(route, { error: 'this terminal is gone' }, 404);
    if (!act) return json(route, t.info);
    if (act === 'stream') return termStream(route, t, Number(url.searchParams.get('after') ?? 0));
    if (act === 'output') return json(route, { text: plain(t.bytes), truncated: false, info: t.info });
    if (act === 'input') { t.bytes = concat(t.bytes, Buffer.from(String(body.dataBase64 ?? ''), 'base64')); t.info.bytes = t.bytes.length; return json(route, { accepted: true }); }
    if (act === 'resize') { t.info.cols = Number(body.cols); t.info.rows = Number(body.rows); return json(route, t.info); }
    if (act === 'close' || act === 'remove') {
      if (t.info.state === 'running') { t.info.state = 'closed'; t.info.exitCode = 129; t.info.endedAt = new Date().toISOString(); }
      if (act === 'remove' || t.info.kind === 'terminal') held.delete(tid);
      return act === 'remove' ? json(route, { accepted: true }) : json(route, t.info);
    }
    return json(route, { error: 'unknown terminal action' }, 404);
  };

  const heldJobs = new Map((scenario.jobs ?? []).map(j => [String(j.id), { ...j }]));
  const jobs = (route: Route, parts: string[]) => {
    const [, , , jid, act] = parts;
    if (!jid) return json(route, [...heldJobs.values()].map(({ log: _log, cut: _cut, ...row }) => row));
    const job = heldJobs.get(jid);
    if (!job) return json(route, { error: `no job ${jid}` }, 404);
    if (act === 'log') return json(route, { text: job.log ?? '', truncated: job.cut === true });
    if (act === 'stop') { job.state = 'stopped'; return json(route, { accepted: true }); }
    return json(route, { error: 'unknown job action' }, 404);
  };

  const fileAt = (path: string) => scenario.files?.[path] ?? scenario.files?.[path.replace(`${state.workspace}/`, '')];
  const stat = (path: string) => {
    const file = fileAt(path);
    const outside = path.startsWith('/') && !path.startsWith(`${state.workspace}/`);
    return { path, exists: Boolean(file), dir: Boolean(file?.dir), size: file ? Math.floor(file.dataBase64.length * 0.75) : 0, modTime: file?.modTime, outside };
  };
  const files = (route: Route, arg: string | undefined, body: Record<string, unknown>, url: URL) => {
    if (arg === 'stat') return json(route, ((body.paths as string[]) ?? []).map(stat));
    const path = url.searchParams.get('path') ?? '';
    const file = fileAt(path);
    if (!file || file.dir) return json(route, { error: `no file ${path}` }, 404);
    const name = path.split('/').pop() ?? path;
    return json(route, { name, mime: file.mime, size: stat(path).size, hash: 'mock', dataBase64: file.dataBase64 });
  };

  const relative = (path: string) => path.replace(`${state.workspace}/`, '');
  const outside = (path: string) => path.startsWith('/') && !path.startsWith(`${state.workspace}/`);
  const split = (path: string) => { const at = path.lastIndexOf('/'); return at < 0 ? { name: path, dir: '' } : { name: path.slice(at + 1), dir: path.slice(0, at) }; };
  const counts = (diff: MockDiff) => diff.hunks.flatMap(h => h.lines).reduce((n, l) => ({ added: n.added + (l.kind === 'add' ? 1 : 0), deleted: n.deleted + (l.kind === 'del' ? 1 : 0) }), { added: 0, deleted: 0 });
  const identity = (path: string) => ({ path: relative(path), ...split(relative(path)), abs: `${state.workspace}/${relative(path)}` });
  const fileDiff = (path: string) => {
    const diff = scenario.diffs?.[relative(path)];
    return diff && { ...identity(path), git: true, base: { kind: 'start', sha: 'abc1234' }, status: diff.status ?? 'modified', lines: diff.lines ?? 0, binary: diff.binary, truncated: diff.truncated, hunks: diff.hunks, ...counts(diff) };
  };
  const refusal = (route: Route, path: string) => json(route, { error: outside(path) ? "engine: outside this conversation's workspace" : `engine: no such file: ${path}` }, outside(path) ? 403 : 404);
  const workView = (route: Route, action: string, arg: string | undefined, url: URL) => {
    const path = url.searchParams.get('path') ?? '';
    if (action === 'changes') {
      const only = url.searchParams.getAll('path').map(relative);
      const rows = Object.keys(scenario.diffs ?? {}).filter(p => !only.length || only.includes(p)).sort().map(p => { const d = fileDiff(p)!; return { path: d.path, name: d.name, dir: d.dir, status: d.status, added: d.added, deleted: d.deleted, binary: d.binary }; });
      return json(route, { git: true, base: { kind: 'start', sha: 'abc1234' }, branch: 'main', files: rows, added: rows.reduce((n, r) => n + r.added, 0), deleted: rows.reduce((n, r) => n + r.deleted, 0) });
    }
    if (action === 'diff') {
      if (outside(path)) return refusal(route, path);
      const diff = fileDiff(path);
      if (diff) return json(route, diff);
      return fileAt(path) ? json(route, { ...identity(path), git: true, status: 'clean', added: 0, deleted: 0, lines: 0, hunks: [] }) : refusal(route, path);
    }
    if (arg === 'find') {
      const q = (url.searchParams.get('q') ?? '').toLowerCase();
      const paths = [...new Set([...Object.keys(scenario.files ?? {}), ...Object.keys(scenario.diffs ?? {})])].map(relative).filter(p => q && p.toLowerCase().includes(q)).sort();
      return json(route, { files: paths.slice(0, Number(url.searchParams.get('limit') ?? 20)).map(p => ({ path: p, ...split(p) })) });
    }
    if (arg === 'locate') {
      if (outside(path) || !(fileAt(path) || scenario.diffs?.[relative(path)])) return refusal(route, path);
      return json(route, { path, abs: `${state.workspace}/${relative(path)}`, host: 'mock-host', local: true });
    }
    const file = fileAt(path);
    if (outside(path) || !file || file.dir) return refusal(route, path);
    const bytes = atob(file.dataBase64);
    const base = { ...identity(path), size: bytes.length, language: (path.split('.').pop() ?? '').toLowerCase() };
    if (bytes.includes('\0')) return json(route, { ...base, lines: 0, text: '', refusal: 'binary', message: `${base.name} is not text; open it in your editor.` });
    return json(route, { ...base, lines: bytes.split('\n').length - (bytes.endsWith('\n') ? 1 : 0), text: new TextDecoder().decode(Uint8Array.from(bytes, c => c.charCodeAt(0))) });
  };

  // A note is recorded on the task page as the person's own, so the next read shows it.
  const taskAction = (route: Route, taskId: string, verb: string, body: Record<string, unknown>) => {
    const taskPage = scenario.taskPages?.[taskId];
    if (taskPage && verb === 'note') taskPage.Notes = [...(taskPage.Notes ?? []), { Person: true, Body: String(body.text ?? ''), At: new Date().toISOString() }];
    return json(route, { accepted: true });
  };

  // Model roles: the choice is held here like the engine holds it in the profile.
  const chosen: Record<string, { model: string; effort?: string }> = {};
  const roleView = (role: typeof ROLES[number]) => {
    const own = chosen[role.id];
    const followed = !own && role.inherits ? chosen[role.inherits] : undefined;
    return { ...role, model: own?.model ?? followed?.model ?? MODEL, default: MODEL, effort: own?.effort ?? followed?.effort, chosen: Boolean(own) };
  };
  // Places organization choices, held like the engine holds them in the profile.
  const placesChosen: Record<string, boolean | number> = {};
  const placesView = (row: typeof PLACES_POLICY[number]) => ({ ...row, value: placesChosen[row.key] ?? row.default, chosen: row.key in placesChosen && placesChosen[row.key] !== row.default });
  const placesPolicy = (route: Route, parts: string[], method: string, body: Record<string, unknown>) => {
    if (scenario.placesPolicy === false) return json(route, { error: 'unknown route' }, 404);
    if (!parts[2]) return json(route, { settings: PLACES_POLICY.map(placesView) });
    const row = PLACES_POLICY.find(entry => entry.key === parts[2]);
    if (!row || method !== 'PUT') return json(route, { error: 'no such setting' }, 404);
    const value = body.value;
    if (value === null || value === undefined) delete placesChosen[row.key];
    else if (row.kind === 'switch' ? typeof value !== 'boolean' : !Number.isInteger(value) || (value as number) < (row as { min?: number }).min! || (value as number) > (row as { max?: number }).max!) return json(route, { error: `${row.key} is out of range` }, 400);
    else placesChosen[row.key] = value as boolean | number;
    return json(route, placesView(row));
  };
  let pins = [GLM_FLASH, MODEL, GLM];
  let pinsChosen = false;
  const pinsView = () => ({ pinned: pins.map(id => ({ id, label: PIN_LABELS[id] ?? id.slice(id.lastIndexOf('/') + 1) })), chosen: pinsChosen });
  const models = (route: Route, parts: string[], method: string, body: Record<string, unknown>) => {
    const listed = scenario.models ?? [{ id: MODEL, name: 'DeepSeek V4.1 Flash' }];
    if (!parts[1]) return json(route, { models: listed });
    if (parts[1] === 'pinned') {
      if (method === 'PUT') {
        const ids = (body.models ?? []) as string[];
        if (ids.length && (ids.length !== 3 || new Set(ids).size !== 3 || ids.some(id => !listed.some(row => row.id === id)))) return json(route, { error: 'pin three different models' }, 400);
        pinsChosen = ids.length > 0;
        pins = ids.length ? ids : [GLM_FLASH, MODEL, GLM];
      }
      return json(route, pinsView());
    }
    if (!parts[2]) return json(route, { default: MODEL, roles: ROLES.map(roleView), categories: ROLE_CATEGORIES });
    const role = ROLES.find(row => row.id === parts[2]);
    if (!role || method !== 'PUT') return json(route, { error: 'unknown model role' }, 404);
    const model = String(body.model ?? '');
    if (model && !listed.some(row => row.id === model)) return json(route, { error: 'that model is not on the list' }, 400);
    if (model) chosen[parts[2]] = { model, effort: String(body.effort ?? '') || undefined }; else delete chosen[parts[2]];
    // Only the Conversation role moves the open chat, as the bridge does.
    if (parts[2] === 'conversation') { state = { ...state, model: model || MODEL }; publish(); }
    return json(route, roleView(role));
  };

  const workspaces = new Map<string, { key: string; revision: number; workspace: unknown }>();
  const history = historyRoutes(scenario.history, json);

  await page.route('**/api/engine/**', async route => {
    const request = route.request();
    const url = new URL(request.url());
    const method = request.method();
    const parts = url.pathname.replace(/^.*\/api\/engine/, '').split('/').filter(Boolean).map(decodeURIComponent);
    const body = (method === 'POST' || method === 'PUT') && request.postData() ? JSON.parse(request.postData()!) as Record<string, unknown> : {};
    const [root, id, action, arg] = parts;
    // Place, chat, world and workspace reads are not a conversation's calls. A Places
    // policy write is a Settings save, so it stays on the list the settings specs read.
    const policyWrite = root === 'places' && id === 'policy' && method !== 'GET';
    if (policyWrite || !['places', 'chats', 'world', 'events', 'workspaces'].includes(root)) calls.push({ method, path: url.pathname, body });
    if (offline) return offline === '503' ? json(route, { error: 'engine unreachable' }, 503) : route.abort('connectionrefused');
    const forced = (key: keyof NonNullable<Scenario['fail']>) => {
      const status = scenario.fail?.[key];
      return status ? json(route, { error: `Mock engine forced ${key} failure` }, status) : undefined;
    };
    if (world && root === 'world') return json(route, { seq: worldSeq, ...structuredClone(world) });
    if (root === 'events') return (world ? worldEvents : sessionEvents)(route, Number(url.searchParams.get('after') ?? 0));
    // Fixture workspace CAS mirrors the real route; workspace reads never count as conversation calls.
    if (root === 'workspaces' && id) {
      const current = workspaces.get(id) ?? { key: id, revision: 0, workspace: null };
      if (method === 'PUT') {
        if (body.revision !== current.revision) return json(route, { error: 'Tabs changed in another window', code: 'conflict', current }, 409);
        const saved = { key: id, revision: current.revision + 1, workspace: body.workspace };
        workspaces.set(id, saved);
        return json(route, saved);
      }
      if (url.searchParams.get('wait')) await new Promise(resolve => setTimeout(resolve, 250));
      return json(route, workspaces.get(id) ?? current);
    }
    if (root === 'models') return models(route, parts, method, body);
    if (root === 'places' && parts[1] === 'policy') return placesPolicy(route, parts, method, body);
    if (root === 'history') return history.handle(route, parts, method, body, url);
    if (root !== 'sessions') return json(route, { error: 'unknown route' }, 404);
    if (!id) {
      const refused = forced('create');
      if (refused) return refused;
      // Attaching a saved conversation keeps that file. Replacing it with the mock's
      // default made a second Continue look like a different chat and open a duplicate tab.
      const requested = typeof body.sessionFile === 'string' ? body.sessionFile : '';
      const titled = history.titleOf(body.sessionFile);
      if (requested || titled) state = { ...state, ...(requested ? { sessionFile: requested } : {}), ...(titled ? { title: titled } : {}) };
      // Attach is how a window comes back after the stream drops. A raw snapshot
      // put the over-cap body back, so opening the call never fetched it.
      return json(route, snapshotView(null));
    }
    if (id !== state.id) return json(route, { error: 'reattach this conversation' }, 404);
    if (!action) {
      const since = url.searchParams.get('since');
      if (since !== null && (!/^[+-]?\d+$/.test(since) || !Number.isSafeInteger(Number(since)))) return json(route, { error: 'since must be an entry count' }, 400);
      return forced('read') ?? json(route, snapshotView(since));
    }
    if (action === 'events') return forced('events') ?? events(route, Number(url.searchParams.get('after') ?? 0));
    if (action === 'turn') return forced('turn') ?? turn(route, body);
    if (action === 'queue-send') return forced('queue') ?? queueSend(route, body);
    if (action === 'queue-edit' || action === 'queue-move' || action === 'queue-remove') return forced('queue') ?? queueAction(route, action, body);
    if (action === 'stop') {
      if (scenario.fail?.stop) return forced('stop');
      pending = undefined;
      queued = [];
      update({ running: false, queue: [] });
      return json(route, { accepted: true });
    }
    if (action === 'answer') {
      if (scenario.fail?.answer) return forced('answer');
      answer(body);
      return json(route, { accepted: true });
    }
    if (action === 'questions' && arg === 'hold') return json(route, { accepted: true });
    if (action === 'favicon') return json(route, {});
    if (action === 'changes' || action === 'diff' || (action === 'files' && ['text', 'find', 'locate'].includes(arg ?? ''))) return workView(route, action, arg, url);
    if (action === 'terminals') return terminals(route, parts, body, url);
    if (action === 'jobs') return jobs(route, parts);
    if (action === 'files') return files(route, arg, body, url);
    if (action === 'tasks' && arg && parts[4]) return forced('task') ?? taskAction(route, arg, parts[4], body);
    if (action === 'tasks' && arg) {
      const taskPage = scenario.taskPages?.[arg];
      return taskPage ? json(route, taskPage) : json(route, { error: `no task ${arg}` }, 404);
    }
    if (action === 'tools' && arg) {
      const result = scenario.tools?.[arg];
      return result ? json(route, result) : json(route, { error: `no tool result ${arg}` }, 404);
    }
    return json(route, { error: 'unknown action' }, 404);
  });

  const setWorld: MockEngine['setWorld'] = next => {
    if (!world) throw new Error('setWorld needs scenario.world');
    world = { rows: next.rows ?? world.rows, items: next.items ?? world.items, jobs: next.jobs ?? world.jobs };
    worldSeq += 1;
  };

  return {
    calls, turnModels, history: { archived: history.archived }, snapshot: () => state, advance, update, setWorld,
    /** Push one live event, the way a running turn would. */
    push: emitEvent,
    /** Replace one file's diff so the next GET /diff answers with it. */
    replaceDiff: (path: string, diff: MockDiff) => { scenario.diffs = { ...scenario.diffs, [path]: diff }; },
    /** Replace one file's bytes and stat, the way a shell command or another editor would, with no event. */
    replaceFile: (path: string, file: MockFile) => { scenario.files = { ...scenario.files, [path]: file }; },
    setOffline: (on, how = 'refused') => { offline = on ? how : undefined; },
    retrying,
    compacting: () => notice('compacting'),
    compacted: () => notice('compacted'),
  };
}
