import type { Route } from '@playwright/test';
import type { HistoryFile, HistoryItem, Recap } from '../../../src/features/history/types';

/** One conversation the mock History serves: what the engine stores for it (recap, journal) and its live state. */
export type MockConversation = {
  id: string;
  title: string;
  /** RFC 3339: when the person last spoke. */
  at: string;
  recap?: Recap;
  messages?: { role: 'user' | 'assistant'; text: string; at?: string }[];
  tasks?: number;
  tasksRunning?: number;
  open?: boolean;
  state?: HistoryItem['state'];
  reason?: string;
  archived?: boolean;
  taskHits?: { taskId: string; title: string; snippet: string }[];
};

export type MockHistory = {
  conversations: MockConversation[];
  /** Forced HTTP failure for the list route (the error state). */
  failList?: number;
};

export type HistoryHandle = { archived: () => { id: string; archived: boolean }[] };

const sessionFile = (id: string) => `/mock/places/${id}/transcript.jsonl`;
const stopWords = new Set('a an and are as at be but by did do does for from had has have how i if in into is it its me my of on or our so than that the their then there these they this to was we were what when where which who why will with would you your'.split(' '));
const terms = (query: string) => query.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(word => word.length > 1 && !stopWords.has(word));
const sentences = (text: string) => text.split(/(?<=[.!?])\s+/).filter(Boolean);

function itemOf(c: MockConversation): HistoryItem {
  const files: HistoryFile[] = c.recap?.files ?? [];
  return {
    id: c.id, sessionFile: sessionFile(c.id), title: c.title, line: c.recap?.line, at: c.at, messages: c.messages?.length ?? c.recap?.messages ?? 0,
    tasks: c.tasks ?? 0, tasksRunning: c.tasksRunning ?? 0, files: files.slice(0, 3), fileCount: files.length, decisions: c.recap?.decided.length ?? 0,
    state: c.state ?? 'idle', reason: c.reason, open: c.open ?? false, archived: c.archived ?? false, workspace: '/mock-workspace',
  };
}

const keep = (c: MockConversation, filter: string) => {
  if (filter === 'decisions') return (c.recap?.decided.length ?? 0) > 0;
  if (filter === 'files') return (c.recap?.files.length ?? 0) > 0;
  if (filter === 'tasks') return (c.tasks ?? 0) > 0;
  if (filter === 'open') return !!c.open;
  return true;
};

