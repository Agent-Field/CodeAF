// The Inbox's live inputs gathered in one hook: the engine-wide world feed (one stream per window, owned by
// chat/world-store.ts) and the failures this window has already seen. Native delivery belongs to lib/native/notify.ts,
// which reads the canonical world client independently of which workspace is mounted.
import { useCallback, useRef, useState, useSyncExternalStore } from 'react';
import { worldStore } from '../../chat/world-store';
import { engineTransport, type FailureId } from '../../chat/world-client';
import { toasts } from '../../../design/toasts';
import type { Tab } from '../model';
import { buildBackgroundWork, type BackgroundWork } from './background';
import { markFailedSeen, readFailedSeen, saveFailedSeen, type FailedSeen } from './failedSeen';
import { createSeenMarks } from './seenMarks';
import type { Summaries } from './running';

type Options = {
  tabs: readonly Tab[];
  closed: readonly Tab[];
  summaries: Summaries;
  since: (id: string) => number | undefined;
  stopping: ReadonlySet<string>;
  now: number;
};

export function useBackground({ tabs, closed, summaries, since, stopping, now }: Options): { background: BackgroundWork; markFailedSeen: (chatId: string, count: number, failure?: FailureId) => void } {
  const world = useSyncExternalStore(worldStore.subscribe, worldStore.getState);
  const [seen, setSeen] = useState<FailedSeen>(() => readFailedSeen());
  const marks = useRef(createSeenMarks({ transport: engineTransport, onRefused: message => { toasts.show({ message: [message], tone: 'danger' }); } })).current;
  const pending = useSyncExternalStore(marks.subscribe, marks.getPending);
  // With the failure's identity the engine owns the mark (every window and device). Without it (an engine that predates the
  // mark) only this window's own record is possible, and that is all this claims.
  const mark = useCallback((chatId: string, count: number, failure?: FailureId) => {
    if (failure) { void marks.mark(chatId, failure); return; }
    setSeen(current => {
      const next = markFailedSeen(current, chatId, count);
      if (next !== current) saveFailedSeen(next);
      return next;
    });
  }, [marks]);

  const background = buildBackgroundWork({ tabs, closed, summaries, since: Object.fromEntries(closed.flatMap(tab => { const at = since(tab.id); return at ? [[tab.id, at]] : []; })), stopping, world, seen, pending, now });

  return { background, markFailedSeen: mark };
}
