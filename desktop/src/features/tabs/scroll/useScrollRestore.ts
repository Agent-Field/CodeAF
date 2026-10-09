import { useLayoutEffect, type RefObject } from 'react';
import { ScrollMemory, scrollStorageKey, type ScrollSpot } from './scrollMemory';

// Per-pane scroll restoration. Only the active tab's panes are mounted (mounting every tab would multiply streams and polls), so
// a pane remounts empty each time its tab is selected. This hook remembers where every scroller INSIDE the pane body was left,
// without knowing which kind drew it, and puts each one back when content has arrived. It never moves a scroller the reader has
// touched, and it never touches a pane that has no saved position (so a conversation opened for the first time still sticks to its end).

/** How long a restore keeps waiting for content (an engine read, a list, an image) before it gives up for good. */
export const RESTORE_TIMEOUT_MS = 6000;
/** Frames a scroller must hold its place before the restore lets go of it; late layout and the conversation's own follow settle inside this. */
const SETTLE_FRAMES = 20;
const SAVE_DELAY_MS = 300;
/** Within this distance of its end a scroller counts as at its end, which restores to the end even if the content grew meanwhile. */
const END_SLACK = 1.5;

/** The one memory. It survives tab switches by living outside React and survives a reload through localStorage (design Interactions, Relaunch: "scroll positions restore"). */
const memory = ScrollMemory.parse(readSaved());
let saveTimer: ReturnType<typeof setTimeout> | undefined;

function readSaved(): string | null {
  try { return localStorage.getItem(scrollStorageKey); } catch { return null; }
}

function save() {
  saveTimer = undefined;
  try { localStorage.setItem(scrollStorageKey, memory.serialize()); } catch { /* Storage can be full or blocked; the position is a convenience. */ }
}

function scheduleSave() {
  if (saveTimer === undefined) saveTimer = setTimeout(save, SAVE_DELAY_MS);
}

if (typeof window !== 'undefined') window.addEventListener('pagehide', () => { if (saveTimer !== undefined) { clearTimeout(saveTimer); save(); } });

/** Forgets the panes that no longer belong to any tab. Called with every live pane id; an empty set is ignored so a transient empty list never wipes the memory. */
export function pruneScrollMemory(alive: ReadonlySet<string>): void {
  if (alive.size > 0 && memory.prune(alive)) scheduleSave();
}

/** Test and diagnostics read of the memory, as plain data. */
export const scrollMemorySize = () => memory.size;

/** A scroller is named by an explicit `data-scroll-key`, else by its tag and first class plus its order among look-alikes in the pane. */
function baseOf(element: HTMLElement): { selector: string; base: string } {
  const named = element.dataset.scrollKey;
  if (named) return { base: `k:${named}`, selector: `[data-scroll-key="${CSS.escape(named)}"]` };
  const first = element.classList[0];
  return first ? { base: `s:${element.localName}.${first}`, selector: `${element.localName}.${CSS.escape(first)}` } : { base: `s:${element.localName}`, selector: element.localName };
}

function keyOf(root: HTMLElement, element: HTMLElement): string {
  if (element === root) return 'root';
  const { base, selector } = baseOf(element);
  return `${base}#${Array.from(root.querySelectorAll(selector)).indexOf(element)}`;
}

function find(root: HTMLElement, key: string): HTMLElement | null {
  if (key === 'root') return root;
  const at = key.lastIndexOf('#');
  const base = key.slice(0, at);
  const index = Number(key.slice(at + 1));
  const selector = base.startsWith('k:') ? `[data-scroll-key="${CSS.escape(base.slice(2))}"]` : (() => {
    const [tag, first] = base.slice(2).split('.');
    return first ? `${tag}.${CSS.escape(first)}` : tag;
  })();
  return (root.querySelectorAll<HTMLElement>(selector)[index]) ?? null;
}

