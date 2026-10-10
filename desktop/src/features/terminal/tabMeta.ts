// The muted words after a job tab's name (Components, "Finished job · tab menu", C-EDGE-5).
// Running stays silent on the tab (Shell 3j). A non-zero exit does not colour these words; the failed
// dot is the mark the pane already publishes, and the shell owns that glyph.
import type { Pane } from '../tabs/types.ts';

/** The facts the pane has read. Absent means the tab has not heard from the engine yet, so it says nothing. */
export type TabMetaSource = {
  kind: 'terminal' | 'job';
  state: 'running' | 'exited' | 'closed';
  exitCode?: number;
};

const readers = new Map<string, () => TabMetaSource | undefined>();

/**
 * `exit N` for a job that ended itself. A running job, a shell, and a job the person closed say nothing:
 * the header already says "closed", and the design's tab only draws the exit of a job that finished.
 * A missing or non-integer code is `exit 0`, the same fallback the header uses.
 */
export function finishedJobTabMeta(info: TabMetaSource | undefined): string | undefined {
  if (!info || info.kind !== 'job' || info.state !== 'exited') return undefined;
  const code = typeof info.exitCode === 'number' && Number.isInteger(info.exitCode) ? info.exitCode : 0;
  return `exit ${code}`;
}

/** The pane publishes what it last read. The reader is kept after the pane unmounts, so a job you already opened still shows its exit on the strip. */
export function offerTerminalTabMeta(paneId: string, read: () => TabMetaSource | undefined): void {
  readers.set(paneId, read);
}

/** Drops a published reader. Tests use it so one case cannot leak into the next. */
export function clearTerminalTabMeta(paneId: string): void {
  readers.delete(paneId);
}

/** Kind slot. A pane that has not reported yet contributes nothing. */
export function terminalTabMeta(pane: Pane): string | undefined {
  return finishedJobTabMeta(readers.get(pane.id)?.());
}
