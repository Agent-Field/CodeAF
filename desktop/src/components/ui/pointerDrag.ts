// One pointer drag for every in-app reorder, group, split, place and queue gesture.
//
// WKWebView does not start an HTML5 drag when the press lands on a button, and every tab's
// hit target is a button. Desktop strips (the ones this surface is measured against) drag
// with pointer events and a drawn ghost. A click that never moves 4px stays a click.
// Middle-click and right-click never drag. Escape and pointercancel put everything back.
// Finder file drops stay on the HTML5 path in useFileDrop: those are real OS drags.
import design from '../../design/tokens.json' with { type: 'json' };

/** Pixels the pointer must travel before a press becomes a drag. From tokens, one source. */
export const pointerDragThresholdPx = Number.parseInt(design.foundation['pointer-drag-threshold'], 10);

/** Drop targets the pointer can land on. A section is the overview's group heading, which is not a card. */
export type DropKind = 'tab' | 'group' | 'edge-zone' | 'rail-row' | 'place-tile' | 'queue-row' | 'overview-card' | 'overview-section';

/** What is in flight. `ids` is the place or chat list; `id` is the one item the gesture started on. */
export type DragPayload = {
  kind: string;
  id: string;
  ids?: readonly string[];
};

/** The pointer at a move or a release. `altKey` is Option, which files a place as a move. */
export type DragPoint = {
  x: number;
  y: number;
  screenX: number;
  screenY: number;
  altKey: boolean;
};

export type DropRegistration = {
  kind: DropKind;
  id: string;
  accepts: (payload: DragPayload) => boolean;
  hover: (point: DragPoint, payload: DragPayload) => void;
  leave: () => void;
  /** True when this target kept the drop. False lets the source treat the release as a miss. */
  drop: (point: DragPoint, payload: DragPayload) => boolean;
};

export type DragSource = {
  payload: DragPayload;
  enabled?: boolean;
  onStart?: () => void;
  onEnd?: () => void;
  /** A release no target accepted. Tear-off reads the point; everyone else ignores it. */
  onMiss?: (point: DragPoint) => void;
};

export type DragPhase = 'idle' | 'armed' | 'dragging';
export type DragSignal = 'down' | 'move' | 'up' | 'cancel' | 'escape';
export type DragEffect = 'none' | 'begin' | 'drop' | 'abort';

/**
 * The gesture, with no DOM. Button 0 arms. A move past the threshold begins.
 * A release before that is still a click. Escape and cancel abort only once
 * the drag has begun, so a press that has not moved does not swallow Escape.
 */
export function stepPointerDrag(phase: DragPhase, signal: DragSignal, input: { button?: number; distance?: number; threshold?: number } = {}): { phase: DragPhase; effect: DragEffect } {
  const threshold = input.threshold ?? pointerDragThresholdPx;
  if (phase === 'idle') {
    if (signal === 'down' && input.button === 0) return { phase: 'armed', effect: 'none' };
    return { phase: 'idle', effect: 'none' };
  }
  if (phase === 'armed') {
    if (signal === 'move') return (input.distance ?? 0) >= threshold ? { phase: 'dragging', effect: 'begin' } : { phase: 'armed', effect: 'none' };
    if (signal === 'up') return { phase: 'idle', effect: 'none' };
    if (signal === 'cancel' || signal === 'escape') return { phase: 'idle', effect: 'none' };
    return { phase: 'armed', effect: 'none' };
  }
  if (signal === 'move') return { phase: 'dragging', effect: 'none' };
  if (signal === 'up') return { phase: 'idle', effect: 'drop' };
  if (signal === 'cancel' || signal === 'escape') return { phase: 'idle', effect: 'abort' };
  return { phase: 'dragging', effect: 'none' };
}

/** True once the pointer has moved far enough that the press is no longer a click. */
export function passedDragThreshold(dx: number, dy: number, threshold = pointerDragThresholdPx): boolean {
  return dx * dx + dy * dy >= threshold * threshold;
}

/**
 * The topmost registered element that accepts the payload. Ancestors are tried
 * after the node itself, so a card inside a section wins when the card accepts
 * and the section is the fallback when it does not. A ghost is skipped entirely.
 */
