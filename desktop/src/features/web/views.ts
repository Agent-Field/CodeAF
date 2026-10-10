// The life of every native web view, keyed by pane id, outside React: a tab
// switch unmounts a pane, but its page (and its history) must survive, so
// unmounting only hides the view. A view closes when its tab closes (`reap`),
// when too many hidden views pile up, or when nothing claims it after the
// renderer reloads. Closing a view never touches a conversation: a tab is a
// view, not the work.
//
// One animation-frame pass measures every mounted pane's sheet and decides,
// for each view, where it goes and whether it shows: hidden while its pane is
// not on screen, while anything of the app's floats over it (covers.ts), while
// the pane draws its own error over the sheet, and while the window is hidden.

import {
  nativeWebAvailable, onWebFocusAddress, onWebNewTab, onWebState, webClose, webHistory, webList, webNavigate, webOpen, webBounds, webVisible,
  type WebHistoryStep, type WebRect, type WebState,
} from '../../design/nativeWeb';
import { focusAddress } from './addressFocus';
import { coverBoxes, isCovered } from './covers';
import { openFromPage } from './host';
import { capture, forgetShot } from './shots';

/** Hidden views kept for quick return to a tab; the oldest closes beyond this. */
export const MAX_HIDDEN = 4;
/** After a renderer reload, a view no pane claims within this long is closed. */
export const ADOPT_GRACE_MS = 4000;
/** A finished load is pictured this long after it settles. */
const SHOT_SETTLE_MS = 600;

type Native = 'none' | 'opening' | 'open' | 'failed';
type Slot = {
  pane: string;
  url: string;
  sheet: HTMLElement | null;
  mounted: boolean;
  native: Native;
  sentRect: WebRect | null;
  sentVisible: boolean | null;
  covered: boolean;
  hiddenSince: number;
  openError?: string;
  /** A view found alive after a renderer reload, not yet claimed by a pane. */
  adopted: boolean;
  queue: Promise<unknown>;
};

/** What a pane renders from: the page's state, whether the app covers it, and why it failed to open. */
export type PaneView = { state: WebState | null; covered: boolean; openError?: string };

const slots = new Map<string, Slot>();
const states = new Map<string, WebState>();
const listeners = new Set<() => void>();
let frame = 0;
let started = false;
let observers: { resize: ResizeObserver; mutation: MutationObserver } | null = null;

const notify = () => listeners.forEach(listener => listener());

export function subscribeViews(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

const views = new Map<string, PaneView>();
/** Stable per pane between changes, for useSyncExternalStore. */
export function paneView(pane: string): PaneView {
  const slot = slots.get(pane);
  const next: PaneView = { state: states.get(pane) ?? null, covered: slot?.covered ?? false, openError: slot?.openError };
  const last = views.get(pane);
  if (last && last.state === next.state && last.covered === next.covered && last.openError === next.openError) return last;
  views.set(pane, next);
  return next;
}

function slotFor(pane: string, url: string): Slot {
  let slot = slots.get(pane);
  if (!slot) {
    slot = { pane, url, sheet: null, mounted: false, native: 'none', sentRect: null, sentVisible: null, covered: false, hiddenSince: 0, adopted: false, queue: Promise.resolve() };
    slots.set(pane, slot);
  }
  return slot;
}

/** Runs native calls for one view in order, so a hide never overtakes the show before it. */
function enqueue(slot: Slot, op: () => Promise<unknown>) {
  slot.queue = slot.queue.then(op).catch(() => undefined);
}

function start() {
  if (started || !nativeWebAvailable()) return;
  started = true;
  void onWebState(state => {
    const before = states.get(state.pane);
    states.set(state.pane, state);
    const settled = before?.loading && !state.loading && !state.failure;
    if (settled) window.setTimeout(() => { if (slots.get(state.pane)?.sentVisible) void capture(state.pane); }, SHOT_SETTLE_MS);
    notify();
    schedule();
  });
  void onWebNewTab(request => openFromPage(request.url, request.opener));
  void onWebFocusAddress(pane => focusAddress(pane));
  // Views from before a renderer reload: adopt them, and close the unclaimed.
  void webList().then(list => {
    for (const state of list) {
      if (slots.has(state.pane)) continue;
      const slot = slotFor(state.pane, state.url);
      slot.native = 'open';
      slot.adopted = true;
      slot.sentVisible = null;
      slot.hiddenSince = Date.now();
      states.set(state.pane, state);
    }
    window.setTimeout(() => {
      for (const slot of [...slots.values()]) if (slot.adopted) closeView(slot.pane);
    }, ADOPT_GRACE_MS);
    schedule();
  }).catch(() => undefined);
  const resize = new ResizeObserver(schedule);
  const mutation = new MutationObserver(schedule);
  mutation.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'style', 'data-state', 'hidden', 'open'] });
  window.addEventListener('resize', schedule);
  document.addEventListener('visibilitychange', schedule);
  observers = { resize, mutation };
}

export function schedule() {
  if (frame || !started) return;
  frame = window.requestAnimationFrame(() => {
    frame = 0;
    tick();
  });
}

