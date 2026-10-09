import { useEffect, useState } from 'react';
import type { EffectiveModel, PlacesClient } from '../client';

/** What Home knows about the model a new chat here would run on: still asking, could not ask, or the engine's answer. */
export type EffectiveModelRead = { kind: 'loading' } | { kind: 'unknown' } | { kind: 'known'; say: EffectiveModel };

/**
 * Asks the engine which model the first message on this place's Home would run on, under the same place rule the engine
 * applies when the chat opens. It re-reads when the place changes, when the places did (`revision`) and when the engine
 * comes back, so the chip never keeps naming a model the places stopped deciding. A failed read is `unknown`, which the
 * composer shows as no model at all rather than guessing one.
 */
export function useEffectiveModel(client: PlacesClient, placeId: string, revision: number | undefined, offline: boolean | undefined): EffectiveModelRead {
  const [read, setRead] = useState<{ placeId: string; value: EffectiveModelRead }>();
  useEffect(() => {
    if (offline) return;
    const controller = new AbortController();
    client.effectiveModel(placeId, controller.signal)
      .then(say => { if (!controller.signal.aborted) setRead({ placeId, value: { kind: 'known', say } }); })
      .catch(() => { if (!controller.signal.aborted) setRead({ placeId, value: { kind: 'unknown' } }); });
    return () => controller.abort();
  }, [client, placeId, revision, offline]);
  // Another place's answer is never shown for this one, and an offline engine can say nothing.
  if (offline) return { kind: 'unknown' };
  return read && read.placeId === placeId ? read.value : { kind: 'loading' };
}
