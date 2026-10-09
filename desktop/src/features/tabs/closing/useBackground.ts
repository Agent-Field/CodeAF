// The Inbox's live inputs gathered in one hook: the engine-wide world feed (one stream per window, owned by
// chat/world-store.ts), the failures this window has already seen, and the OS-level signals (notification, badge) that
// follow the same attention list. Nothing here polls; the feed is a stream and the signals fire when the list changes.
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import { nativeControls, needsYouCount, type AttentionItem as NativeAttention } from '../../../design/nativeControls';
import { worldStore } from '../../chat/world-store';
import type { Tab } from '../model';
import { buildBackgroundWork, type BackgroundWork } from './background';
import { markFailedSeen, readFailedSeen, saveFailedSeen, type FailedSeen } from './failedSeen';
import type { Summaries } from './running';

type Options = {
  tabs: readonly Tab[];
  closed: readonly Tab[];
  summaries: Summaries;
  since: (id: string) => number | undefined;
  stopping: ReadonlySet<string>;
  now: number;
};

const clip = (text: string, max: number) => (text.length > max ? `${text.slice(0, max - 1)}…` : text).replace(/[\u0000-\u001f\u007f-\u009f]/g, ' ');

export function useBackground({ tabs, closed, summaries, since, stopping, now }: Options): { background: BackgroundWork; markFailedSeen: (chatId: string, count: number) => void } {
  const world = useSyncExternalStore(worldStore.subscribe, worldStore.getState);
  const [seen, setSeen] = useState<FailedSeen>(() => readFailedSeen());
  const mark = useCallback((chatId: string, count: number) => setSeen(current => {
    const next = markFailedSeen(current, chatId, count);
    if (next !== current) saveFailedSeen(next);
    return next;
  }), []);

  const background = buildBackgroundWork({ tabs, closed, summaries, since: Object.fromEntries(closed.flatMap(tab => { const at = since(tab.id); return at ? [[tab.id, at]] : []; })), stopping, world, seen, now });

  // The signals describe the whole machine's attention, not this window's tabs, so they read the feed directly.
  const signals = useMemo<NativeAttention[]>(() => {
    if (world.status !== 'live') return [];
    const titles = new Map(world.rows.map(row => [row.session, row.title] as const));
    const asked = world.items.map(item => ({ id: item.key, kind: 'needsYou' as const, chatTitle: clip(item.title || titles.get(item.session) || 'Conversation', 120), text: clip(item.text, 200) }));
    const failed = background.failed.map(item => ({ id: item.id, kind: 'failed' as const, chatTitle: clip(item.title, 120), text: 'A task failed' }));
    return [...asked, ...failed];
  }, [world.status, world.rows, world.items, background.failed]);
  const signature = signals.map(item => `${item.kind}:${item.id}`).join('|');
  useEffect(() => {
    const native = nativeControls();
    if (!native.desktop || world.status !== 'live') return;
    // Rust announces only what is new and only while no codeaf window is focused; the badge is the needs-you count.
    void native.notifyAttention(signals).catch(() => undefined);
    void native.setBadge(needsYouCount(signals)).catch(() => undefined);
  }, [signature, world.status]);

  return { background, markFailedSeen: mark };
}