export function firstAcceptingTarget<T>(elements: readonly T[], parentOf: (node: T) => T | undefined, lookup: (node: T) => { accepts: boolean } | undefined, isGhost: (node: T) => boolean): T | undefined {
  const seen = new Set<T>();
  for (const start of elements) {
    if (isGhost(start)) continue;
    let current: T | undefined = start;
    while (current !== undefined && !seen.has(current)) {
      seen.add(current);
      const found = lookup(current);
      if (found?.accepts) return current;
      current = parentOf(current);
    }
  }
  return undefined;
}

const registry = new Map<HTMLElement, DropRegistration>();

/** Registers `element` until the returned function runs. A second register replaces the first. */
export function registerDropTarget(element: HTMLElement, registration: DropRegistration): () => void {
  registry.set(element, registration);
  return () => {
    if (registry.get(element) !== registration) return;
    registry.delete(element);
  };
}

/** Test and unmount seam. A drag that outlives its screen must not land on a detached node. */
export function clearDropTargets(): void {
  registry.clear();
}

function pointOf(event: { clientX: number; clientY: number; screenX: number; screenY: number; altKey: boolean }): DragPoint {
  return { x: event.clientX, y: event.clientY, screenX: event.screenX, screenY: event.screenY, altKey: event.altKey };
}

function isGhost(node: Element): boolean {
  return node.classList.contains('pointer-drag-ghost') || !!node.closest('.pointer-drag-ghost');
}

function targetAt(x: number, y: number, payload: DragPayload): { element: HTMLElement; registration: DropRegistration } | undefined {
  const hit = firstAcceptingTarget(document.elementsFromPoint(x, y), node => node.parentElement ?? undefined, node => {
    if (!(node instanceof HTMLElement)) return undefined;
    const registration = registry.get(node);
    if (!registration) return undefined;
    return { accepts: registration.accepts(payload) };
  }, isGhost);
  if (!(hit instanceof HTMLElement)) return undefined;
  const registration = registry.get(hit);
  return registration ? { element: hit, registration } : undefined;
}

type Ghost = { move: (x: number, y: number) => void; remove: () => void };

/** A copy of the grabbed row, lifted with the rail ghost opacity and the shared sh-2 shadow. */
function mountGhost(source: HTMLElement, event: { clientX: number; clientY: number }): Ghost {
  const rect = source.getBoundingClientRect();
  const ghost = source.cloneNode(true) as HTMLElement;
  ghost.classList.add('pointer-drag-ghost');
  ghost.setAttribute('aria-hidden', 'true');
  ghost.setAttribute('inert', '');
  // A copy must not be a second tab, tile, or chat. Queries and drop targets follow those attributes.
  const scrub = (node: Element) => {
    node.removeAttribute('id');
    for (const name of ['data-place-id', 'data-chat-id', 'data-queued-id', 'data-card-id', 'data-drop', 'data-dragging', 'data-rail-item']) node.removeAttribute(name);
  };
  scrub(ghost);
  ghost.querySelectorAll('*').forEach(scrub);
  const offsetX = event.clientX - rect.left;
  const offsetY = event.clientY - rect.top;
  ghost.style.width = `${rect.width}px`;
  ghost.style.height = `${rect.height}px`;
  const move = (x: number, y: number) => {
    ghost.style.transform = `translate(${x - offsetX}px, ${y - offsetY}px)`;
  };
  move(event.clientX, event.clientY);
  document.body.appendChild(ghost);
  return { move, remove: () => ghost.remove() };
}

function suppressClick(element: HTMLElement, release: () => void) {
  const stop = (event: Event) => {
    event.preventDefault();
    event.stopPropagation();
    element.removeEventListener('click', stop, true);
    release();
  };
  element.addEventListener('click', stop, true);
}

let activeDrag = false;

/** True while a drag is past the threshold. Previews stay shut for that whole gesture. */
export function pointerDragActive(): boolean {
  return activeDrag;
}

