import { useEffect, useState } from 'react';

/** Loads once while the card is open. A failed read leaves the card with its title: nothing is invented in its place. */
export function useLoaded<T>(key: string | undefined, load: () => Promise<T>): T | undefined {
  const [value, setValue] = useState<T>();
  useEffect(() => {
    if (!key) return;
    let live = true;
    load().then(result => { if (live) setValue(result); }, () => {});
    return () => { live = false; };
  }, [key]);
  return value;
}
