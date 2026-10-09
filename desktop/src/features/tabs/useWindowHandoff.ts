// Both halves of a tab moving between windows, for THIS window. Receiving: claim the tab addressed to us at boot and
// whenever another window says one is ready, and open it. Sending: when the target has claimed a tab we started, drop it.
// Outside the desktop app every adapter call is a no-op, so the browser build never moves or loses a tab.
import { useEffect, type Dispatch } from 'react';
import { nativeControls, paneFromHandoff, type NativeControls } from '../../design/nativeControls';
import { toasts } from '../../design/toasts';
import { createId } from './helpers';
import type { WorkspaceAction } from './model';

export function useWindowHandoff(dispatch: Dispatch<WorkspaceAction>, native: NativeControls = nativeControls()) {
  useEffect(() => {
    if (!native.desktop) return;
    const off: Array<() => void> = [];
    let gone = false;
    async function claim() {
      try {
        const claimed = await native.claimHandoff();
        if (!claimed) return;
        const { handoffId: _id, ...handoff } = claimed;
        // The claim is made: even if this effect has been torn down, the tab must land, or it would exist nowhere.
        dispatch({ type: 'open', tab: { ...paneFromHandoff(handoff, createId()), pinned: false }, background: false });
      } catch { toasts.show({ message: ['Could not open the tab that was sent here'], tone: 'danger' }); }
    }
    const listen = (promise: Promise<() => void>) => void promise.then(stop => { if (gone) stop(); else off.push(stop); }, () => undefined);
    listen(native.onHandoffReady(() => void claim()));
    listen(native.onHandoffClaimed(id => dispatch({ type: 'release-moved', id })));
    void claim();
    return () => { gone = true; off.forEach(stop => stop()); };
  }, [dispatch, native]);
}