/**
 * Arms a drag from this pointerdown. The listener stays on window so the gesture
 * survives leaving the row. Capture is taken only after the threshold, so a click
 * still lands on the button that was pressed. The previous HTML5 paths did not
 * auto-scroll a strip or a list, so this one does not either.
 */
export function armPointerDrag(event: { button: number; pointerId: number; clientX: number; clientY: number; screenX: number; screenY: number; altKey: boolean }, source: DragSource, element: HTMLElement): void {
  // React listens on the root, so the native event's currentTarget is that root, not the row. The row is passed in.
  if (event.button !== 0 || source.enabled === false) return;
  if (!(element instanceof HTMLElement)) return;
  const pointerId = event.pointerId;
  const originX = event.clientX;
  const originY = event.clientY;
  const payload = source.payload;
  let phase: DragPhase = 'armed';
  let hit: { element: HTMLElement; registration: DropRegistration } | undefined;
  let ghost: Ghost | undefined;

  const releaseCapture = () => {
    if (element.hasPointerCapture(pointerId)) element.releasePointerCapture(pointerId);
  };

  // The drop has to see the target that was current at release. Leave clears the highlight first, then the drop mutates the tree.
  const finish = (ev: { clientX: number; clientY: number; screenX: number; screenY: number; altKey: boolean }, effect: DragEffect) => {
    if (phase !== 'dragging') return;
    const current = hit;
    phase = 'idle';
    activeDrag = false;
    hit = undefined;
    current?.registration.leave();
    ghost?.remove();
    ghost = undefined;
    delete element.dataset.dragging;
    delete document.documentElement.dataset.pointerDragEffect;
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', onUp);
    window.removeEventListener('pointercancel', onCancel);
    window.removeEventListener('keydown', onKey, true);
    // Capture stays until the click that follows the release, so that click is swallowed on the grabbed row. A drop's click is the next task's predecessor, so the timer only covers a browser that never clicks.
    if (effect === 'drop') setTimeout(releaseCapture, 0);
    if (effect === 'drop') {
      const accepted = current?.registration.drop(pointOf(ev), payload) ?? false;
      if (!accepted) source.onMiss?.(pointOf(ev));
    }
    source.onEnd?.();
  };

  const onMove = (ev: PointerEvent) => {
    if (ev.pointerId !== pointerId) return;
    if (phase === 'armed') {
      const step = stepPointerDrag(phase, 'move', { distance: Math.hypot(ev.clientX - originX, ev.clientY - originY) });
      if (step.effect !== 'begin') return;
      phase = 'dragging';
      activeDrag = true;
      element.setPointerCapture(pointerId);
      suppressClick(element, releaseCapture);
      ghost = mountGhost(element, ev);
      element.dataset.dragging = 'true';
      source.onStart?.();
    }
    ghost?.move(ev.clientX, ev.clientY);
    const next = targetAt(ev.clientX, ev.clientY, payload);
    if (next?.element !== hit?.element) {
      hit?.registration.leave();
      hit = next;
    }
    hit?.registration.hover(pointOf(ev), payload);
  };

  const onUp = (ev: PointerEvent) => {
    if (ev.pointerId !== pointerId) return;
    if (phase !== 'dragging') {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onCancel);
      window.removeEventListener('keydown', onKey, true);
      return;
    }
    finish(ev, 'drop');
  };

  const onCancel = (ev: PointerEvent) => {
    if (ev.pointerId !== pointerId) return;
    releaseCapture();
    if (phase !== 'dragging') {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', onCancel);
      window.removeEventListener('keydown', onKey, true);
      return;
    }
    finish(ev, 'abort');
  };

  const onKey = (ev: KeyboardEvent) => {
    if (ev.key !== 'Escape' || phase !== 'dragging') return;
    ev.preventDefault();
    ev.stopPropagation();
    finish({ clientX: originX, clientY: originY, screenX: 0, screenY: 0, altKey: false }, 'abort');
  };

  window.addEventListener('pointermove', onMove);
  window.addEventListener('pointerup', onUp);
  window.addEventListener('pointercancel', onCancel);
  window.addEventListener('keydown', onKey, true);
}
