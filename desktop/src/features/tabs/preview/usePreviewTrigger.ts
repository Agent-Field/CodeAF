import { useEffect, useRef, type FocusEvent, type PointerEvent } from 'react';
import { pointerDragActive } from '../../../components/ui/pointerDrag';
import design from '../../../design/tokens.json';
import type { PreviewStore } from './previewStore';
import { previewMayOpen } from './previewOpen';

/** Pixels the preview card keeps clear of the viewport. The narrow max-width inset is twice this. */
export const previewCollisionPadding = Number.parseInt(design.foundation['preview-collision-padding'], 10);

/** A screen that cannot hover never gets a preview card, including from a later timer or a focus. */
const hoverNone = () => window.matchMedia('(hover: none)').matches;

/**
 * The trigger side of a hover preview: a 500ms hover opens it (instantly when a neighbour's card is
 * already open), keyboard focus opens it, and a press, a drag, the context menu, a blur or Escape closes it.
 * After a press it stays shut until the pointer leaves, so the hover timer cannot reopen it over what was opened.
 * A touch pointer, and any screen that reports hover: none, never opens a card.
 */
export function usePreviewTrigger(store: PreviewStore, id: string, open: boolean, disabled: boolean) {
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const pressed = useRef(false);
  const stop = () => { clearTimeout(timer.current); timer.current = undefined; };
  const dismiss = () => { stop(); store.close(id); };

  useEffect(() => () => { stop(); store.close(id); }, [id]);
  useEffect(() => { if (disabled) dismiss(); }, [disabled]);
  useEffect(() => {
    const media = window.matchMedia('(hover: none)');
    const shut = () => { if (media.matches) dismiss(); };
    shut();
    media.addEventListener('change', shut);
    return () => media.removeEventListener('change', shut);
  }, [id]);
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') dismiss(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open]);

  return {
    onPointerEnter: (event: PointerEvent) => {
      // A drag already in flight must not open a card on the row it crosses.
      if (pointerDragActive()) return;
      if (!previewMayOpen({ hoverNone: hoverNone(), pointerType: event.pointerType, disabled, pressed: pressed.current })) return;
      stop();
      if (store.isWarm()) store.open(id);
      else timer.current = setTimeout(() => { if (!hoverNone()) store.open(id); }, design.interaction.previewOpenDelay);
    },
    onPointerLeave: () => { pressed.current = false; stop(); store.scheduleClose(id); },
    onPointerDown: () => { pressed.current = true; dismiss(); },
    // A drag is a press that kept going. The press already closed the card, and capture keeps the next tab from opening one.
    onClick: dismiss,
    onContextMenu: dismiss,
    onFocusCapture: (event: FocusEvent) => {
      if (!previewMayOpen({ hoverNone: hoverNone(), pointerType: 'mouse', disabled, pressed: pressed.current })) return;
      if (event.currentTarget.matches(':focus-visible')) store.open(id);
    },
    onBlurCapture: dismiss,
  };
}
