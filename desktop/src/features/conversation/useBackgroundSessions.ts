import { useEffect, useRef } from 'react';
import { connectEngine, watchEngine, type EngineSnapshot } from '../chat/engine-client';

export type SessionTarget = { id: string; sessionFile: string };

/** Inactive open tabs keep observing their sessions. Closing a tab only aborts its reader. */
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

function observe(target: SessionTarget, receive: Receiver): AbortController {
  const controller = new AbortController();
  const deliver = (snapshot: EngineSnapshot) => {
    if (!controller.signal.aborted) receive.current(target.id, snapshot);
  };
  // Transport loss never invents a finished state; the tab reattaches when opened.
  void connectEngine(target.sessionFile)
    .then((snapshot) => {
      deliver(snapshot);
      return watchEngine(snapshot, deliver, () => undefined, controller.signal);
    })
    .catch(() => undefined);
  return controller;
}
