// The window frame's tint (Places 6a, 9a, 9d). Now and All places (root) are graphite.
// A graph place wears the effective tint the engine already resolved, inheritance included.
// A place the graph has not named yet wears nothing: unknown renders as nothing.
//
// The value is the current place's tint on this render; the frame background crossfades in the shell CSS.
// The attribute is written on document.body before paint so the frame and any menu portalled
// onto the body inherit the same tokens. macOS keeps its overlay title bar and sidebar vibrancy,
// and Linux keeps its decorated window: this module never calls the native window.

import { useLayoutEffect } from 'react';
import type { Tint } from './wire.ts';

const frameTints: Record<Tint, true> = { tide: true, iris: true, rose: true, sand: true, sage: true, graphite: true };

const isFrameTint = (value: string | undefined): value is Tint => !!value && Object.prototype.hasOwnProperty.call(frameTints, value);

/** What the caller passes. A Places shell satisfies this; specimens can pass the same shape. */
export type WindowTintSource = {
  place: string;
  index?: { byId: { get(id: string): { effectiveTint?: string } | undefined } };
};

/**
 * The tint the frame should wear for this place. Now and root are graphite even before the graph
 * arrives. Any other place uses `effectiveTint` only when it is one of the six palette names.
 */
export function windowFrameTint(place: string | undefined, effectiveTint: string | undefined): Tint | undefined {
  if (!place) return undefined;
  if (place === 'now' || place === 'root') return 'graphite';
  return isFrameTint(effectiveTint) ? effectiveTint : undefined;
}

/** The frame tint for the window's current place. The returned name is what `data-tint` wears. */
export function useWindowTint(shell: WindowTintSource): Tint | undefined {
  const tint = windowFrameTint(shell.place, shell.index?.byId.get(shell.place)?.effectiveTint);
  useLayoutEffect(() => {
    if (tint) document.body.dataset.tint = tint;
    else delete document.body.dataset.tint;
    return () => { delete document.body.dataset.tint; };
  }, [tint]);
  return tint;
}
