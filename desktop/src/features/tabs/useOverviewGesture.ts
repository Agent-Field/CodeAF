import { useEffect, type RefObject } from 'react';
import { createOverviewPinch } from './overviewGesture';

type ScaleEvent = Event & { scale?: number };

/** Only the app's tab strip owns this gesture. Content, editors, terminals and native pages keep their own zoom. */
export function useOverviewGesture(root: RefObject<HTMLElement | null>, enabled: boolean, open: () => void) {
  useEffect(() => {
    const element = root.current;
    if (!element || !enabled) return;
    const pinch = createOverviewPinch();
    let gestureOwned = false;
    const allowed = (target: EventTarget | null) => target instanceof Element &&
      !!target.closest('.workspace-tabstrip') &&
      !target.closest('input, textarea, select, [contenteditable], [role="textbox"], dialog, [role="menu"], [role="listbox"]') &&
      !document.querySelector('dialog:modal');
    const start = (event: Event) => { pinch.reset(); gestureOwned = allowed(event.target); };
    const change = (event: ScaleEvent) => {
      if (!gestureOwned || !allowed(event.target) || typeof event.scale !== 'number') return;
      if (event.scale > 1 && Number.isFinite(event.scale)) event.preventDefault();
      if (pinch.scale(event.scale)) open();
    };
    const end = () => { gestureOwned = false; pinch.reset(); };
    const wheel = (event: WheelEvent) => {
      if (!allowed(event.target) || !event.ctrlKey || event.metaKey || event.altKey || event.shiftKey || event.deltaMode !== WheelEvent.DOM_DELTA_PIXEL || event.deltaX !== 0) return;
      if (event.deltaY < 0 && Number.isFinite(event.deltaY)) event.preventDefault();
      if (pinch.wheel(event.deltaY, event.timeStamp)) open();
    };
    element.addEventListener('gesturestart', start);
    element.addEventListener('gesturechange', change, { passive: false });
    element.addEventListener('gestureend', end);
    element.addEventListener('wheel', wheel, { passive: false });
    return () => {
      element.removeEventListener('gesturestart', start);
      element.removeEventListener('gesturechange', change);
      element.removeEventListener('gestureend', end);
      element.removeEventListener('wheel', wheel);
    };
  }, [root, enabled, open]);
}
