// The window's reading of the untouched-place suggestion (design 6d), shared by the All places Home and the Go to chooser.
//
// The engine decides which places are due and remembers "Not now"; this hook only asks. It reads when the window's graph
// reading moves (a write here, a change on the world stream, focus coming back), so a snooze, an archive or a merge made in
// any window is gone from the line here as soon as the graph is read again. A failed or absent route is "no suggestion":
// the line is left out, never drawn broken.
//
// CROSS-WINDOW CONTRACT. A snooze is its own file and moves no graph revision and no world broadcast, so the graph reading
// cannot carry it to another window. The supported contract is therefore stated here and nowhere softer: a window shows
// another window's "Not now" (a) the moment it regains focus or becomes visible, and (b) within STALE_REREAD_MS while it is
// visible AND a line is on screen. A hidden window reads nothing, and a window with no line has nothing to hide, so there is
// no standing poll. Both are one small GET of a bounded list; neither involves a model.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { createStaleClient, type StaleClient, type StalePlace } from '../stale-client';
import { staleSuggestion } from '../stale-model';
import type { PlacesShell } from './PlacesShell';

const shared = createStaleClient();
/** How often a visible window with a line on screen asks again. Exported for the two-window test's fake clock. */
export const STALE_REREAD_MS = 30_000;
const quoted = (name: string | undefined) => (name ? `“${name}”` : 'the place');

export type StaleReading = { places: readonly StalePlace[]; snooze: (placeId: string) => Promise<void> };

export function useStale(shell: PlacesShell | undefined, client: StaleClient = shared): StaleReading {
  const version = shell?.places.version ?? 0;
  const ready = shell?.places.status === 'ready';
  const [places, setPlaces] = useState<readonly StalePlace[]>([]);
  useEffect(() => {
    if (!ready) return;
    const abort = new AbortController();
    client.list(abort.signal).then(answer => { if (!abort.signal.aborted) setPlaces(answer.places); }).catch(() => { if (!abort.signal.aborted) setPlaces([]); });
    return () => abort.abort();
  }, [client, ready, version]);
  // Settle with other windows' snoozes (see the contract above). The read keeps what it had when the engine cannot be reached.
  const showing = places.length > 0;
  useEffect(() => {
    if (!ready || typeof window === 'undefined') return;
    let abort: AbortController | undefined;
    const reread = () => {
      if (document.visibilityState === 'hidden') return;
      abort?.abort();
      const mine = abort = new AbortController();
      client.list(mine.signal).then(answer => { if (!mine.signal.aborted) setPlaces(answer.places); }).catch(() => {});
    };
    window.addEventListener('focus', reread);
    document.addEventListener('visibilitychange', reread);
    const timer = showing ? window.setInterval(reread, STALE_REREAD_MS) : undefined;
    return () => {
      abort?.abort();
      window.removeEventListener('focus', reread);
      document.removeEventListener('visibilitychange', reread);
      if (timer !== undefined) window.clearInterval(timer);
    };
  }, [client, ready, showing]);
  const refresh = shell?.refresh;
  const snooze = useCallback(async (placeId: string) => {
    await client.snooze(placeId);
    // The engine has it on disk; drop the line now and let the graph reading tell every other part of the window.
    setPlaces(before => before.filter(place => place.id !== placeId));
    await refresh?.();
  }, [client, refresh]);
  return useMemo(() => ({ places, snooze }), [places, snooze]);
}

/** The line for the first due place, with the verbs a suggestion line outside a Home page has: the same Merge and Archive the place menu has, then "Not now". */
export function staleLineFor(shell: PlacesShell, reading: StaleReading) {
  const { places, snooze } = reading;
  const name = (id: string) => shell.index?.byId.get(id)?.name;
  return staleSuggestion(places, {
    merge: id => shell.openChooser({ kind: 'merge', placeId: id, placeName: name(id) ?? 'this place' }),
    archive: async id => { await shell.write(`Archived ${quoted(name(id))}`, () => shell.client.archivePlace(id), { subject: name(id) }); },
    snooze,
  });
}
