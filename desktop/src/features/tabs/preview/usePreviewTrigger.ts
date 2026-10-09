import { useEffect, useRef, type FocusEvent, type PointerEvent } from 'react';
import design from '../../../design/tokens.json';
import type { PreviewStore } from './previewStore';

/**
 * The trigger side of a hover preview: a 500ms hover opens it (instantly when a neighbour's card is
 * already open), keyboard focus opens it, and a press, a drag, the context menu, a blur or Escape closes it.
 * After a press it stays shut until the pointer leaves, so the hover timer cannot reopen it over what was opened.
 */
export function usePreviewTrigger(store: PreviewStore, id: string, open: boolean, disabled: boolean) {
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const pressed = useRef(false);
  const stop = () => { clearTimeout(timer.current); timer.current = undefined; };
  const dismiss = () => { stop(); store.close(id); };

  useEffect(() => () => { stop(); store.close(id); }, [id]);
  useEffect(() => { if (disabled) dismiss(); }, [disabled]);
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') dismiss(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open]);

  return {
    onPointerEnter: (event: PointerEvent) => {
      if (disabled || pressed.current || event.pointerType === 'touch') return;
      stop();
      if (store.isWarm()) store.open(id);
      else timer.current = setTimeout(() => store.open(id), design.interaction.previewOpenDelay);
    },
    onPointerLeave: () => { pressed.current = false; stop(); store.scheduleClose(id); },
    onPointerDown: () => { pressed.current = true; dismiss(); },
    onDragStart: dismiss,
    onClick: dismiss,
    onContextMenu: dismiss,
    onFocusCapture: (event: FocusEvent) => { if (!disabled && !pressed.current && event.currentTarget.matches(':focus-visible')) store.open(id); },
    onBlurCapture: dismiss,
  };
}