const sameRect = (a: WebRect | null, b: WebRect) => !!a && a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;

function tick() {
  const covers = coverBoxes();
  const windowShown = document.visibilityState !== 'hidden';
  let changed = false;
  for (const slot of slots.values()) {
    if (!slot.mounted || !slot.sheet) {
      if (slot.native === 'open' && slot.sentVisible !== false) {
        slot.sentVisible = false;
        enqueue(slot, () => webVisible(slot.pane, false));
      }
      continue;
    }
    const box = slot.sheet.getBoundingClientRect();
    const rect: WebRect = { x: Math.round(box.left), y: Math.round(box.top), width: Math.round(box.width), height: Math.round(box.height) };
    const covered = isCovered(rect, covers);
    if (covered !== slot.covered) {
      slot.covered = covered;
      changed = true;
      // The frozen frame the pane draws while covered is the latest picture.
      if (covered && slot.sentVisible) void capture(slot.pane);
    }
    const failed = !!states.get(slot.pane)?.failure;
    const show = windowShown && !covered && !failed && rect.width >= 1 && rect.height >= 1;
    if (slot.native === 'none') {
      slot.native = 'opening';
      slot.openError = undefined;
      enqueue(slot, () => webOpen(slot.pane, slot.url, rect, show).then(state => {
        slot.native = 'open';
        slot.sentRect = rect;
        slot.sentVisible = show;
        if (!states.has(slot.pane)) states.set(slot.pane, state);
        notify();
        schedule();
      }, (error: unknown) => {
        slot.native = 'failed';
        slot.openError = error instanceof Error ? error.message : typeof error === 'string' ? error : 'The page could not open';
        notify();
      }));
      continue;
    }
    if (slot.native !== 'open') continue;
    if (!show && slot.sentVisible !== false) {
      slot.sentVisible = false;
      enqueue(slot, () => webVisible(slot.pane, false));
    }
    if (show && !sameRect(slot.sentRect, rect)) {
      slot.sentRect = rect;
      enqueue(slot, () => webBounds(slot.pane, rect));
    }
    if (show && slot.sentVisible !== true) {
      slot.sentVisible = true;
      enqueue(slot, () => webVisible(slot.pane, true));
    }
  }
  if (changed) notify();
}

/** A pane with a page came on screen: claim (or create) its view and measure its sheet. */
export function attach(pane: string, url: string, sheet: HTMLElement) {
  start();
  const slot = slotFor(pane, url);
  if (slot.native === 'failed' && slot.url !== url) slot.native = 'none';
  slot.url = url;
  slot.sheet = sheet;
  slot.mounted = true;
  slot.adopted = false;
  observers?.resize.observe(sheet);
  schedule();
}

/** The pane left the screen (tab switch, split change, unmount): hide, keep the page. */
export function detach(pane: string) {
  const slot = slots.get(pane);
  if (!slot) return;
  if (slot.sheet) observers?.resize.unobserve(slot.sheet);
  slot.sheet = null;
  slot.mounted = false;
  slot.hiddenSince = Date.now();
  schedule();
  const hidden = [...slots.values()].filter(s => !s.mounted).sort((a, b) => a.hiddenSince - b.hiddenSince);
  for (const old of hidden.slice(0, Math.max(0, hidden.length - MAX_HIDDEN))) closeView(old.pane);
}

/** Closes one view. Its tab closed, or it was the oldest hidden one. */
export function closeView(pane: string) {
  const slot = slots.get(pane);
  slots.delete(pane);
  states.delete(pane);
  views.delete(pane);
  forgetShot(pane);
  if (slot && (slot.native === 'open' || slot.native === 'opening')) enqueue(slot, () => webClose(pane));
  notify();
}

/** The workspace's live pane ids: every view whose pane is gone closes. */
export function reap(live: Iterable<string>) {
  const keep = new Set(live);
  for (const pane of [...slots.keys()]) if (!keep.has(pane)) closeView(pane);
}

export function navigate(pane: string, url: string) {
  const slot = slots.get(pane);
  if (!slot) return;
  slot.url = url;
  if (slot.native === 'failed') slot.native = 'none';
  if (slot.native === 'open') {
    const state = states.get(pane);
    if (state) states.set(pane, { ...state, url, failure: null, loading: true });
    enqueue(slot, () => webNavigate(pane, url));
    notify();
  }
  schedule();
}

export function step(pane: string, which: WebHistoryStep) {
  const slot = slots.get(pane);
  if (slot?.native !== 'open') return;
  if (which === 'reload') {
    const state = states.get(pane);
    if (state?.failure) states.set(pane, { ...state, failure: null, loading: true });
    notify();
    schedule();
  }
  enqueue(slot, () => webHistory(pane, which));
}

/** Try again: reopen a view that failed to open, or reload a page that failed to load. */
export function retry(pane: string) {
  const slot = slots.get(pane);
  if (!slot) return;
  if (slot.native === 'failed') {
    slot.native = 'none';
    slot.openError = undefined;
    notify();
    schedule();
    return;
  }
  step(pane, 'reload');
}
