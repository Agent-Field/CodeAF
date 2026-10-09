import type { Page, Route } from '@playwright/test';
import type { EngineEntry, EngineEvent, EngineFile, EngineSnapshot, EngineTaskPage } from '../../../src/features/chat/engine-client';

export const MODEL = 'deepseek/deepseek-v4.1-flash';

export type StreamRecord = { seq: number; type: 'snapshot' | 'event'; snapshot?: EngineSnapshot; event?: EngineEvent };

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
export type MockFile = Omit<EngineFile, 'name' | 'size' | 'hash'> & { dir?: boolean };

export type Scenario = {
  /** State returned by POST /sessions and GET /sessions/{id}. */
  initial: Partial<EngineSnapshot>;
  /** One element is consumed per accepted turn; the last one repeats. */
  turns?: ScriptedTurn[];
  taskPages?: Record<string, EngineTaskPage>;
  tools?: Record<string, { output: string; full: boolean }>;
  /** Workspace files by path, relative or absolute; anything else stats as missing. */
  files?: Record<string, MockFile>;
  /** When true a turn only records the message; call engine.advance() to apply the reply. */
  manual?: boolean;
  /** Models the provider offers; defaults to the one default model. */
  models?: { id: string; name: string; efforts?: string[] }[];
  /** Forced HTTP failure per endpoint, e.g. { turn: 409 }. */
  fail?: Partial<Record<'create' | 'read' | 'turn' | 'stop' | 'answer' | 'events' | 'task', number>>;
};

export type Call = { method: string; path: string; body: Record<string, unknown> };

export type MockEngine = {
  calls: Call[];
  /** The conversation model in force at the moment each accepted turn arrived. */
  turnModels: string[];
  snapshot: () => EngineSnapshot;
  /** Apply the next scripted turn reply (for scenarios with manual: true). */
  advance: () => void;
  /** Merge fields into the snapshot and publish a snapshot record to stream readers. */
  update: (patch: Partial<EngineSnapshot>) => void;
};

