import { isTauri } from '@tauri-apps/api/core';

// Frame material (Foundations §09, F-MAT-1..5). The frame is the one translucent surface: the window shows through
// the rail and tab strip. Three states, decided per window so a second window never inherits the first one's focus.
export type Material = 'native' | 'glass' | 'solid';

export interface MaterialInputs {
  /** Running inside the Tauri shell. */
  desktop: boolean;
  /** navigator.platform of that shell. */
  platform: string;
  /** The window has focus. An inactive window is solid so it recedes (F-MAT-3). */
  active: boolean;
  reducedTransparency: boolean;
  moreContrast: boolean;
  backdropFilter: boolean;
  /** A popover or palette that blurs its own backdrop is open: never two blurs at once. */
  blurOpen: boolean;
}

/** Pure so the whole state machine is testable without a window. */
export function resolveMaterial(input: MaterialInputs): Material {
  if (!input.active || input.reducedTransparency || input.moreContrast || input.blurOpen) return 'solid';
  // Vibrancy lives under the webview on macOS and Windows; CSS adds no blur on top of it.
  if (input.desktop && /Mac|Win/.test(input.platform)) return 'native';
  // Linux and browsers use the same frame-only CSS fallback when blur is supported.
  return input.backdropFilter ? 'glass' : 'solid';
}

/** Marker for any layer that blurs its own backdrop (palette scrim, popovers). */
export const BLUR_LAYER = '[data-blur-layer]';

/** Wires one window's focus, media queries and blur layers to data attributes on <html>. Returns a cleanup. */
export function initMaterial(): () => void {
  if (typeof document === 'undefined' || typeof window === 'undefined') return () => {};
  const root = document.documentElement;
  const reduced = window.matchMedia('(prefers-reduced-transparency: reduce)');
  const contrast = window.matchMedia('(prefers-contrast: more)');
  let active = document.hasFocus();
  // Writing an attribute its own value still queues a mutation record. The terminal reads its theme through a probe element
  // appended inside the body, and re-reads whenever <html> attributes mutate, so an unconditional write here made the two
  // observers wake each other forever. Only a change is written.
  const write = (name: 'windowActive' | 'material', value: string) => { if (root.dataset[name] !== value) root.dataset[name] = value; };
  const apply = () => {
    write('windowActive', String(active));
    write('material', resolveMaterial({
      desktop: isTauri(),
      platform: navigator.platform,
      active,
      reducedTransparency: reduced.matches,
      moreContrast: contrast.matches,
      backdropFilter: typeof CSS !== 'undefined' && CSS.supports('backdrop-filter', 'blur(1px)'),
      blurOpen: document.querySelector(BLUR_LAYER) !== null,
    }));
  };
  const set = (next: boolean) => () => { active = next; apply(); };
  const focus = set(true);
  const blur = set(false);
  window.addEventListener('focus', focus);
  window.addEventListener('blur', blur);
  reduced.addEventListener('change', apply);
  contrast.addEventListener('change', apply);
  const layers = new MutationObserver(apply);
  layers.observe(document.body, { childList: true, subtree: true });
  let unlisten: (() => void) | undefined;
  let closed = false;
  // The native window can lose focus without the page seeing a blur (another app's panel), so ask the shell too.
  if (isTauri()) {
    void import('@tauri-apps/api/window').then(async ({ getCurrentWindow }) => {
      const off = await getCurrentWindow().onFocusChanged(({ payload }) => { active = payload; apply(); });
      if (closed) off(); else unlisten = off;
    }).catch(() => {});
  }
  apply();
  return () => {
    closed = true;
    unlisten?.();
    layers.disconnect();
    window.removeEventListener('focus', focus);
    window.removeEventListener('blur', blur);
    reduced.removeEventListener('change', apply);
    contrast.removeEventListener('change', apply);
  };
}
