import { useCallback, useSyncExternalStore } from 'react';
import { worldClient } from './worldClient.ts';
import type { WorldClient } from './worldClient.ts';

/** Select a stable store value; connection ownership belongs to the store, not to a pane. */
export function useWorld<T>(selector: (world: WorldClient) => T): T {
 const snapshot = useCallback(() => selector(worldClient), [selector]);
 return useSyncExternalStore(worldClient.subscribe, snapshot, snapshot);
}
