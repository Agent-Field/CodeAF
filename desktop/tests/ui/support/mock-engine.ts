import type { Page, Route } from '@playwright/test';
import type { EngineEntry, EngineEvent, EngineFile, EngineFileDiff, EngineSnapshot, EngineTaskPage, TerminalInfo } from '../../../src/features/chat/engine-client';

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

/** A changed file the mock reports through GET /changes and /diff; counts derive from the hunks. */
export type MockDiff = Pick<EngineFileDiff, 'hunks'> & Partial<Pick<EngineFileDiff, 'status' | 'lines' | 'binary' | 'truncated'>>;
/** A terminal or job the mock engine already holds; `output` is its kept log (raw terminal text). */
export type MockTerminal = Partial<TerminalInfo> & { id: string; output?: string };

export type Scenario = {
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

  const relative = (path: string) => path.replace(`${state.workspace}/`, '');
  const outside = (path: string) => path.startsWith('/') && !path.startsWith(`${state.workspace}/`);
  const split = (path: string) => { const at = path.lastIndexOf('/'); return at < 0 ? { name: path, dir: '' } : { name: path.slice(at + 1), dir: path.slice(0, at) }; };
  const counts = (diff: MockDiff) => diff.hunks.flatMap(h => h.lines).reduce((n, l) => ({ added: n.added + (l.kind === 'add' ? 1 : 0), deleted: n.deleted + (l.kind === 'del' ? 1 : 0) }), { added: 0, deleted: 0 });
  const identity = (path: string) => ({ path: relative(path), ...split(relative(path)), abs: `${state.workspace}/${relative(path)}` });
  const fileDiff = (path: string) => {
    const diff = scenario.diffs?.[relative(path)];
    return diff && { ...identity(path), git: true, status: diff.status ?? 'modified', lines: diff.lines ?? 0, binary: diff.binary, truncated: diff.truncated, hunks: diff.hunks, ...counts(diff) };
  };
  const refusal = (route: Route, path: string) => json(route, { error: outside(path) ? "engine: outside this conversation's workspace" : `engine: no such file: ${path}` }, outside(path) ? 403 : 404);
  const workView = (route: Route, action: string, arg: string | undefined, url: URL) => {
    const path = url.searchParams.get('path') ?? '';
    if (action === 'changes') {
      const only = url.searchParams.getAll('path').map(relative);
      const rows = Object.keys(scenario.diffs ?? {}).filter(p => !only.length || only.includes(p)).sort().map(p => { const d = fileDiff(p)!; return { path: d.path, name: d.name, dir: d.dir, status: d.status, added: d.added, deleted: d.deleted, binary: d.binary }; });
      return json(route, { git: true, base: 'abc1234', branch: 'main', files: rows, added: rows.reduce((n, r) => n + r.added, 0), deleted: rows.reduce((n, r) => n + r.deleted, 0) });
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
