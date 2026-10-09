// Stop for closed work: the same engine stop the composer's Stop button sends, addressed by saved session.
import { connectEngine, stopEngine } from '../../chat/engine-client';
import type { Pane } from '../model';

/** Stops the running turn (and with it the tasks the engine ties to it) of each pane. True when every stop was accepted. */
export async function stopPanes(panes: readonly Pane[]): Promise<boolean> {
  const results = await Promise.allSettled(panes.filter(pane => pane.sessionFile).map(async pane => {
    const session = await connectEngine(pane.sessionFile);
    await stopEngine(session.id);
  }));
  return results.every(result => result.status === 'fulfilled');
}
