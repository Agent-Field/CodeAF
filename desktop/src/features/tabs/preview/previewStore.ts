// One hover preview is open at a time. The store holds which tab it belongs to and whether the card
// arrived by swapping from a neighbour (3a: moving across tabs swaps instantly, with no delay and no animation).
import { useSyncExternalStore } from 'react';
import design from '../../../design/tokens.json';

type Shown = { id: string; swapped: boolean } | null;

let shown: Shown = null;
let closeTimer: ReturnType<typeof setTimeout> | undefined;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach(listener => listener());
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };

/** True while a card is open: the next tab the pointer reaches shows its card at once. */
export const isWarm = () => shown !== null;

export function openPreview(id: string) {
  clearTimeout(closeTimer);
  if (shown?.id === id) return;
  shown = { id, swapped: shown !== null };
  emit();
}

export function closePreview(id?: string) {
  clearTimeout(closeTimer);
  if (!shown || (id !== undefined && shown.id !== id)) return;
  shown = null;
  emit();
}

/** Leaving a tab or the card closes after a short grace, so the pointer can cross the gap or reach the card. */
export function scheduleClose(id: string) {
  clearTimeout(closeTimer);
  closeTimer = setTimeout(() => closePreview(id), design.interaction.previewCloseDelay);
}

export const keepOpen = () => clearTimeout(closeTimer);

export function usePreviewShown(id: string): { open: boolean; swapped: boolean } {
  const current = useSyncExternalStore(subscribe, () => shown);
  return { open: current?.id === id, swapped: current?.id === id && current.swapped };
}
