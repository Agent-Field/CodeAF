import { useEffect, useState } from 'react';

const TICK_MS = 1000;

/** The current time, ticking once a second while `active`; a caller that already
 * has a clock passes it as `given` and no timer starts. */
export function useNow(active: boolean, given?: number): number {
  const [now, setNow] = useState(() => given ?? Date.now());
  useEffect(() => {
    if (given !== undefined || !active) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(timer);
  }, [active, given]);
  return given ?? now;
}
