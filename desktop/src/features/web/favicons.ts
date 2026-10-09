import { useEffect, useSyncExternalStore } from 'react';
import { fetchEngine } from '../chat/engine-client';
import type { Pane } from '../tabs/model';

const images = new Map<string, string | null>();
const pending = new Map<string, Promise<void>>();
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
let revision = 0;
const changed = () => { revision++; listeners.forEach(listener => listener()); };
const validImage = (value: unknown): value is string => typeof value === 'string' && value.length <= 90_000 && /^data:image\/(?!svg)[a-z0-9.+-]+;base64,[a-z0-9+/=]+$/i.test(value);
const identity = (place: string, pane: Pane) => `${place}:${pane.id}:${pane.target?.url ?? ''}`;

async function load(place: string, pane: Pane): Promise<void> {
 const key = identity(place, pane);
 if (images.has(key)) return;
 if (pending.has(key)) return pending.get(key);
 const request = (async () => {
  let domain: string;
  try { const url = new URL(pane.target!.url!); if (!/^https?:$/.test(url.protocol)) return; domain = url.hostname.toLowerCase(); } catch { return; }
  // The new target may still be in the canonical write queue. A short bounded
  // retry avoids permanently caching the initial not-yet-saved pane as a miss.
  for (const delay of [0, 400, 1000]) {
   if (delay) await new Promise(resolve => setTimeout(resolve, delay));
   try {
    const response = await fetchEngine(`/workspaces/${encodeURIComponent(place)}/favicon?pane=${encodeURIComponent(pane.id)}`);
    if (!response.ok) break;
    const result = await response.json() as { domain?: string; dataUrl?: unknown };
    if (result.domain !== domain) continue;
    images.set(key, validImage(result.dataUrl) ? result.dataUrl : null); changed(); return;
   } catch { break; }
  }
  images.set(key, null); changed();
 })();
 pending.set(key, request);
 try { await request; } finally { pending.delete(key); }
}

/** Shared across strip/split/address consumers; images stay transient, never in tab persistence. */
export function useWebFavicons(place: string | undefined, panes: readonly Pane[]): ReadonlyMap<string, string> {
 useSyncExternalStore(subscribe, () => revision);
 const web = panes.filter(pane => pane.kind === 'web' && pane.target?.url);
 const signature = JSON.stringify(web.map(pane => [pane.id, pane.target?.url]));
 useEffect(() => { if (place) web.forEach(pane => { void load(place, pane); }); }, [place, signature]);
 return new Map(place ? web.flatMap(pane => { const image = images.get(identity(place, pane)); return image ? [[pane.id, image] as const] : []; }) : []);
}
