import { useEffect, useState } from 'react';
import { loadedCache } from './loadedCache.ts';

export type Loaded<T> = { key: string; value: T } | undefined;

/** A stored result counts only for the key it answered; any other key reads as not loaded yet. */
export const valueFor = <T,>(loaded: Loaded<T>, key: string | undefined): T | undefined => (key !== undefined && loaded?.key === key ? loaded.value : undefined);

/**
 * Returns already-known content during render. Concurrent cards share one read per resource and revision;
 * closing the overview does not discard it. A changed target never displays the previous target's result.
 */
export function useLoaded<T>(key: string | undefined, load: () => Promise<T>, revision = ''): T | undefined {
  const [loaded, setLoaded] = useState<Loaded<T>>();
  useEffect(() => {
    if (!key) return;
    let live = true;
    const known = loadedCache.read<T>(key);
    // A cache hit already drew during render; setting state again would repaint every card on reopen.
    loadedCache.load(key, revision, load).then(value => { if (live && value !== known) setLoaded({ key, value }); }, () => {});
    return () => { live = false; };
  }, [key, revision]);
  return loadedCache.read<T>(key) ?? valueFor(loaded, key);
}
