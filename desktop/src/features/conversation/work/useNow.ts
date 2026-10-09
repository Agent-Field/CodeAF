import { useEffect, useState } from 'react';

/** The clock for elapsed time: the caller's `given` value, else a 1s tick that runs only while `active`. */
export function useNow(active: boolean, given?: number): number {
  const [tick, setTick] = useState(() => Date.now());
  useEffect(() => {
    if (!active || given !== undefined) return undefined;
    setTick(Date.now());
    const timer = setInterval(() => setTick(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active, given]);
  return given ?? tick;
}

/** Open state that follows `live` until the person chooses, then stays theirs. */
export function useWorkOpen(live: boolean): { open: boolean; toggle: () => void } {
  const [chosen, setChosen] = useState<boolean>();
  const open = chosen ?? live;
  return { open, toggle: () => setChosen(!open) };
}
