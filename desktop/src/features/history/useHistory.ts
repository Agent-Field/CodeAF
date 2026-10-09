// The History tab's data: the paged list, one recap and a debounced search. Reads only; every request is cancellable.
import { useCallback, useEffect, useRef, useState } from 'react';
import design from '../../design/tokens.json';
import { EngineError } from '../chat/engine-client';
import { listHistory, readHistory, searchHistory } from './client';
import type { HistoryDetail, HistoryFilter, HistoryItem, SearchResult } from './types';

const pageSize = Number.parseInt(design.foundation['history-page-size'], 10);
/** How long typing waits before a search is asked: long enough to skip keystrokes, short enough to feel live. */
export const SEARCH_DELAY_MS = 220;
/** The list quietly re-reads this often while it is on screen, so an open conversation's state stays true. */
const REFRESH_MS = 20_000;

const messageOf = (error: unknown) => (error instanceof EngineError ? error.message : error instanceof Error ? error.message : 'History could not be read.');

export type ListState = { items: HistoryItem[]; total: number; matching: number; loading: boolean; more: boolean; error: string; loadMore: () => void; refresh: () => void };

export function useHistoryList(filter: HistoryFilter): ListState {
  const [items, setItems] = useState<HistoryItem[]>([]);
  const [total, setTotal] = useState(0);
  const [matching, setMatching] = useState(0);
  const [next, setNext] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const busy = useRef(false);

  // A fresh read replaces the pages loaded so far; a quiet refresh re-reads only as many rows as are loaded.
  const read = useCallback(async (signal: AbortSignal, keep: number) => {
    try {
      const page = await listHistory({ filter, limit: Math.max(pageSize, keep), signal });
      if (signal.aborted) return;
      setItems(page.items); setTotal(page.total); setMatching(page.matching); setNext(page.next); setError('');
    } catch (reason) {
      if (!signal.aborted) setError(messageOf(reason));
    } finally {
      if (!signal.aborted) setLoading(false);
    }
  }, [filter]);

  const kept = useRef(0);
  kept.current = items.length;
  const live = useRef<AbortController | null>(null);
  useEffect(() => {
    const stop = new AbortController();
    live.current = stop;
    setLoading(true);
    void read(stop.signal, 0);
    const timer = window.setInterval(() => { if (!document.hidden) void read(stop.signal, kept.current); }, REFRESH_MS);
    return () => { stop.abort(); window.clearInterval(timer); };
  }, [read]);

  const loadMore = useCallback(() => {
    if (!next || busy.current) return;
    busy.current = true;
    void listHistory({ filter, limit: pageSize, before: next })
      .then(page => { setItems(current => [...current, ...page.items.filter(item => !current.some(one => one.id === item.id))]); setNext(page.next); })
      .catch(reason => setError(messageOf(reason)))
      .finally(() => { busy.current = false; });
  }, [filter, next]);

  // After the person changes something (an archive), the rows already loaded are read again at once.
  const refresh = useCallback(() => { const stop = live.current; if (stop && !stop.signal.aborted) void read(stop.signal, kept.current); }, [read]);

  return { items, total, matching, loading, more: !!next, error, loadMore, refresh };
}

export type DetailState = { detail?: HistoryDetail; loading: boolean; error: string };

/** One conversation's recap, read when the selection settles. */
export function useHistoryDetail(id: string | undefined, revision = 0): DetailState {
  const [state, setState] = useState<DetailState>({ loading: false, error: '' });
  useEffect(() => {
    if (!id) { setState({ loading: false, error: '' }); return; }
    const stop = new AbortController();
    setState(current => ({ detail: current.detail?.item.id === id ? current.detail : undefined, loading: true, error: '' }));
    readHistory(id, stop.signal)
      .then(detail => { if (!stop.signal.aborted) setState({ detail, loading: false, error: '' }); })
      .catch(reason => { if (!stop.signal.aborted) setState({ loading: false, error: messageOf(reason) }); });
    return () => stop.abort();
  }, [id, revision]);
  return state;
}

export type SearchState = { result?: SearchResult; loading: boolean; error: string };

/** Full text and meaning in one field: asked after a short pause, and only while there is something to ask. */
export function useHistorySearch(query: string, filter: HistoryFilter): SearchState {
  const [state, setState] = useState<SearchState>({ loading: false, error: '' });
  const text = query.trim();
  useEffect(() => {
    if (!text) { setState({ loading: false, error: '' }); return; }
    const stop = new AbortController();
    setState(current => ({ result: current.result, loading: true, error: '' }));
    const timer = window.setTimeout(() => {
      searchHistory(text, filter, stop.signal)
        .then(result => { if (!stop.signal.aborted) setState({ result, loading: false, error: '' }); })
        .catch(reason => { if (!stop.signal.aborted) setState({ loading: false, error: messageOf(reason) }); });
    }, SEARCH_DELAY_MS);
    return () => { stop.abort(); window.clearTimeout(timer); };
  }, [text, filter]);
  return state;
}