const spotOf = (element: HTMLElement): ScrollSpot => {
  const max = element.scrollHeight - element.clientHeight;
  return { top: element.scrollTop, end: max > END_SLACK && max - element.scrollTop <= END_SLACK };
};

type Pending = { spot: ScrollSpot; held: number };

/**
 * Remembers and restores every scroller inside `root` for the pane `paneId`.
 * `visible` is false while the pane is hidden (another pane is maximized): a hidden pane loses its offsets, so becoming visible restores them again.
 */
export function useScrollRestore(paneId: string, root: RefObject<HTMLElement | null>, visible: boolean): void {
  // Recording. Scroll does not bubble, so one capturing listener on the body sees every nested scroller.
  useLayoutEffect(() => {
    const body = root.current;
    if (!body) return;
    const seen = new Map<HTMLElement, ScrollSpot>();
    let frame = 0;
    const flush = () => {
      frame = 0;
      for (const [element, spot] of seen) if (body.contains(element)) {
        const key = keyOf(body, element);
        // A scroller still being restored is being moved by us (or clamped by content still arriving): not the reader's choice yet.
        if (!restoring.get(body)?.has(key)) memory.set(paneId, key, spot);
      }
      if (seen.size) scheduleSave();
      seen.clear();
    };
    const onScroll = (event: Event) => {
      const target = event.target;
      if (!(target instanceof HTMLElement) || !body.contains(target)) return;
      // A hidden pane reports offsets of nothing.
      if (body.getClientRects().length === 0) return;
      seen.set(target, spotOf(target));
      if (!frame) frame = requestAnimationFrame(flush);
    };
    body.addEventListener('scroll', onScroll, { capture: true, passive: true });
    return () => {
      body.removeEventListener('scroll', onScroll, { capture: true });
      if (frame) cancelAnimationFrame(frame);
      // The tab is being left: take what the reader last did before the nodes go.
      flush();
    };
  }, [paneId, root]);

  // Restoring. Runs after the children's own layout effects but before any scroll event they cause is delivered, so those events are already ours.
  useLayoutEffect(() => {
    const body = root.current;
    if (!body || !visible) return;
    const pending = new Map<string, Pending>();
    for (const [key, spot] of memory.get(paneId)) pending.set(key, { spot, held: 0 });
    if (pending.size === 0) return;
    restoring.set(body, pending);
    const started = performance.now();
    let frame = 0;
    const stop = () => {
      cancelAnimationFrame(frame);
      pending.clear();
      if (restoring.get(body) === pending) restoring.delete(body);
      for (const type of INTENT) body.removeEventListener(type, stop, true);
    };
    const step = () => {
      for (const [key, entry] of pending) {
        const element = find(body, key);
        if (!element) continue;
        const max = element.scrollHeight - element.clientHeight;
        // Content that has not grown tall enough yet cannot hold the offset: wait for it rather than settle for a clamp.
        if (!entry.spot.end && entry.spot.top > max + 0.5) { entry.held = 0; continue; }
        const target = entry.spot.end ? max : entry.spot.top;
        if (Math.abs(element.scrollTop - target) > 0.5) { element.scrollTop = target; entry.held = 0; } else if (++entry.held >= SETTLE_FRAMES) pending.delete(key);
      }
      if (pending.size === 0 || performance.now() - started > RESTORE_TIMEOUT_MS) return stop();
      frame = requestAnimationFrame(step);
    };
    // The reader taking the scroller (wheel, touch, a scrollbar drag, a key) ends the restore: their hand always wins over ours.
    for (const type of INTENT) body.addEventListener(type, stop, { capture: true, passive: true, once: true });
    step();
    return stop;
  }, [paneId, root, visible]);
}

const INTENT = ['wheel', 'touchstart', 'pointerdown', 'keydown'] as const;
/** Scrollers each body is still restoring, by key. The recorder consults it so a restore never records itself. */
const restoring = new WeakMap<HTMLElement, Map<string, Pending>>();
