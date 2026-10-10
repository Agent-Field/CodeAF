// The workspace's live inputs for closed-but-running work, gathered in one hook: the engine-wide world feed (one stream per
// window, owned by chat/world-store.ts) read against this window's own tabs. Home's Live section reads the place feed for
// the same work; this model only keeps closed tabs observed until their work ends. Native delivery belongs to
// lib/native/notify.ts, which reads the canonical world client independently of which workspace is mounted.
import { useSyncExternalStore } from 'react';
import { worldStore } from '../../chat/world-store';
import type { Tab } from '../model';
import { buildBackgroundWork, type BackgroundWork } from './background';
import type { Summaries } from './running';

type Options = {
  tabs: readonly Tab[];
  closed: readonly Tab[];
  summaries: Summaries;
  since: (id: string) => number | undefined;
  stopping: ReadonlySet<string>;
};

export function useBackground({ tabs, closed, summaries, since, stopping }: Options): { background: BackgroundWork } {
  const world = useSyncExternalStore(worldStore.subscribe, worldStore.getState);
  const background = buildBackgroundWork({ tabs, closed, summaries, since: Object.fromEntries(closed.flatMap(tab => { const at = since(tab.id); return at ? [[tab.id, at]] : []; })), stopping, world });
  return { background };
}
