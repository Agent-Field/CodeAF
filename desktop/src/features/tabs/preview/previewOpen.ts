/**
 * Whether a hover or a keyboard focus may open a preview card.
 * A press, a disabled trigger, a touch pointer, and a screen that cannot hover all refuse.
 * Keyboard focus passes pointerType "mouse": it is not a touch, and hover-none still blocks it.
 */
export function previewMayOpen(options: { hoverNone: boolean; pointerType: string; disabled: boolean; pressed: boolean }): boolean {
  return !options.disabled && !options.pressed && !options.hoverNone && options.pointerType !== 'touch';
}
