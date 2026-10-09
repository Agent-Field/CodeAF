// The words and the one-line shape of the untouched-place suggestion (design 6d). Pure and React-free so node tests
// pin the wording. The DATES are the engine's: this file turns the numbers it sent into a sentence and never reads a
// clock, so what a person sees can only disagree with the engine's rule by being a different sentence.

import type { StalePlace } from './stale-client.ts';

/** One button on the suggestion line. */
export type SuggestionAction = { id: string; label: string; onSelect: () => void | Promise<void>; disabled?: boolean };

/** What the shared suggestion line draws: one quiet sentence and its verbs. */
export type SuggestionModel = { text: string; actions: readonly SuggestionAction[] };

const plural = (count: number, one: string, many = `${one}s`) => `${count} ${count === 1 ? one : many}`;

/** "“Launch week” hasn’t been touched in 75 days". The count is whole days, as the engine counted them. */
export function staleText(place: Pick<StalePlace, 'name' | 'daysUntouched'>): string {
  return `“${place.name}” hasn’t been touched in ${plural(place.daysUntouched, 'day')}`;
}

/** The verbs the owner has wired, in the design's order: Merge, Archive, Not now. A verb without a handler has no button. */
export function staleActions(place: Pick<StalePlace, 'id'>, verbs: {
  merge?: (placeId: string) => void | Promise<void>;
  archive?: (placeId: string) => void | Promise<void>;
  snooze?: (placeId: string) => void | Promise<void>;
}): SuggestionAction[] {
  const out: SuggestionAction[] = [];
  if (verbs.merge) out.push({ id: 'merge', label: 'Merge', onSelect: () => verbs.merge?.(place.id) });
  if (verbs.archive) out.push({ id: 'archive', label: 'Archive', onSelect: () => verbs.archive?.(place.id) });
  if (verbs.snooze) out.push({ id: 'not-now', label: 'Not now', onSelect: () => verbs.snooze?.(place.id) });
  return out;
}

/** The line for the first due place, or nothing: no place due, or no verb to offer (the emptiness law; a line that cannot act is not drawn). */
export function staleSuggestion(places: readonly StalePlace[], verbs: Parameters<typeof staleActions>[1]): (SuggestionModel & { placeId: string }) | undefined {
  const first = places[0];
  if (!first) return undefined;
  const actions = staleActions(first, verbs);
  // "Not now" alone would hide a suggestion nobody can act on; the line needs a real way to tidy.
  if (!actions.some(action => action.id === 'merge' || action.id === 'archive')) return undefined;
  return { placeId: first.id, text: staleText(first), actions };
}
