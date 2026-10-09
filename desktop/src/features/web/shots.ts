// Pictures of pages, in memory only: a data URL per pane, taken by the native
// view (WKWebView / WebKitGTK snapshot) and never synthesised. A pane with no
// picture says so; it never draws a stand-in that looks like a page.

import { webSnapshot } from '../../design/nativeWeb';

export type Shot = { image: string; url: string; takenAt: number };
/** 'none' before any attempt; 'unavailable' when the platform could not take one. */
export type ShotState = Shot | 'none' | 'unavailable';

const shots = new Map<string, ShotState>();
const listeners = new Set<() => void>();
const inflight = new Map<string, Promise<Shot | null>>();
/** A pane is pictured at most this often; a hover card does not need more. */
export const SHOT_INTERVAL_MS = 3000;

export const shotOf = (pane: string): ShotState => shots.get(pane) ?? 'none';

export function subscribeShots(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function set(pane: string, state: ShotState | undefined) {
  if (state === undefined) shots.delete(pane);
  else shots.set(pane, state);
  listeners.forEach(listener => listener());
}

export const forgetShot = (pane: string) => set(pane, undefined);

/** Takes a picture now unless a fresh one exists (pass `force` for a person's own action). */
export function capture(pane: string, force = false): Promise<Shot | null> {
  const current = shotOf(pane);
  if (!force && typeof current === 'object' && Date.now() - current.takenAt < SHOT_INTERVAL_MS) return Promise.resolve(current);
  const running = inflight.get(pane);
  if (running) return running;
  const next = webSnapshot(pane).then(shot => {
    inflight.delete(pane);
    const state: ShotState = shot ? { ...shot, takenAt: Date.now() } : 'unavailable';
    set(pane, state);
    return typeof state === 'object' ? state : null;
  });
  inflight.set(pane, next);
  return next;
}
