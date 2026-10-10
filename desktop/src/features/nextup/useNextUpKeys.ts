// The three window chords of Iteration 2 that move through attention and history: ⌘J walks the questions, ⌘[ and ⌘]
// step focus history. One registration so a walk in progress can claim ⌘[ as "go back to where I was" before history does.
import { useEffect, useRef } from 'react';
import { registerShortcuts, shortcutLayer } from '../../design/keyboard.ts';
import type { FocusWire } from '../focus-history/useFocusHistory.ts';
import { worldStore } from '../chat/world-store.ts';
import { nextUpWalk, type WalkOrigin } from './useNextUpWalk.ts';

export function useNextUpKeys(options: { enabled: boolean; origin: () => WalkOrigin; wire: Pick<FocusWire, 'back' | 'forward'> }) {
 const latest = useRef(options);
 latest.current = options;
 useEffect(() => options.enabled ? registerShortcuts(shortcutLayer.surface + 1, shortcut => {
  const walking = nextUpWalk.getSnapshot().phase !== 'idle';
  switch (shortcut.id) {
   case 'next-up': nextUpWalk.start(latest.current.origin(), worldStore.getState().items); return true;
   // While a walk is open, back means the walk's origin; the history stack is untouched until it has returned.
   case 'back': if (walking) nextUpWalk.exit(); else latest.current.wire.back(); return true;
   case 'forward': if (walking) return false; latest.current.wire.forward(); return true;
   default: return false;
  }
 }) : undefined, [options.enabled]);
}
