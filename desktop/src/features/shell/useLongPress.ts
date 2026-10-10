import { useEffect } from 'react';
import design from '../../design/tokens.json' with { type: 'json' };
import { synthesizeContextMenu } from '../../components/ui/contextMenuEvent.ts';

/** Half a second, the same number Shift+F10 does not wait for. One source: interaction.longPressDelay. */
export const longPressDelayMs = design.interaction.longPressDelay;

/**
 * Controls whose existing context menu a finger should open. Scoped to the live strip and the
 * live rail so a design-system specimen, which reuses the same class names, does not grow a menu.
 */
export const longPressTargets = '.workspace-tabstrip .workspace-group-label, .workspace-tabstrip .workspace-tab, .place-rail .rail-row';

/** A press that starts on the close mark stays a close. The hold does not also open the menu. */
export const longPressIgnore = '.workspace-tab-close, .rail-row-close';

/**
 * Whether this contact should arm a long-press. A mouse hold is not one, unless the primary
 * pointer is already coarse: dragging a tab with a mouse must keep working. A finger is one
 * even on a machine whose primary pointer is a mouse, because a finger has no right button.
 * The close mark is excluded. Anything that is not a tab, a group label, or a place row is excluded.
 */
export function holdOpensMenu(input: { button: number; coarse: boolean; touch: boolean; onClose: boolean; onTarget: boolean }): boolean {
  if (input.button !== 0) return false;
  if (!input.coarse && !input.touch) return false;
  if (input.onClose || !input.onTarget) return false;
  return true;
}

function coarsePointer(): boolean {
  return window.matchMedia('(pointer: coarse)').matches;
}

type Live = { timer: number; stop: () => void; pointerId: number };

let users = 0;
let detach: (() => void) | undefined;

function attach(): () => void {
  let live: Live | undefined;
  // The click that ends the hold is still the same gesture. It must not select the tab, toggle the
  // group, go to the place, or land on a menu item the menu just opened under the finger.
  let swallowClick = false;

  const clear = () => {
    if (!live) return;
    window.clearTimeout(live.timer);
    live.stop();
    live = undefined;
  };

  const onClick = (event: MouseEvent) => {
    if (!swallowClick) return;
    swallowClick = false;
    event.preventDefault();
    event.stopPropagation();
  };

  const onDown = (event: PointerEvent) => {
    // A new contact means the previous hold's click already happened, or never will.
    swallowClick = false;
    const origin = event.target instanceof Element ? event.target : null;
    const target = origin?.closest<HTMLElement>(longPressTargets) ?? null;
    if (!holdOpensMenu({
      button: event.button,
      coarse: coarsePointer(),
      touch: event.pointerType === 'touch',
      onClose: !!origin?.closest(longPressIgnore),
      onTarget: !!target,
    }) || !target) return;
    clear();
    const stop = () => clear();
    // Sliding off the control, or the browser taking the pointer to scroll, cancels the hold.
    target.addEventListener('pointerleave', stop);
    target.addEventListener('pointercancel', stop);
    const timer = window.setTimeout(() => {
      if (!target.isConnected) { clear(); return; }
      const done = live;
      live = undefined;
      done?.stop();
      swallowClick = true;
      synthesizeContextMenu(target);
    }, longPressDelayMs);
    live = {
      timer,
      pointerId: event.pointerId,
      stop: () => {
        target.removeEventListener('pointerleave', stop);
        target.removeEventListener('pointercancel', stop);
      },
    };
  };

  const onUp = (event: PointerEvent) => {
    if (live && event.pointerId === live.pointerId) clear();
  };

  document.addEventListener('pointerdown', onDown);
  document.addEventListener('pointerup', onUp);
  document.addEventListener('pointercancel', onUp);
  // A drag that actually starts is a drag, not a menu.
  document.addEventListener('dragstart', clear);
  document.addEventListener('click', onClick, true);
  return () => {
    clear();
    swallowClick = false;
    document.removeEventListener('pointerdown', onDown);
    document.removeEventListener('pointerup', onUp);
    document.removeEventListener('pointercancel', onUp);
    document.removeEventListener('dragstart', clear);
    document.removeEventListener('click', onClick, true);
  };
}

/**
 * Arms the document listener while the caller is mounted. The strip and the rail both call it;
 * the count keeps a single listener, so a hold is not dispatched twice. Tabs, group labels, and
 * place rows need no extra props: the listener finds them and dispatches the menu they already own.
 */
export function useLongPress() {
  useEffect(() => {
    users += 1;
    if (users === 1) detach = attach();
    return () => {
      users -= 1;
      if (users === 0) {
        detach?.();
        detach = undefined;
      }
    };
  }, []);
}
