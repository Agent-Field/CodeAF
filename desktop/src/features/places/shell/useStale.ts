// The window's reading of the untouched-place suggestion (design 6d), shared by the All places Home and the Go to chooser.
//
// The engine decides which places are due and remembers "Not now"; this hook only asks. It reads when the window's graph
// reading moves (a write here, a change on the world stream, focus coming back), so a snooze, an archive or a merge made in
// any window is gone from the line here as soon as the graph is read again. A failed or absent route is "no suggestion":
// the line is left out, never drawn broken.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { createStaleClient, type StaleClient, type StalePlace } from '../stale-client';
import { staleSuggestion } from '../stale-model';
import type { PlacesShell } from './PlacesShell';

const shared = createStaleClient();
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
