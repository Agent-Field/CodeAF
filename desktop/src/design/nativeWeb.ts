// The one door from the renderer to native web views (src-tauri/src/web.rs).
// Every call carries a pane id, a URL, a rectangle or one of four history
// steps; nothing here sends a script. Outside the desktop app there are no
// native views, and `nativeWebAvailable()` says so before anything is tried.

import { invoke, isTauri } from '@tauri-apps/api/core';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';

export type WebRect = { x: number; y: number; width: number; height: number };
export type WebHistoryStep = 'back' | 'forward' | 'reload' | 'stop';
export type WebRefusal = 'tooLong' | 'malformed' | 'scheme' | 'noHost' | 'credentials' | 'appOrigin';
export type WebFailure = { kind: 'refused'; reason: WebRefusal } | { kind: 'unreachable' } | { kind: 'certificate' };
export type WebNotice = 'download' | 'blocked' | 'permission';

/** What the native view reports about its page; mirrors `WebState` in web.rs. */
export type WebState = {
  pane: string;
  url: string;
  title: string;
  loading: boolean;
  canBack: boolean;
  canForward: boolean;
  /** False where the platform cannot report history: Back and Forward then stay enabled. */
  historyKnown: boolean;
  failure: WebFailure | null;
  notice: WebNotice | null;
};

/** A page asked for a new window; codeaf opens a web tab instead. */
export type WebNewTab = { opener: string; url: string };
export type WebSnapshot = { image: string; url: string };

export const nativeWebAvailable = (): boolean => isTauri();

const refusals: readonly string[] = ['tooLong', 'malformed', 'scheme', 'noHost', 'credentials', 'appOrigin'];
const notices: readonly string[] = ['download', 'blocked', 'permission'];

function failureOf(value: unknown): WebFailure | null {
  if (!value || typeof value !== 'object') return null;
  const failure = value as { kind?: unknown; reason?: unknown };
  if (failure.kind === 'certificate') return { kind: 'certificate' };
  if (failure.kind === 'unreachable') return { kind: 'unreachable' };
  if (failure.kind === 'refused' && typeof failure.reason === 'string' && refusals.includes(failure.reason)) return { kind: 'refused', reason: failure.reason as WebRefusal };
  return null;
}

/** Validates a state from the native side; anything malformed is dropped, never guessed. */
export function webStateOf(value: unknown): WebState | null {
  if (!value || typeof value !== 'object') return null;
  const s = value as Record<string, unknown>;
  const flags = ['loading', 'canBack', 'canForward', 'historyKnown'] as const;
  if (typeof s.pane !== 'string' || typeof s.url !== 'string' || typeof s.title !== 'string' || flags.some(flag => typeof s[flag] !== 'boolean')) return null;
  return {
    pane: s.pane, url: s.url, title: s.title,
    loading: s.loading as boolean, canBack: s.canBack as boolean, canForward: s.canForward as boolean, historyKnown: s.historyKnown as boolean,
    failure: failureOf(s.failure),
    notice: typeof s.notice === 'string' && notices.includes(s.notice) ? s.notice as WebNotice : null,
  };
}

function rounded(rect: WebRect): WebRect {
  return { x: Math.round(rect.x), y: Math.round(rect.y), width: Math.max(0, Math.round(rect.width)), height: Math.max(0, Math.round(rect.height)) };
}

export async function webOpen(pane: string, url: string, rect: WebRect, visible: boolean): Promise<WebState> {
  const state = webStateOf(await invoke('web_open', { pane, url, rect: rounded(rect), visible }));
  if (!state) throw new Error('The page could not open');
  return state;
}

export const webNavigate = (pane: string, url: string) => invoke<void>('web_navigate', { pane, url });
export const webBounds = (pane: string, rect: WebRect) => invoke<void>('web_bounds', { pane, rect: rounded(rect) });
export const webVisible = (pane: string, visible: boolean) => invoke<void>('web_visible', { pane, visible });
export const webHistory = (pane: string, step: WebHistoryStep) => invoke<void>('web_history', { pane, step });
export const webClose = (pane: string) => invoke<void>('web_close', { pane });

export async function webSnapshot(pane: string): Promise<WebSnapshot | null> {
  try {
    const shot = await invoke<WebSnapshot>('web_snapshot', { pane });
    return typeof shot?.image === 'string' && shot.image.startsWith('data:image/png;base64,') ? shot : null;
  } catch {
    return null;
  }
}

export async function webList(): Promise<WebState[]> {
  const list: unknown = await invoke('web_list');
  return Array.isArray(list) ? list.map(webStateOf).filter((s): s is WebState => s !== null) : [];
}

export function onWebState(handler: (state: WebState) => void): Promise<UnlistenFn> {
  return listen<unknown>('web://state', event => {
    const state = webStateOf(event.payload);
    if (state) handler(state);
  });
}

/** ⌘L / Ctrl+L while a page is focused. A pane is set when the page itself forwarded the key. */
export function onWebFocusAddress(handler: (pane?: string) => void): Promise<UnlistenFn> {
  return listen<unknown>('web://focus-address', event => {
    const payload = event.payload;
    const pane = payload && typeof payload === 'object' && typeof (payload as { pane?: unknown }).pane === 'string'
      ? (payload as { pane: string }).pane
      : undefined;
    handler(pane);
  });
}

export function onWebNewTab(handler: (request: WebNewTab) => void): Promise<UnlistenFn> {
  return listen<unknown>('web://new-tab', event => {
    const p = event.payload as Partial<WebNewTab> | null;
    if (p && typeof p.opener === 'string' && typeof p.url === 'string') handler({ opener: p.opener, url: p.url });
  });
}
