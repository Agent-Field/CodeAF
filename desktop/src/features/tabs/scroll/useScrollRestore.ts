import { createContext, useContext, useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import type { ScrollSpot } from './scrollMemory';
import { scheduleSave, useScrollMemory } from './memoryStore';

export { pruneScrollMemory, scrollMemorySize } from './memoryStore';

// Per-pane scroll restoration. Only the active tab's panes are mounted (mounting every tab would multiply streams and polls), so
// a pane remounts empty each time its tab is selected. This hook remembers where every scroller INSIDE the pane body was left,
// without knowing which kind drew it, and puts each one back when content has arrived. It never moves a scroller the reader has
// touched, and it never touches a pane that has no saved position (so a conversation opened for the first time still sticks to its end).

/** How long a restore keeps waiting for content (an engine read, a list, an image) before it gives up for good. */
export const RESTORE_TIMEOUT_MS = 6000;
/** Frames a scroller must hold its place before the restore lets go of it; late layout and the conversation's own follow settle inside this. */
const SETTLE_FRAMES = 20;
/** Within this distance of its end a scroller counts as at its end, which restores to the end even if the content grew meanwhile. */
const END_SLACK = 1.5;
/** Characters of a scroller's own text that name it when it has no explicit key. */
const FINGERPRINT_CHARS = 120;

/**
 * Something that scrolls and is not a DOM scroller: the xterm buffer is the case. It reads and writes the same spot a DOM scroller does
 * (`top` is a line, `left` is 0). `reachable` is false while the content is too short to hold the spot (an output still replaying).
 */
export type Scroller = {
  read(): ScrollSpot;
  reachable(spot: ScrollSpot): boolean;
  write(spot: ScrollSpot): void;
  /** True when the scroller already shows the spot. */
  settled(spot: ScrollSpot): boolean;
};

export type ScrollController = { register(key: string, scroller: Scroller): { changed(): void; dispose(): void } };
const ScrollControllerContext = createContext<ScrollController | null>(null);
export const ScrollControllerProvider = ScrollControllerContext.Provider;

/**
 * A kind registers a scroller the DOM cannot see under a stable key; the returned `changed()` tells the pane it moved. Outside a pane
 * (a specimen) it does nothing.
 */
export function useScrollAdapter(key: string, scroller: Scroller): () => void {
  const controller = useContext(ScrollControllerContext);
  const latest = useRef(scroller);
  latest.current = scroller;
  const changed = useRef<() => void>(() => undefined);
  useEffect(() => {
    if (!controller) return;
    const proxy: Scroller = { read: () => latest.current.read(), reachable: spot => latest.current.reachable(spot), write: spot => latest.current.write(spot), settled: spot => latest.current.settled(spot) };
    const handle = controller.register(key, proxy);
    changed.current = handle.changed;
    return () => { changed.current = () => undefined; handle.dispose(); };
  }, [controller, key]);
  return () => changed.current();
}

/** djb2 over the text: small, stable, and good enough to tell sibling outputs apart. */
function hash(text: string): string {
  let h = 5381;
  for (let i = 0; i < text.length; i++) h = ((h * 33) ^ text.charCodeAt(i)) >>> 0;
  return h.toString(36);
}

/**
 * A scroller is named by an explicit `data-scroll-key` (the container's meaning, so it never depends on order). A nested output that has none
 * (a tool call's excerpt) is named by its tag, first class and what it says, so another output appearing above it does not rename it.
 * Look-alikes with the same name are told apart by their order among themselves.
 */
function describe(element: HTMLElement): { selector: string; name: string; same: (other: HTMLElement) => boolean } {
  const named = element.dataset.scrollKey;
  if (named) return { name: `k:${named}`, selector: `[data-scroll-key="${CSS.escape(named)}"]`, same: () => true };
  const first = element.classList[0];
  const selector = first ? `${element.localName}.${CSS.escape(first)}` : element.localName;
  const print = (el: HTMLElement) => hash(`${el.getAttribute('aria-label') ?? ''}\n${(el.textContent ?? '').slice(0, FINGERPRINT_CHARS)}`);
  const mine = print(element);
  return { name: `s:${first ? `${element.localName}.${first}` : element.localName}:${mine}`, selector, same: other => print(other) === mine };
}

function keyOf(root: HTMLElement, element: HTMLElement): string {
  if (element === root) return 'root';
  const { name, selector, same } = describe(element);
  const order = Array.from(root.querySelectorAll<HTMLElement>(selector)).filter(same).indexOf(element);
  return `${name}#${order}`;
}

function find(root: HTMLElement, key: string): HTMLElement | null {
  if (key === 'root') return root;
  const at = key.lastIndexOf('#');
  if (at < 0) return null;
  const name = key.slice(0, at);
  const order = Number(key.slice(at + 1));
  if (name.startsWith('k:')) return root.querySelectorAll<HTMLElement>(`[data-scroll-key="${CSS.escape(name.slice(2))}"]`)[order] ?? null;
  const [, tagClass, fingerprint] = name.split(':');
  const [tag, first] = tagClass.split('.');
  const selector = first ? `${tag}.${CSS.escape(first)}` : tag;
  const matching = Array.from(root.querySelectorAll<HTMLElement>(selector)).filter(el => hash(`${el.getAttribute('aria-label') ?? ''}\n${(el.textContent ?? '').slice(0, FINGERPRINT_CHARS)}`) === fingerprint);
  return matching[order] ?? null;
}

const spotOf = (element: HTMLElement): ScrollSpot => {
  const max = element.scrollHeight - element.clientHeight;
  return { top: element.scrollTop, left: element.scrollLeft, end: max > END_SLACK && max - element.scrollTop <= END_SLACK };
};

/** A DOM scroller as a Scroller, so the restore loop has one shape to drive. */
function domScroller(element: HTMLElement): Scroller {
  const maxTop = () => element.scrollHeight - element.clientHeight;
  const maxLeft = () => element.scrollWidth - element.clientWidth;
  // Content that has not grown big enough yet cannot hold the offset: wait for it rather than settle for a clamp.
  return {
    read: () => spotOf(element),
    reachable: spot => (spot.end || spot.top <= maxTop() + 0.5) && spot.left <= maxLeft() + 0.5,
    write: spot => { element.scrollTop = spot.end ? maxTop() : spot.top; element.scrollLeft = spot.left; },
    settled: spot => Math.abs(element.scrollTop - (spot.end ? maxTop() : spot.top)) <= 0.5 && Math.abs(element.scrollLeft - spot.left) <= 0.5,
  };
}

type Pending = { spot: ScrollSpot; held: number };
const INTENT = ['wheel', 'touchstart', 'pointerdown', 'keydown'] as const;

/**
 * Remembers and restores every scroller inside `root` for the pane `paneId`.
 * `visible` is false while the pane is hidden (another pane is maximized): a hidden pane loses its offsets, so becoming visible restores them again.
 * `layout` names the shape of the card (which pane is maximized, the split layout); the browser clamps offsets when a pane is resized by it, so a new layout restores again.
 * Returns the controller a kind registers non-DOM scrollers with (see useScrollAdapter).
 */
export function useScrollRestore(paneId: string, root: RefObject<HTMLElement | null>, visible: boolean, layout = ''): ScrollController {
  const memory = useScrollMemory();
  const adapters = useRef(new Map<string, Scroller>());
  // Scrollers this pane is still restoring, by key. The recorder consults it so a restore never records itself.
  const restoring = useRef<Map<string, Pending> | null>(null);
  const notes = useRef(new Map<string, ScrollSpot>());
  const poke = useRef<() => void>(() => undefined);
  const controller = useRef<ScrollController>({
    register(key, scroller) {
      adapters.current.set(key, scroller);
      return {
        changed: () => { if (!restoring.current?.has(key)) { notes.current.set(key, scroller.read()); poke.current(); } },
        dispose: () => { if (adapters.current.get(key) === scroller) adapters.current.delete(key); },
      };
    },
  }).current;

  // Recording. Scroll does not bubble, so one capturing listener on the body sees every nested scroller.
  useLayoutEffect(() => {
    const body = root.current;
    if (!body || !memory) return;
    const seen = new Map<HTMLElement, ScrollSpot>();
    let frame = 0;
    const flush = () => {
      frame = 0;
      for (const [element, spot] of seen) if (body.contains(element)) {
        const key = keyOf(body, element);
        if (!restoring.current?.has(key)) memory.set(paneId, key, spot);
      }
      for (const [key, spot] of notes.current) if (!restoring.current?.has(key)) memory.set(paneId, key, spot);
      if (seen.size || notes.current.size) scheduleSave();
      seen.clear(); notes.current.clear();
    };
    poke.current = () => { if (!frame) frame = requestAnimationFrame(flush); };
    const onScroll = (event: Event) => {
      const target = event.target;
      if (!(target instanceof HTMLElement) || !body.contains(target)) return;
      // xterm's own viewport is driven through its buffer (the terminal's registered scroller), never by its DOM offset.
      if (target.closest('.xterm')) return;
      // A hidden pane reports offsets of nothing.
      if (body.getClientRects().length === 0) return;
      seen.set(target, spotOf(target));
      poke.current();
    };
    body.addEventListener('scroll', onScroll, { capture: true, passive: true });
    return () => {
      body.removeEventListener('scroll', onScroll, { capture: true });
      if (frame) cancelAnimationFrame(frame);
      poke.current = () => undefined;
      // The tab is being left: take what the reader last did before the nodes go.
      flush();
    };
  }, [paneId, root, memory]);

  // Restoring. Runs after the children's own layout effects but before any scroll event they cause is delivered, so those events are already ours.
  useLayoutEffect(() => {
    const body = root.current;
    if (!body || !visible || !memory) return;
    const pending = new Map<string, Pending>();
    for (const [key, spot] of memory.get(paneId)) pending.set(key, { spot, held: 0 });
    if (pending.size === 0) return;
    restoring.current = pending;
    const started = performance.now();
    let frame = 0;
    const stop = () => {
      cancelAnimationFrame(frame);
      pending.clear();
      if (restoring.current === pending) restoring.current = null;
      for (const type of INTENT) body.removeEventListener(type, stop, true);
    };
    const step = () => {
      for (const [key, entry] of pending) {
        const adapter = adapters.current.get(key);
        const element = adapter ? null : find(body, key);
        const scroller = adapter ?? (element ? domScroller(element) : undefined);
        if (!scroller) continue;
        if (!scroller.reachable(entry.spot)) { entry.held = 0; continue; }
        if (!scroller.settled(entry.spot)) { scroller.write(entry.spot); entry.held = 0; } else if (++entry.held >= SETTLE_FRAMES) pending.delete(key);
      }
      if (pending.size === 0 || performance.now() - started > RESTORE_TIMEOUT_MS) return stop();
      frame = requestAnimationFrame(step);
    };
    // The reader taking the scroller (wheel, touch, a scrollbar drag, a key) ends the restore: their hand always wins over ours.
    for (const type of INTENT) body.addEventListener(type, stop, { capture: true, passive: true, once: true });
    step();
    return stop;
  }, [paneId, root, visible, layout, memory]);

  return controller;
}
