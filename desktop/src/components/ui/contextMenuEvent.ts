/**
 * The one synthetic contextmenu. Shift+F10 in Menu.tsx and a coarse long-press
 * both call this, so a finger and the keyboard open the menu at the same point
 * and the first item takes the next key. A synthetic event does not move focus
 * by itself; the frame below does, unless the menu has already focused an item.
 */
export function contextMenuInit(bounds: { left: number; width: number; bottom: number }) {
  return { bubbles: true, cancelable: true, button: 2, clientX: bounds.left + bounds.width / 2, clientY: bounds.bottom };
}

export function synthesizeContextMenu(target: HTMLElement) {
  const bounds = target.getBoundingClientRect();
  target.dispatchEvent(new MouseEvent('contextmenu', contextMenuInit(bounds)));
  requestAnimationFrame(() => {
    // Radix may already have focused an item; a late frame must not undo keyboard navigation into a submenu.
    if (document.activeElement?.closest('.app-menu')) return;
    document.querySelector<HTMLElement>('.app-menu-context[data-state="open"] [role^="menuitem"]:not([data-disabled])')?.focus();
  });
}
