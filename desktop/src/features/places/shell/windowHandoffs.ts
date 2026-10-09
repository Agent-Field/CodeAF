// Moving a tab between windows (Interactions "Move to new window", "Dragging a tab out of the strip makes a new
// window"). The move is two-phase in the native layer (src/design/nativeControls.ts): the source keeps its tab until
// the target window has claimed it, so a window that never opens never loses a tab. Tabs are views; the work behind
// a moved conversation keeps running and the target reattaches it by its journal.
import { useEffect } from 'react';
import { nativeControls, paneFromHandoff } from '../../../design/nativeControls';
import { newTab } from '../../tabs/helpers';
import type { WorkspaceAction } from '../../tabs/model';

/** Claims a tab handed to this window (at start and whenever one arrives), and drops a tab this window handed away once it has been claimed. */
export function useWindowHandoffs(dispatch: (action: WorkspaceAction) => void, closeTab: (id: string) => void) {
  useEffect(() => {
    const native = nativeControls();
    if (!native.desktop) return;
    let live = true;
    const unlisten: (() => void)[] = [];
    const claim = async () => {
      const handed = await native.claimHandoff().catch(() => undefined);
      if (!handed || !live) return;
      const pane = paneFromHandoff(handed, crypto.randomUUID());
      dispatch({ type: 'open', tab: { ...newTab(), ...pane, pinned: false }, background: false });
    };
    void claim();
    void native.onHandoffReady(() => void claim()).then(stop => { if (live) unlisten.push(stop); else stop(); });
    void native.onHandoffClaimed(paneId => closeTab(paneId)).then(stop => { if (live) unlisten.push(stop); else stop(); });
    return () => { live = false; unlisten.forEach(stop => stop()); };
    // The strip's dispatch and close are stable for its lifetime.
  }, []);
}
