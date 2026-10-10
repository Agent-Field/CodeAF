// The web tab's pure model: what an address means, how it is shown in two
// tones, the mark a site gets, and how load state moves. No DOM and no native
// calls, so every rule is testable with node alone. The address rules are the
// ones in address.ts (http and https only); this file gives them the shape the
// tab strip and the pane ask for.

import { monogramHue, monogramLetter } from '../conversation/assets/paths.ts';
import { addressParts, toAddress } from './address.ts';

/** An address, or null when the text is not one and the caller should treat it as a question. */
export function parseAddress(text: string): URL | null {
  const address = toAddress(text);
  return 'url' in address ? new URL(address.url) : null;
}

/** The site in ink and everything after it dimmed. */
export function displayParts(url: URL | string): { host: string; rest: string } {
  const { site, rest } = addressParts(typeof url === 'string' ? url : url.href);
  return { host: site, rest };
}

/** A site's mark: one letter and one of the six stable hues. */
export function monogram(host: string): { letter: string; hue: number } {
  return { letter: monogramLetter(host), hue: monogramHue(host) };
}

export type WebErrorKind = 'none' | 'offline' | 'blocked' | 'certificate' | 'failed';

export type WebLoadState = {
  loading: boolean;
  /** 0..1 while loading; null when the page does not report it, so nothing is drawn. */
  progress: number | null;
  title: string;
  favicon: string | null;
  url: string;
  canBack: boolean;
  canForward: boolean;
  error: WebErrorKind;
};

export const initialWebState: WebLoadState = { loading: false, progress: null, title: '', favicon: null, url: '', canBack: false, canForward: false, error: 'none' };

export type WebAction =
  | { type: 'load'; url: string }
  | { type: 'progress'; progress: number }
  | { type: 'loaded'; title?: string }
  | { type: 'title'; title: string }
  | { type: 'favicon'; favicon: string | null }
  | { type: 'history'; canBack: boolean; canForward: boolean }
  | { type: 'error'; error: Exclude<WebErrorKind, 'none'> };

export function webReducer(state: WebLoadState, action: WebAction): WebLoadState {
  switch (action.type) {
    // A new load forgets the last failure and the last page's title and mark: they belong to the old page.
    case 'load': return { ...state, url: action.url, loading: true, progress: null, error: 'none', title: '', favicon: null };
    case 'progress': return state.loading ? { ...state, progress: Math.min(1, Math.max(0, action.progress)) } : state;
    case 'loaded': return { ...state, loading: false, progress: null, title: action.title ?? state.title };
    case 'title': return { ...state, title: action.title };
    case 'favicon': return { ...state, favicon: action.favicon };
    case 'history': return { ...state, canBack: action.canBack, canForward: action.canForward };
    case 'error': return { ...state, loading: false, progress: null, error: action.error };
  }
}
