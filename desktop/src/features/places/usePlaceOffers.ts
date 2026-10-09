import { useEffect, useRef, useState } from 'react';
import { PlacesError } from './client';
import { proposalsClient, type PlaceProposal } from './proposals-client';
import { usePlacesShell } from './shell/PlacesShell';

/** Reads the ledger after graph/world changes, and while an already-running pass settles. No model call on render. */
export function usePlaceOffers(enabled: boolean, scope: string, awaitFirstOffer = false, active = true) {
  const shell = usePlacesShell();
  const version = shell?.places.version ?? 0;
  const [reading, setReading] = useState<{ scope: string; offers: PlaceProposal[] }>({ scope, offers: [] });
  const [attempt, setAttempt] = useState(0);
  const waiting = useRef({ scope, until: Date.now() + 90_000 });
  if (waiting.current.scope !== scope) waiting.current = { scope, until: Date.now() + 90_000 };
  useEffect(() => {
    if (!enabled || !active || !shell) return;
    const abort = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const read = async () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'hidden') { timer = setTimeout(() => void read(), 10_000); return; }
      try {
        const result = await proposalsClient.read(abort.signal);
        if (abort.signal.aborted) return;
        setReading({ scope, offers: result.proposals });
        if (result.organizing || (awaitFirstOffer && !result.proposals.some(p => p.kind === 'file' && p.chatIds?.includes(scope)) && Date.now() < waiting.current.until)) timer = setTimeout(() => void read(), 2000);
        else if (scope === 'root') timer = setTimeout(() => void read(), 10_000);
      } catch (failure) {
        if (abort.signal.aborted) return;
        setReading({ scope, offers: [] });
        // Older engines do not advertise this optional capability. Other failures remain explicit on interaction.
        if (!(failure instanceof PlacesError && (failure.status === 404 || failure.unreachable))) shell.warn(failure);
      }
    };
    void read();
    return () => { abort.abort(); clearTimeout(timer); };
  }, [enabled, active, scope, version, attempt, awaitFirstOffer, shell?.client]);
  return { offers: enabled && reading.scope === scope ? reading.offers : [], refresh: () => setAttempt(n => n + 1) };
}
