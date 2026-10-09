import { useEffect, useState } from 'react';
import { readHistory, searchHistory } from '../../../history/client';
import { SEARCH_DELAY_MS } from '../../../history/useHistory';
import type { HistoryItem } from '../../../history/types';
import { fromHistoryLimit, matchesOf, rankConversations, type HistoryMatches } from './historyRows';

/**
 * What History says about the typed words: the engine's own search (no model call), asked after a pause and never for
 * an empty field. Typing aborts the request in flight, so an older answer can neither land nor replace a newer one; the
 * answer is only returned while it is for exactly what is in the field. The engine away, or nothing found, is nothing.
 */
export function useHistoryMatches(query: string, enabled: boolean): HistoryMatches | undefined {
  const [found, setFound] = useState<HistoryMatches>();
  const text = query.trim();
  useEffect(() => {
    if (!enabled || !text) { setFound(undefined); return; }
    const stop = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        const { candidates, total } = rankConversations(await searchHistory(text, 'all', stop.signal));
        const items = new Map<string, HistoryItem>();
        // Only the conversations the section will show are read; one that cannot be read leaves its row out.
        await Promise.all(candidates.slice(0, fromHistoryLimit).filter(one => !one.item).map(async one => {
          try { items.set(one.id, (await readHistory(one.id, stop.signal)).item); } catch { /* No row for a conversation that cannot be read. */ }
        }));
        if (!stop.signal.aborted) setFound(matchesOf(text, candidates, items, total));
      } catch { if (!stop.signal.aborted) setFound(undefined); }
    }, SEARCH_DELAY_MS);
    return () => { stop.abort(); window.clearTimeout(timer); };
  }, [text, enabled]);
  return found?.query === text ? found : undefined;
}
