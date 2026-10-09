// Whether Option/Alt is held right now, shared by every tab (design 3l: holding it turns a running tab's X into a stop square).
import { useSyncExternalStore } from 'react';

let held = false;
const listeners = new Set<() => void>();
const set = (value: boolean) => { if (held === value) return; held = value; listeners.forEach(listener => listener()); };
let wired = false;

function wire() {
  if (wired || typeof window === 'undefined') return;
  wired = true;
  window.addEventListener('keydown', event => { if (event.key === 'Alt') set(true); }, true);
  window.addEventListener('keyup', event => { if (event.key === 'Alt') set(false); }, true);
  // A pointer move reports the true state when Alt went down or up while another window had focus.
  window.addEventListener('pointermove', event => set(event.altKey), { capture: true, passive: true });
  window.addEventListener('blur', () => set(false));
}

function subscribe(listener: () => void) {
  wire();
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export const useAltHeld = (): boolean => useSyncExternalStore(subscribe, () => held, () => false);
