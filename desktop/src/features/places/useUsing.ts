import { useCallback, useEffect, useRef, useState } from 'react';
import { PlacesError } from './client.ts';
import type { PolicyField, UsingApi, UsingView } from './using-types.ts';

/**
 * Where the Using list stands. 'absent' is a conversation that has no list to read (no api given, no token yet, or an engine
 * whose bridge has no such route): the capability is left off rather than drawn broken. A failure is never folded into
 * 'absent' once the engine has answered the route, and it never replaces a list already read: the last good one stays visible
 * beside the failure so a flicker in the connection does not erase what the model is using.
 */
export type UsingState =
  | { phase: 'absent' }
  | { phase: 'loading'; view?: undefined }
  | { phase: 'ready'; view: UsingView }
  | { phase: 'failed'; message: string; unreachable: boolean; view?: UsingView };

export type UsingControl = {
  state: UsingState;
  /** The field a write is in flight for, so only its own controls wait. */
  busy: PolicyField | null;
  /** The engine's sentence for the last refused or failed write; stays until the person acts again or dismisses it. */
  writeError: string | null;
  dismissWriteError: () => void;
  reload: () => void;
  choose: (field: PolicyField, placeId: string) => Promise<boolean>;
  apply: (field: PolicyField) => Promise<boolean>;
};

const sentence = (reason: unknown) => (reason instanceof Error && reason.message ? reason.message : 'The engine did not answer.');
const routeMissing = (reason: unknown) => reason instanceof PlacesError && reason.status === 404 && reason.code === '';

/** Reads and writes one conversation's Using list. `token` is the bridge's session token; the chat id comes back in the view. */
export function useUsing(api: UsingApi | undefined, token: string | undefined, refreshKey: unknown = undefined): UsingControl {
  const [state, setState] = useState<UsingState>(api && token ? { phase: 'loading' } : { phase: 'absent' });
  const [busy, setBusy] = useState<PolicyField | null>(null);
  const [writeError, setWriteError] = useState<string | null>(null);
  const [generation, setGeneration] = useState(0);
  const last = useRef<UsingView | undefined>(undefined);

  /** A late answer never overwrites a newer one: the revision only moves forward for one conversation. */
  const take = useCallback((view: UsingView) => {
    const held = last.current;
    if (held && held.chatId === view.chatId && view.revision < held.revision) return;
    last.current = view;
    setState({ phase: 'ready', view });
  }, []);

  useEffect(() => {
    last.current = undefined;
    setWriteError(null);
    if (!api || !token) { setState({ phase: 'absent' }); return; }
    setState({ phase: 'loading' });
  }, [api, token]);

  useEffect(() => {
    if (!api || !token) return;
    const controller = new AbortController();
    api.using(token, controller.signal).then(take, (reason: unknown) => {
      if (controller.signal.aborted) return;
      const held = last.current;
      if (!held && routeMissing(reason)) { setState({ phase: 'absent' }); return; }
      setState({ phase: 'failed', message: sentence(reason), unreachable: reason instanceof PlacesError && reason.unreachable, view: held });
    });
    return () => controller.abort();
  }, [api, token, refreshKey, generation, take]);

  const write = useCallback(async (field: PolicyField, call: (api: UsingApi, token: string) => Promise<UsingView>) => {
    if (!api || !token) return false;
    setBusy(field);
    setWriteError(null);
    try {
      take(await call(api, token));
      return true;
    } catch (reason) {
      // A refusal (no conflict left, nothing to apply) means the list changed under the person: read it again, keep the sentence.
      setWriteError(sentence(reason));
      if (reason instanceof PlacesError && reason.status >= 400 && reason.status < 500) setGeneration(value => value + 1);
      return false;
    } finally {
      setBusy(null);
    }
  }, [api, token, take]);

  return {
    state, busy, writeError,
    dismissWriteError: () => setWriteError(null),
    reload: () => setGeneration(value => value + 1),
    choose: (field, placeId) => write(field, (client, id) => client.choose(id, field, placeId)),
    apply: field => write(field, (client, id) => client.apply(id, field)),
  };
}
