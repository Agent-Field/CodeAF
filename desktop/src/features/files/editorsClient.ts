import type { EngineEditor } from '../chat/engine-client';

/** Default first, then by name. The route already ranks them; a stale answer still draws the same way. */
export function editorsDefaultFirst(editors: readonly EngineEditor[]): EngineEditor[] {
  return [...editors].sort((a, b) => Number(Boolean(b.default)) - Number(Boolean(a.default)) || a.name.localeCompare(b.name));
}