/** The History routes of the mock engine: list (paged, newest first), recap, messages, search, archive, delete and restore. */
export function historyRoutes(history: MockHistory | undefined, json: (route: Route, value: unknown, status?: number) => Promise<void>) {
  const all = () => [...(history?.conversations ?? [])].sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
  const changes: { id: string; archived: boolean }[] = [];
  const trash = new Map<string, MockConversation[]>();
  const handle = async (route: Route, parts: string[], method: string, body: Record<string, unknown>, url: URL) => {
    const [, id, action] = parts;
    if (!id && method === 'GET') {
      if (history?.failList) return json(route, { error: 'Mock history forced failure' }, history.failList);
      const rows = all();
      const filter = url.searchParams.get('filter') ?? 'all';
      const matching = rows.filter(c => keep(c, filter));
      const limit = Math.min(Number(url.searchParams.get('limit') ?? 50), 200);
      const from = Number(url.searchParams.get('before') ?? 0) || 0;
      const page = matching.slice(from, from + limit);
      return json(route, { total: rows.length, matching: matching.length, items: page.map(itemOf), next: from + limit < matching.length ? String(from + limit) : undefined });
    }
    if (id === 'archive' && method === 'POST') {
      let changed = 0;
      for (const target of (body.ids as string[]) ?? []) {
        const found = history?.conversations.find(c => c.id === target);
        if (found) { found.archived = Boolean(body.archived); changed++; changes.push({ id: target, archived: found.archived }); }
      }
      return json(route, { changed });
    }
    if (id === 'delete' && method === 'POST') {
      const ids = Array.isArray(body.ids) ? body.ids.filter((one): one is string => typeof one === 'string') : [];
      const rows = history?.conversations ?? [];
      for (const target of ids) {
        const found = rows.find(c => c.id === target);
        if (!found) continue;
        if (found.open) return json(route, { error: 'a conversation that is open cannot be deleted' }, 409);
        if (!found.archived) return json(route, { error: 'only archived conversations can be deleted' }, 409);
      }
      const moved: MockConversation[] = [];
      for (const target of ids) {
        const at = rows.findIndex(c => c.id === target);
        if (at >= 0) moved.push(...rows.splice(at, 1));
      }
      if (!moved.length) return json(route, { deleted: 0 });
      const token = `20261010T051400Z-${(trash.size + 1).toString(16).padStart(8, '0')}`;
      trash.set(token, moved);
      return json(route, { deleted: moved.length, undoToken: token });
    }
    if (id === 'restore' && method === 'POST') {
      const token = typeof body.undoToken === 'string' ? body.undoToken : '';
      const moved = trash.get(token);
      if (!moved) return json(route, { error: 'nothing to restore' }, 404);
      history?.conversations.push(...moved);
      trash.delete(token);
      return json(route, { restored: moved.length });
    }
    if (id === 'search') {
      const query = url.searchParams.get('q') ?? '';
      const filter = url.searchParams.get('filter') ?? 'all';
      const wanted = terms(query);
      const rows = all().filter(c => keep(c, filter));
      const has = (text: string) => wanted.length > 0 && wanted.some(word => text.toLowerCase().includes(word));
      // The engine's own rule (internal/desktopbridge/history.go): THE answer is one existing recap sentence that covers
      // at least 60% of the question's content words, matched by prefix. Nothing is composed.
      const share = (text: string) => {
        const tokens = text.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(Boolean);
        return wanted.length ? wanted.filter(word => tokens.some(token => token === word || (word.length >= 3 && token.startsWith(word)))).length / wanted.length : 0;
      };
      let best: unknown;
      for (const c of rows) {
        const candidates = [c.recap?.line ?? '', c.recap?.outcome ?? '', ...sentences(c.recap?.discussed ?? ''), ...(c.recap?.decided ?? []).map(d => d.text)].filter(Boolean);
        const hit = candidates.map(text => ({ text, cover: share(text) })).sort((a, b) => b.cover - a.cover)[0];
        if (hit && hit.cover >= 0.6) { best = { item: itemOf(c), answer: hit.text, messageIndex: c.messages?.findIndex(m => has(m.text)), terms: wanted }; break; }
      }
      if ((best as { messageIndex?: number } | undefined)?.messageIndex === -1) delete (best as { messageIndex?: number }).messageIndex;
      const decisions = rows.flatMap(c => (c.recap?.decided ?? []).filter(d => has(d.text) || has(c.title)).map(d => ({ id: c.id, title: d.text, context: `${c.title} · ${d.by}${d.how ? `, ${d.how}` : ''}`, at: c.at })));
      const discussed = rows.flatMap(c => (c.messages ?? []).map((m, index) => ({ m, index })).filter(({ m }) => has(m.text)).slice(0, 1).map(({ m, index }) => ({ id: c.id, title: c.title, snippet: `“…${m.text.slice(0, 80)}…”`, messageIndex: index, at: c.at })));
      const filesFound = new Map<string, { path: string; conversations: number; last: string; ids: string[] }>();
      for (const c of rows) for (const f of c.recap?.files ?? []) if (has(f.path)) {
        const prior = filesFound.get(f.path) ?? { path: f.path, conversations: 0, last: c.at, ids: [] };
        prior.conversations++; prior.ids.push(c.id); if (Date.parse(c.at) > Date.parse(prior.last)) prior.last = c.at;
        filesFound.set(f.path, prior);
      }
      const tasks = rows.flatMap(c => (c.taskHits ?? []).filter(t => has(`${t.title} ${t.snippet}`)).map(t => ({ id: c.id, taskId: t.taskId, conversationTitle: c.title, title: t.title, snippet: t.snippet, at: c.at })));
      const files = [...filesFound.values()];
      return json(route, { query, best, decisions, discussed, files, tasks, counts: { decisions: decisions.length, discussed: discussed.length, files: files.length, tasks: tasks.length } });
    }
    const found = history?.conversations.find(c => c.id === id);
    if (!found) return json(route, { error: 'no such conversation' }, 404);
    if (!action) return json(route, { item: itemOf(found), recap: found.recap, stale: false });
    if (action === 'messages') {
      const messages = (found.messages ?? []).map((m, index) => ({ index, ...m }));
      const limit = Number(url.searchParams.get('limit') ?? 120);
      const before = url.searchParams.get('before') === null ? messages.length : Number(url.searchParams.get('before'));
      const page = messages.filter(m => m.index < before).slice(-limit);
      return json(route, { total: messages.length, messages: page });
    }
    return json(route, { error: 'unknown history action' }, 404);
  };
  /** Attaching a saved conversation answers with that conversation's own title, as the engine does. */
  const titleOf = (file: unknown) => history?.conversations.find(c => file === sessionFile(c.id))?.title;
  return { handle, archived: () => changes, titleOf };
}
