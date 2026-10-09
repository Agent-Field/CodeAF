// The not-yet-recorded tail of a running turn, built from stream events.
// The snapshot stays canonical: the overlay only fills what it lacks.

import type { EngineEvent } from '../chat/engine-client.ts';

export type LiveOverlay = {
  text: string;
  thinking: string;
  activeTool?: { tool: string; hint: string };
  turnBaseEntries: number;
  error?: string;
};

export const emptyOverlay = (entryCount = 0): LiveOverlay => ({
  text: '',
  thinking: '',
  turnBaseEntries: entryCount,
});

const isIdle = (o: LiveOverlay) => !o.text && !o.thinking && !o.activeTool;

export function reduceLiveEvent(
  overlay: LiveOverlay,
  event: EngineEvent,
  entryCount: number,
): LiveOverlay {
  const base = isIdle(overlay) ? { ...overlay, turnBaseEntries: entryCount } : overlay;
  switch (event.kind) {
    case 'text':
      return { ...base, text: base.text + (event.text ?? '') };
    case 'thinking':
    case 'reasoning':
      return { ...base, thinking: base.thinking + (event.text ?? '') };
    case 'toolBegin':
      return { ...base, activeTool: { tool: event.tool, hint: event.hint } };
    case 'toolEnd':
    case 'toolFailed':
      return { ...base, activeTool: undefined };
    case 'assistantDone':
    case 'turnDone':
      return emptyOverlay(entryCount);
    case 'error':
      return {
        ...base,
        activeTool: undefined,
        error: event.error || event.text || 'The engine reported an error.',
      };
    default:
      return base;
  }
}
