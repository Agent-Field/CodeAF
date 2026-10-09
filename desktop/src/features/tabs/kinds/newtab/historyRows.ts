// The "From history" rows of the new-tab field (design 4c): pure, so a node test covers the ranking and the words.
// Every word on a row is the engine's own (a title, a decision, the best answer, a snippet); nothing is summarised here.
import type { HistoryItem, SearchResult } from '../../../history/types.ts';

/** Conversations the section lists before "See all"; the design (Shell 4c) shows two. */
export const fromHistoryLimit = 2;

/** One conversation the search found, in the engine's order. `item` is known for the best match; the rest are read on demand. */
export type HistoryCandidate = { id: string; line: string; item?: HistoryItem };
/** A candidate whose conversation has been read, so Enter has something to open. */
export type HistoryMatch = { id: string; line: string; item: HistoryItem };
export type HistoryMatches = { query: string; rows: HistoryMatch[]; total: number };

const oneLine = (text: string) => text.replace(/\s+/g, ' ').trim();

/**
 * The conversations a search found, best first, then in the order the engine ranked its discussed hits, then any that only a
 * decision or a task found. `total` is how many conversations the engine said matched (its discussed total can exceed the 20
 * hits it returns), and never less than the distinct conversations actually returned.
 */
export function rankConversations(result: SearchResult): { candidates: HistoryCandidate[]; total: number } {
  const decided = new Map<string, string>();
  for (const hit of result.decisions) if (!decided.has(hit.id) && oneLine(hit.title)) decided.set(hit.id, `decided: ${oneLine(hit.title)}`);
  const seen = new Set<string>();
  const candidates: HistoryCandidate[] = [];
  const add = (id: string, fallback: string, item?: HistoryItem) => {
    if (!id || seen.has(id)) return;
    seen.add(id);
    candidates.push({ id, line: decided.get(id) ?? oneLine(fallback), item });
  };
  if (result.best) add(result.best.item.id, result.best.answer, result.best.item);
  for (const hit of result.discussed) add(hit.id, hit.snippet);
  for (const hit of result.decisions) add(hit.id, '');
  for (const hit of result.tasks) add(hit.id, '');
  return { candidates, total: Math.max(result.counts.discussed, candidates.length) };
}

/** Candidates whose conversation could be read, in order, capped at the section's size. */
export const matchesOf = (query: string, candidates: readonly HistoryCandidate[], items: ReadonlyMap<string, HistoryItem>, total: number): HistoryMatches => ({
  query,
  rows: candidates.flatMap(one => { const item = one.item ?? items.get(one.id); return item ? [{ id: one.id, line: one.line, item }] : []; }).slice(0, fromHistoryLimit),
  total,
});

/** What follows a row's title: the engine's line, then "archived" when the conversation is. */
export const historyDetail = (match: HistoryMatch): string | undefined => {
  const parts = [match.line, match.item.archived ? 'archived' : ''].filter(Boolean);
  return parts.length ? parts.map(part => `· ${part}`).join(' ') : undefined;
};
