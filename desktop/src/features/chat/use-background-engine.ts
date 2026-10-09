import { useEffect, useRef } from 'react';
import { connectEngine, watchEngine, type EngineSnapshot } from './engine-client';
import type { WorkDocument } from './work-model';

/** Inactive open tabs keep observing. Closing a tab only aborts its reader. */
export function useBackgroundEngine(documents: Readonly<Record<string, WorkDocument>>, activeId: string, openIds: readonly string[], onSnapshot: (id: string, snapshot: EngineSnapshot) => void) {
 const subscriptions = useRef(new Map<string, AbortController>());
 const receive = useRef(onSnapshot); receive.current = onSnapshot;
 const desired = openIds.filter(id => id !== activeId && documents[id]?.engine).map(id => ({ id, file: documents[id].engine!.sessionFile }));
 const key = JSON.stringify(desired);
 useEffect(() => {
  const wanted = new Set(desired.map(item => item.id));
  for (const [id, controller] of subscriptions.current) {
   if (!wanted.has(id)) { controller.abort(); subscriptions.current.delete(id); }
  }
  for (const item of desired) {
   if (subscriptions.current.has(item.id)) continue;
   const controller = new AbortController(); subscriptions.current.set(item.id, controller);
   void connectEngine(item.file).then(snapshot => {
    if (controller.signal.aborted) return;
    receive.current(item.id, snapshot);
    return watchEngine(snapshot, value => { if (!controller.signal.aborted) receive.current(item.id, value); }, () => {}, controller.signal);
   }).catch(() => {
    // Never invent a stopped/completed state on transport loss. Active attachment
    // provides the reconnect action when the person returns to this tab.
   });
  }
 }, [key]);
 useEffect(() => () => { for (const controller of subscriptions.current.values()) controller.abort(); subscriptions.current.clear(); }, []);
}
