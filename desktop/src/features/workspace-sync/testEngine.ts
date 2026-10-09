// Test support only (nothing in the app imports it): an in-memory engine with the same compare-and-swap rules as
// internal/workspacestore, served through the real client over a fake transport, and a manual clock.
import { createWorkspaceClient, type WorkspaceRequest, type WorkspaceTransport } from './client.ts';
import type { Clock } from './controller.ts';

type Stored = { revision: number; writer?: string; workspace?: unknown };

export function createTestEngine() {
  const docs = new Map<string, Stored>();
  const waiters = new Set<() => void>();
  let down = false;
  /** When set, the next PUT is committed and then answered as a dropped connection (a lost acknowledgement). */
  let loseNextAnswer = false;
  const calls: { method: string; path: string }[] = [];
  const wake = () => { for (const w of [...waiters]) w(); };
  const record = (key: string) => {
    const doc = docs.get(key);
    return { key, revision: doc?.revision ?? 0, ...(doc?.writer ? { writer: doc.writer } : {}), workspace: doc?.workspace ?? null };
  };
  const transport: WorkspaceTransport = async (path: string, request: WorkspaceRequest) => {
    calls.push({ method: request.method, path });
    await Promise.resolve();
    if (down) throw Object.assign(new Error('down'), { unreachable: true });
    const url = new URL(path, 'http://engine');
    const key = url.pathname.replace('/workspaces/', '');
    if (request.method === 'GET' && url.searchParams.get('wait')) {
      const after = Number(url.searchParams.get('after'));
      if (record(key).revision === after) {
        await new Promise<void>((resolve, reject) => {
          const done = () => { waiters.delete(done); resolve(); };
          waiters.add(done);
          request.signal?.addEventListener('abort', () => { waiters.delete(done); reject(new DOMException('aborted', 'AbortError')); }, { once: true });
        });
        if (down) throw new Error('down');
      }
      return { status: 200, body: structuredClone(record(key)) };
    }
    if (request.method === 'GET') return { status: 200, body: structuredClone(record(key)) };
    const body = request.body as { revision: number; writer: string; workspace: unknown };
    const current = record(key);
    if (current.revision !== body.revision) return { status: 409, body: { error: 'these tabs changed in another window', code: 'conflict', current: structuredClone(current) } };
    const text = JSON.stringify(body.workspace);
    if (text.includes('"activeId"') || text.includes('"recentIds"') || text.includes('"focus"')) return { status: 400, body: { error: 'these tabs could not be saved: window-local field', code: 'invalid' } };
    if (current.workspace && JSON.stringify(current.workspace) === text) return { status: 200, body: structuredClone(current) };
    docs.set(key, { revision: current.revision + 1, writer: body.writer, workspace: JSON.parse(text) });
    wake();
    if (loseNextAnswer) { loseNextAnswer = false; throw new Error('connection dropped after commit'); }
    return { status: 200, body: structuredClone(record(key)) };
  };
  // A thrown transport error that is not a WorkspaceSyncError is "not running" to the client: wrap it.
  const wrapped: WorkspaceTransport = async (path, request) => {
    try { return await transport(path, request); }
    catch (error) {
      if ((error as Error).name === 'AbortError') throw error;
      const { WorkspaceSyncError } = await import('./client.ts');
      throw new WorkspaceSyncError('codeaf engine is not running', 0, '', true);
    }
  };
  return {
    client: () => createWorkspaceClient(wrapped),
    doc: (key: string) => docs.get(key),
    calls,
    setDown(value: boolean) { down = value; if (value) wake(); },
    loseNextAnswer() { loseNextAnswer = true; },
    /** Writes as a window outside the test would. */
    write(key: string, workspace: unknown) { const r = record(key); docs.set(key, { revision: r.revision + 1, writer: 'outside', workspace }); wake(); },
  };
}

/** A clock that only moves when the test moves it. */
export function createManualClock() {
  let now = 0;
  let seq = 0;
  const timers = new Map<number, { at: number; run: () => void }>();
  const clock: Clock = {
    set(run, ms) { const id = ++seq; timers.set(id, { at: now + ms, run }); return id; },
    clear(timer) { timers.delete(timer as number); },
  };
  const drain = async () => { for (let i = 0; i < 20; i++) await new Promise<void>(resolve => setTimeout(resolve, 0)); };
  return {
    clock,
    pendingTimers: () => [...timers.values()].map(t => t.at - now),
    /** Runs every timer due within `ms`, in order, letting promises settle between them. */
    async advance(ms = 0) {
      const until = now + ms;
      await drain();
      for (;;) {
        const due = [...timers.entries()].filter(([, t]) => t.at <= until).sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        timers.delete(due[0]);
        now = Math.max(now, due[1].at);
        due[1].run();
        await drain();
      }
      now = until;
      await drain();
    },
  };
}
