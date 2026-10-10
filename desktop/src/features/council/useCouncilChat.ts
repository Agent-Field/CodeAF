import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { chatIdFromSessionFile } from '../places/client';
import { createCouncilClient, type Council, type CouncilClient, type CouncilMessage } from './client';

/** The client every council pane talks to. A test provides a fake stream here instead of the engine. */
export const CouncilClientContext = createContext<CouncilClient>(createCouncilClient());

/** How often a running discussion is read again. An ended one is read once, because nothing more will be said. */
export const COUNCIL_POLL_MS = 1500;

/** The discussion that owns this saved session, or none: an ordinary conversation is not in the list. */
export function useCouncilOf(sessionFile: string | undefined): { council?: Council; settled: boolean } {
  const client = useContext(CouncilClientContext);
  const [found, setFound] = useState<{ file: string; council?: Council }>();
  useEffect(() => {
    if (!sessionFile) return;
    const abort = new AbortController();
    const chatId = chatIdFromSessionFile(sessionFile);
    // A list that cannot be read leaves the tab an ordinary conversation, which is what it was before this existed.
    client.list(undefined, abort.signal).then(
      ({ councils }) => setFound({ file: sessionFile, council: councils.find(c => c.chatId === chatId) }),
      () => { if (!abort.signal.aborted) setFound({ file: sessionFile }); });
    return () => abort.abort();
  }, [client, sessionFile]);
  if (!sessionFile) return { settled: true };
  return found?.file === sessionFile ? { council: found.council, settled: true } : { settled: false };
}

/** The discussion and its lines, kept current while it runs. */
export function useCouncilChat(initial: Council) {
  const client = useContext(CouncilClientContext);
  const [council, setCouncil] = useState(initial);
  const [messages, setMessages] = useState<CouncilMessage[]>([]);
  const ended = council.state === 'decided' || council.state === 'escalated';
  const endedRef = useRef(ended);
  endedRef.current = ended;

  const refresh = useCallback(async (signal?: AbortSignal) => {
    try {
      const [{ councils }, lines] = await Promise.all([client.list(undefined, signal), client.messages(initial.id, signal)]);
      const next = councils.find(c => c.id === initial.id);
      if (next) setCouncil(next);
      setMessages(lines);
    } catch { /* the next tick reads again; a failed read draws nothing new */ }
  }, [client, initial.id]);

  useEffect(() => {
    const abort = new AbortController();
    void refresh(abort.signal);
    const timer = window.setInterval(() => { if (!endedRef.current) void refresh(abort.signal); }, COUNCIL_POLL_MS);
    return () => { abort.abort(); window.clearInterval(timer); };
  }, [refresh]);

  return { council, setCouncil, messages, refresh, client, ended };
}
