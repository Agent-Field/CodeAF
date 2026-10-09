import { useSyncExternalStore } from 'react';
import type { PreviewStore } from './previewStore';

/** Whether this tab's card is open in the workspace's store, and whether it arrived by swapping from a neighbour. */
export function usePreviewShown(store: PreviewStore, id: string): { open: boolean; swapped: boolean } {
  const current = useSyncExternalStore(store.subscribe, store.get);
  return { open: current?.id === id, swapped: current?.id === id && current.swapped };
}
