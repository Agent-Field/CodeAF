// The typed client for the engine's history routes. Reads only, plus the one archive write.
import { fetchEngine } from '../chat/engine-client';
import type { ArchiveResult, HistoryDetail, HistoryFilter, HistoryMessages, HistoryPage, SearchResult } from './types';

const query = (params: Record<string, string | number | undefined>) => {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) if (value !== undefined && value !== '') search.set(key, String(value));
  const text = search.toString();
  return text ? `?${text}` : '';
};

export async function listHistory(options: { filter?: HistoryFilter; limit?: number; before?: string; signal?: AbortSignal } = {}): Promise<HistoryPage> {
  const response = await fetchEngine(`/history${query({ filter: options.filter, limit: options.limit, before: options.before })}`, { signal: options.signal });
  const page = await response.json() as Partial<HistoryPage>;
  return { total: page.total ?? 0, matching: page.matching ?? 0, items: page.items ?? [], next: page.next };
}

export async function readHistory(id: string, signal?: AbortSignal): Promise<HistoryDetail> {
  const response = await fetchEngine(`/history/${encodeURIComponent(id)}`, { signal });
  return await response.json() as HistoryDetail;
}

export async function readHistoryMessages(id: string, options: { limit?: number; before?: number; signal?: AbortSignal } = {}): Promise<HistoryMessages> {
  const response = await fetchEngine(`/history/${encodeURIComponent(id)}/messages${query({ limit: options.limit, before: options.before })}`, { signal: options.signal });
  const page = await response.json() as Partial<HistoryMessages>;
  return { total: page.total ?? 0, messages: page.messages ?? [] };
}

export async function searchHistory(text: string, filter: HistoryFilter = 'all', signal?: AbortSignal): Promise<SearchResult> {
  const response = await fetchEngine(`/history/search${query({ q: text, filter })}`, { signal });
  const found = await response.json() as Partial<SearchResult>;
  return {
    query: found.query ?? text, best: found.best, decisions: found.decisions ?? [], discussed: found.discussed ?? [], files: found.files ?? [], tasks: found.tasks ?? [],
    counts: found.counts ?? { decisions: found.decisions?.length ?? 0, discussed: found.discussed?.length ?? 0, files: found.files?.length ?? 0, tasks: found.tasks?.length ?? 0 },
  };
}

export async function archiveHistory(ids: string[], archived: boolean): Promise<ArchiveResult> {
  const response = await fetchEngine('/history/archive', { method: 'POST', body: JSON.stringify({ ids, archived }) });
  return await response.json() as ArchiveResult;
}
