import { useEffect, useRef } from 'react';
import { connectEngine, readEngine, type EngineSnapshot } from '../chat/engine-client';

export type SessionTarget = { id: string; sessionFile: string };

/**
 * How often an inactive tab re-reads its session. Inactive tabs READ, they never
 * hold a stream: a browser allows only six connections to one host, so six saved
 * tabs each holding an idle stream left no connection for a new tab's first send,
 * which then waited forever with its text still in the composer. Only the open
 * tab streams; every other tab costs one short request per interval.
 */
export const BACKGROUND_READ_INTERVAL_MS = 2000;

/** Inactive open tabs keep observing their sessions. Closing a tab only stops its reads. */
export function useBackgroundSessions(targets: SessionTarget[], onSnapshot: (id: string, snapshot: EngineSnapshot) => void) {
  const readers = useRef(new Map<string, AbortController>());
  const receive = useRef(onSnapshot);
  receive.current = onSnapshot;
  const key = JSON.stringify(targets);

  useEffect(() => {
    const wanted = new Set(targets.map((target) => `${target.id}:${target.sessionFile}`));
    for (const [id, controller] of readers.current) {
      if (wanted.has(id)) continue;
      controller.abort();
      readers.current.delete(id);
    }
    for (const target of targets) {
      const id = `${target.id}:${target.sessionFile}`;
      if (!readers.current.has(id)) readers.current.set(id, observe(target, receive));
    }
  }, [key]);

  useEffect(() => {
    const all = readers.current;
    return () => {
      for (const controller of all.values()) controller.abort();
      all.clear();
    };
  }, []);
}

type Receiver = { current: (id: string, snapshot: EngineSnapshot) => void };

/** Attaches once, then re-reads; a failed read attaches again on the next interval. */
function observe(target: SessionTarget, receive: Receiver): AbortController {
  const controller = new AbortController();
  let session: string | undefined;
  let seen: number | undefined;
  let timer: number | undefined;
  async function read() {
    try {
      const snapshot = session ? await readEngine(session) : await connectEngine(target.sessionFile);
      if (controller.signal.aborted) return;
      session = snapshot.id;
      // Only a changed session reaches the tab, so idle reads cost no render.
      if (snapshot.seq !== seen) receive.current(target.id, snapshot);
      seen = snapshot.seq;
    } catch {
      // Transport loss never invents a finished state; the tab reattaches when opened.
      session = undefined;
    }
    if (!controller.signal.aborted) timer = window.setTimeout(() => void read(), BACKGROUND_READ_INTERVAL_MS);
  }
  controller.signal.addEventListener('abort', () => window.clearTimeout(timer));
  void read();
  return controller;
}