/** Names and one-line jobs of the roles, as the engine reports them. */
const ROLES = [
  ['conversation', 'Conversation', 'Answers what you type in a chat and decides when to start a task.'],
  ['tasks', 'Tasks', 'Does the steps of a task: reading, editing, running commands.'],
  ['naming', 'Titles and summaries', 'Writes the short names for chats, tasks and background jobs, and the one-line step captions.'],
];

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
  const calls: Call[] = [];
  const turnModels: string[] = [];
  let turnIndex = 0;
  let pending: ScriptedTurn | undefined;
  let closed = false;
  page.on('close', () => { closed = true; });

  const publish = () => {
    state = { ...state, seq: state.seq + 1, updatedAt: new Date().toISOString() };
    log.push({ seq: state.seq, type: 'snapshot', snapshot: structuredClone(state) });
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
  // A queued message is recorded as the person's next message once the turn ends.
  let queued: EngineEntry[] = [];
  const apply = (turn: ScriptedTurn | undefined) => {
    turn?.stream?.forEach(emit);
    turn?.events?.forEach(emitEvent);
    const entries = [...state.entries, ...(turn?.entries ?? []), ...queued];
    queued = [];
    state = { ...state, ...turn?.patch, running: false, entries };
    publish();
  };
  const advance = () => {
    apply(pending);
    pending = undefined;
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
      queued.push({ Role: 'user', Text: text });
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

  // Answering records the decision the way the engine's recentOutcomes reports it.
  const answer = (body: Record<string, unknown>) => {
    const asked = state.questions?.find(q => q.id === body.id && q.kind === body.kind);
    const words = asked?.options?.find(o => o.key === body.key)?.label ?? String(body.key ?? '');
    const outcome = { kind: String(body.kind), token: String(body.ref || body.id), head: asked?.head, outcome: 'decided', words, by: String(body.decidedBy ?? 'person'), at: new Date().toISOString() };
    const questions = (state.questions ?? []).filter(q => q !== asked);
    const before = (state as { recentOutcomes?: unknown[] }).recentOutcomes ?? [];
    update({ needsPerson: questions.length > 0, questions, recentOutcomes: [outcome, ...before] } as Partial<EngineSnapshot>);
  };

  const fileAt = (path: string) => scenario.files?.[path] ?? scenario.files?.[path.replace(`${state.workspace}/`, '')];
  const stat = (path: string) => {
    const file = fileAt(path);
    const outside = path.startsWith('/') && !path.startsWith(`${state.workspace}/`);
    return { path, exists: Boolean(file), dir: Boolean(file?.dir), size: file ? Math.floor(file.dataBase64.length * 0.75) : 0, outside };
  };
  const files = (route: Route, arg: string | undefined, body: Record<string, unknown>, url: URL) => {
    if (arg === 'stat') return json(route, ((body.paths as string[]) ?? []).map(stat));
    const path = url.searchParams.get('path') ?? '';
    const file = fileAt(path);
    if (!file || file.dir) return json(route, { error: `no file ${path}` }, 404);
    const name = path.split('/').pop() ?? path;
    return json(route, { name, mime: file.mime, size: stat(path).size, hash: 'mock', dataBase64: file.dataBase64 });
  };

  // A note is recorded on the task page as the person's own, so the next read shows it.
  const taskAction = (route: Route, taskId: string, verb: string, body: Record<string, unknown>) => {
    const taskPage = scenario.taskPages?.[taskId];
    if (taskPage && verb === 'note') taskPage.Notes = [...(taskPage.Notes ?? []), { Person: true, Body: String(body.text ?? ''), At: new Date().toISOString() }];
    return json(route, { accepted: true });
  };

  // Model roles: the choice is held here like the engine holds it in the profile.
  const chosen: Record<string, { model: string; effort?: string }> = {};
  const roleView = ([id, name, controls]: string[]) => ({ id, name, controls, model: chosen[id]?.model ?? MODEL, default: MODEL, effort: chosen[id]?.effort, chosen: Boolean(chosen[id]) });
  const models = (route: Route, parts: string[], method: string, body: Record<string, unknown>) => {
    const listed = scenario.models ?? [{ id: MODEL, name: 'DeepSeek V4.1 Flash' }];
    if (!parts[1]) return json(route, { models: listed });
    if (!parts[2]) return json(route, { default: MODEL, roles: ROLES.map(roleView) });
    const role = ROLES.find(row => row[0] === parts[2]);
    if (!role || method !== 'PUT') return json(route, { error: 'unknown model role' }, 404);
    const model = String(body.model ?? '');
    if (model && !listed.some(row => row.id === model)) return json(route, { error: 'that model is not on the list' }, 400);
    if (model) chosen[parts[2]] = { model, effort: String(body.effort ?? '') || undefined }; else delete chosen[parts[2]];
    // Only the Conversation role moves the open chat, as the bridge does.
    if (parts[2] === 'conversation') { state = { ...state, model: model || MODEL }; publish(); }
    return json(route, roleView(role));
  };

  await page.route('**/api/engine/**', async route => {
    const request = route.request();
    const url = new URL(request.url());
    const method = request.method();
    const parts = url.pathname.replace(/^.*\/api\/engine/, '').split('/').filter(Boolean).map(decodeURIComponent);
    const body = (method === 'POST' || method === 'PUT') && request.postData() ? JSON.parse(request.postData()!) as Record<string, unknown> : {};
    calls.push({ method, path: url.pathname, body });
    const [root, id, action, arg] = parts;
    const forced = (key: keyof NonNullable<Scenario['fail']>) => {
      const status = scenario.fail?.[key];
      return status ? json(route, { error: `Mock engine forced ${key} failure` }, status) : undefined;
    };
    if (root === 'models') return models(route, parts, method, body);
    if (root !== 'sessions') return json(route, { error: 'unknown route' }, 404);
    if (!id) return forced('create') ?? json(route, state);
    if (id !== state.id) return json(route, { error: 'reattach this conversation' }, 404);
    if (!action) return forced('read') ?? json(route, state);
    if (action === 'events') return forced('events') ?? events(route, Number(url.searchParams.get('after') ?? 0));
    if (action === 'turn') return forced('turn') ?? turn(route, body);
    if (action === 'stop') {
      if (scenario.fail?.stop) return forced('stop');
      pending = undefined;
      update({ running: false });
      return json(route, { accepted: true });
    }
    if (action === 'answer') {
      if (scenario.fail?.answer) return forced('answer');
      answer(body);
      return json(route, { accepted: true });
    }
    if (action === 'questions' && arg === 'hold') return json(route, { accepted: true });
    if (action === 'favicon') return json(route, {});
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

  return { calls, turnModels, snapshot: () => state, advance, update };
}
