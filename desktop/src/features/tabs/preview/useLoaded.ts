import { useEffect, useState } from 'react';

export type Loaded<T> = { key: string; value: T } | undefined;

/** A stored result counts only for the key it answered; any other key reads as not loaded yet. */
export const valueFor = <T,>(loaded: Loaded<T>, key: string | undefined): T | undefined => (key !== undefined && loaded?.key === key ? loaded.value : undefined);

/**
 * Loads once per key while the card is open. The result is stored WITH the key it answered, and a result for any
 * other key is never returned: a card that moves to a different target shows nothing until that target's own read
 * lands, never the last target's contents. A failed read leaves the card with its title; nothing is invented.
 */
export function useLoaded<T>(key: string | undefined, load: () => Promise<T>): T | undefined {
  const [loaded, setLoaded] = useState<Loaded<T>>();
  useEffect(() => {
    if (!key) return;
    let live = true;
    load().then(value => { if (live) setLoaded({ key, value }); }, () => {});
    return () => { live = false; };
  }, [key]);
  return valueFor(loaded, key);
}
