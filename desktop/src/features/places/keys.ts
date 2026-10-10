// The Places shortcuts as a pure decision: which command a chord means for the window as it is right now. The chords
// themselves are matched once, in design/keyboard.ts (one registry, so two surfaces never both claim a key); this file
// only turns a matched chord into a typed command, so every platform and window state can be tested without a DOM.
import type { Shortcut } from '../../design/keyboard.ts';

/** What the window knows when a chord arrives. */
export type PlaceKeyContext = {
  /** The window's place: `now` or a place id. */
  place: string;
  /** Ids the rail's ⌃1–9 slots point at, Pinned first and then Open, in the rail's order. */
  order: readonly string[];
};

export type PlaceKeyCommand =
  | { type: 'go-to-chooser' }
  | { type: 'all-places' }
  | { type: 'home' }
  | { type: 'jump'; place: string }
  | { type: 'close-place' }
  | { type: 'up' }
  | { type: 'new-window' }
  | { type: 'undo' };

/**
 * ⌘0 Home, ⌃0 Now, ⌃1–9 the rail slots, ⌘P the chooser, ⌘⇧P a fresh All places tab, ⌘⇧W close the place, ⌘↑ up (matched
 * on a Home only, where it replaces the turn keys), ⌘N a new window, ⌘Z the last structural action. A slot with no place
 * behind it is not a command, so the key stays free for whoever else wants it. Space (Quick Look) is not decided here:
 * it needs the focused tile, which only the Home surface knows. ⌘N inside the chooser creates a place and is handled by
 * the chooser, which is open above this layer.
 */
export function placeKeyCommand(shortcut: Shortcut, context: PlaceKeyContext): PlaceKeyCommand | undefined {
  switch (shortcut.id) {
    case 'goto': return { type: 'go-to-chooser' };
    case 'all-places': return { type: 'all-places' };
    case 'place-home': return { type: 'home' };
    case 'close-place': return { type: 'close-place' };
    case 'up-level': return { type: 'up' };
    case 'new-window': return { type: 'new-window' };
    case 'undo': return { type: 'undo' };
    case 'place-jump': {
      const place = shortcut.index === 0 ? 'now' : context.order[(shortcut.index ?? 0) - 1];
      return place ? { type: 'jump', place } : undefined;
    }
  }
}
