import { useCallback, useEffect, useRef, useSyncExternalStore } from 'react';
import { nativeWebAvailable } from '../../design/nativeWeb';
import { shotOf, subscribeShots, type ShotState } from './shots';
import { attach, detach, paneView, subscribeViews, type PaneView } from './views';

/**
 * Binds one pane to its native view: the returned ref goes on the sheet the
 * page is drawn over. While the sheet is mounted the view follows it; when it
 * unmounts the view hides and keeps its page.
 */
export function useWebPane(pane: string, url: string | undefined): PaneView & { native: boolean; shot: ShotState; sheetRef: (node: HTMLDivElement | null) => void } {
  const native = nativeWebAvailable();
  const view = useSyncExternalStore(subscribeViews, () => paneView(pane));
  const shot = useSyncExternalStore(subscribeShots, () => shotOf(pane));
  const sheet = useRef<HTMLDivElement | null>(null);
  const sheetRef = useCallback((node: HTMLDivElement | null) => { sheet.current = node; }, []);
  useEffect(() => {
    const node = sheet.current;
    if (!native || !url || !node) return;
    // The filmstrip draws this same pane as a scaled picture, centre card and
    // side cards. A native view cannot follow that transform, and attaching
    // the picture would take the one view off the real sheet. The overview
    // already hides the real view while those cards are up.
    if (node.closest('.overview-live')) return;
    attach(pane, url, node);
    return () => detach(pane);
    // The URL a pane was opened with only matters for the first open; later
    // navigation goes through `navigate`, so a URL change is not a re-attach.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pane, native, !!url]);
  return { ...view, native, shot, sheetRef };
}
