// The Inbox's live inputs gathered in one hook: the engine-wide world feed (one stream per window, owned by
// chat/world-store.ts), the failures this window has already seen, and the OS-level signals (notification, badge) that
// follow the same attention list. Nothing here polls; the feed is a stream and the signals fire when the list changes.
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { nativeControls, needsYouCount } from '../../../design/nativeControls';
import { worldStore } from '../../chat/world-store';
import { engineTransport, type FailureId } from '../../chat/world-client';
import { toasts } from '../../../design/toasts';
import type { Tab } from '../model';
import { buildBackgroundWork, type BackgroundWork } from './background';
import { markFailedSeen, readFailedSeen, saveFailedSeen, type FailedSeen } from './failedSeen';
import { createSeenMarks } from './seenMarks';
import type { Summaries } from './running';
import { attentionSignals } from './signals';

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

  // The signals describe the whole machine's attention, not this window's tabs, so they come from the feed alone
  // (signals.ts says why never from the Inbox's own list). Each names the conversation (and the question) a click on its
  // notification opens. This is the ONE place a window posts them: Rust treats an item missing from the newest list as
  // answered. The feed sequence goes with the list, so a window behind another's reading cannot undo it.
  const signals = useMemo(() => attentionSignals(world, seen, now), [world, seen, now]);
  const signature = signals.map(item => `${item.kind}:${item.id}`).join('|');
  useEffect(() => {
    const native = nativeControls();
    if (!native.desktop || world.status !== 'live') return;
    // Rust announces only what is new and only while no codeaf window is focused; the badge is the needs-you count.
    void native.notifyAttention(signals, world.seq).catch(() => undefined);
    void native.setBadge(needsYouCount(signals), world.seq).catch(() => undefined);
  }, [signature, world.status]);

  return { background, markFailedSeen: mark };
}
