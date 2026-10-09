import type { Page, Route } from '@playwright/test';
import type { EngineEntry, EngineEvent, EngineSnapshot, EngineTaskPage } from '../../../src/features/chat/engine-client';

export const MODEL = 'deepseek/deepseek-v4.1-flash';

export type StreamRecord = { seq: number; type: 'snapshot' | 'event'; snapshot?: EngineSnapshot; event?: EngineEvent };

/** What one scripted turn does after the person's message is recorded. */
export type ScriptedTurn = {
  entries?: EngineEntry[];
  patch?: Partial<EngineSnapshot>;
  /** Text deltas streamed (as `text` events) before the final snapshot. */
  stream?: string[];
};

export type Scenario = {
  /** State returned by POST /sessions and GET /sessions/{id}. */
  initial: Partial<EngineSnapshot>;
  /** One element is consumed per accepted turn; the last one repeats. */
  turns?: ScriptedTurn[];
  taskPages?: Record<string, EngineTaskPage>;
  tools?: Record<string, { output: string; full: boolean }>;
  /** When true a turn only records the message; call engine.advance() to apply the reply. */
  manual?: boolean;
  /** Forced HTTP failure per endpoint, e.g. { turn: 409 }. */
  fail?: Partial<Record<'create' | 'read' | 'turn' | 'stop' | 'answer' | 'events', number>>;
};

export type Call = { method: string; path: string; body: Record<string, unknown> };

export type MockEngine = {
  calls: Call[];
  snapshot: () => EngineSnapshot;
  /** Apply the next scripted turn reply (for scenarios with manual: true). */
  advance: () => void;
  /** Merge fields into the snapshot and publish a snapshot record to stream readers. */
  update: (patch: Partial<EngineSnapshot>) => void;
};

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
  let turnIndex = 0;
  let pending: ScriptedTurn | undefined;
  let closed = false;
  page.on('close', () => { closed = true; });

  const publish = () => {
    state = { ...state, seq: state.seq + 1, updatedAt: new Date().toISOString() };
    log.push({ seq: state.seq, type: 'snapshot', snapshot: structuredClone(state) });
  };
  const emit = (text: string) => {
    state = { ...state, seq: state.seq + 1 };
    const event: EngineEvent = { kind: 'text', text, tool: '', hint: '', raw: {} };
    log.push({ seq: state.seq, type: 'event', event });
  };
  const update = (patch: Partial<EngineSnapshot>) => {
    state = { ...state, ...patch };
    publish();
  };
  const nextTurn = (): ScriptedTurn | undefined => {
    const turns = scenario.turns ?? [];
    return turns[Math.min(turnIndex++, turns.length - 1)];
  };
  const apply = (turn: ScriptedTurn | undefined) => {
    turn?.stream?.forEach(emit);
    state = { ...state, ...turn?.patch, running: false, entries: [...state.entries, ...(turn?.entries ?? [])] };
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
    state = { ...state, running: true, entries: [...state.entries, { Role: 'user', Text: String(body.text ?? '') }] };
    publish();
    if (scenario.manual) pending = nextTurn();
    else apply(nextTurn());
    return json(route, { accepted: true });
  };

  await page.route('**/api/engine/**', async route => {
    const request = route.request();
    const url = new URL(request.url());
    const method = request.method();
    const parts = url.pathname.replace(/^.*\/api\/engine/, '').split('/').filter(Boolean).map(decodeURIComponent);
    const body = method === 'POST' && request.postData() ? JSON.parse(request.postData()!) as Record<string, unknown> : {};
    calls.push({ method, path: url.pathname, body });
    const [root, id, action, arg] = parts;
    const forced = (key: keyof NonNullable<Scenario['fail']>) => {
      const status = scenario.fail?.[key];
      return status ? json(route, { error: `Mock engine forced ${key} failure` }, status) : undefined;
    };
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
      update({ needsPerson: false, questions: [] });
      return json(route, { accepted: true });
    }
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

  return { calls, snapshot: () => state, advance, update };
}
