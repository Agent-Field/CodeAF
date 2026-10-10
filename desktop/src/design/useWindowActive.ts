import { useEffect } from 'react';
import { initMaterial } from './material';

/** One focus subscription owns the window's material, including native focus and accessibility changes. */
export function useWindowActive() {
  useEffect(initMaterial, []);
}
