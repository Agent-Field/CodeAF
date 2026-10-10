// Refcount for "something of the app is drawn over a web page".
//
// A native web view paints above the DOM, so a menu, palette, Quick Look,
// overview, hover preview, toast or drag ghost that intersects a page would
// slide underneath it. The first hold hides every web view on this window.
// The last release shows the panes that still want to be on screen. A pane
// closed while the count is above zero is forgotten first, so the release
// does not bring it back. Calling a release twice does not show twice.
//
// Menus, popovers, the command palette, Go to, Quick Look, the overview and
// hover previews are already covers (features/web/covers.ts). views.ts takes
// one hold while any of those overlaps a page. Toasts carry data-native-cover
// so the same path sees them. A tab or group drag has no element of its own;
// the drag guard below holds for the drag.

export type WebOverlayTransport = {
  hideAll(): Promise<void>;
  showAll(panes: readonly string[]): Promise<void>;
};

const TAB_DRAGS = ['application/codeaf-tab', 'application/codeaf-group'];

const nativeTransport: WebOverlayTransport = {
  async hideAll() {
    const { invoke, isTauri } = await import('@tauri-apps/api/core');
    if (!isTauri()) return;
    await invoke('web_hide_all');
  },
  async showAll(panes) {
    const { invoke, isTauri } = await import('@tauri-apps/api/core');
    if (!isTauri()) return;
    await invoke('web_show_all', { panes: [...panes] });
  },
};

let transport = nativeTransport;
/** One entry per live hold. The length is the refcount. */
const reasons: string[] = [];
/** Pane id to "wants to be on screen, apart from this hold". */
const tracked = new Map<string, boolean>();
const listeners = new Set<() => void>();
let concealed = false;
let scheduled = false;
let chain: Promise<void> = Promise.resolve();
let dragRelease: (() => void) | null = null;
let dragGuard = false;

const emit = () => listeners.forEach(listener => listener());

function wantedPanes(): string[] {
  return [...tracked.entries()].filter(([, show]) => show).map(([pane]) => pane);
}

/**
 * One native hide or show, in order. A hold that arrives while a show is in
 * flight hides again afterwards, and a release during a hide shows afterwards.
 * Collapsing to the matching call is what keeps a nested menu at one hide.
 */
function pump() {
  if (scheduled) return;
  scheduled = true;
  chain = chain.then(async () => {
    scheduled = false;
    try {
      if (reasons.length > 0 && !concealed) {
        await transport.hideAll();
        concealed = true;
      } else if (reasons.length === 0 && concealed) {
        concealed = false;
        await transport.showAll(wantedPanes());
      }
    } catch {
      // A failed native call must not retry forever. views.ts still hides and
      // shows each pane on its own.
      concealed = reasons.length > 0;
    }
    if ((reasons.length > 0) !== concealed) pump();
  }).catch(() => {
    scheduled = false;
  });
}

/** True while at least one hold is open. The sheet under every web page is blank. */
export function webOverlayHeld(): boolean {
  return reasons.length > 0;
}

export function subscribeWebOverlay(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * A native view the window has. `wantsShow` is whether it should be on screen
 * once no overlay is up: a hidden tab passes false, so the release leaves it
 * hidden. Call again as that changes.
 */
export function trackWebPane(pane: string, wantsShow: boolean): void {
  tracked.set(pane, wantsShow);
}

/** The view is gone. A release after this will not show it. */
export function forgetWebPane(pane: string): void {
  tracked.delete(pane);
}

/**
 * Hold every web view on this window down until the returned function runs.
 * Nested holds hide once. The last release shows once. A second call of the
 * returned function does nothing.
 */
export function holdWebOverlay(reason: string): () => void {
  reasons.push(reason);
  if (reasons.length === 1) pump();
  emit();
  let released = false;
  return () => {
    if (released) return;
    released = true;
    const at = reasons.indexOf(reason);
    if (at >= 0) reasons.splice(at, 1);
    if (reasons.length === 0) pump();
    emit();
  };
}

/** Resolves when the hide or show queued so far has finished. */
export function whenWebOverlayIdle(): Promise<void> {
  return chain;
}

/** Tab and group drags. A second start while one is in flight does not hold again. */
export function webOverlayDragStart(types: readonly string[]): void {
  if (dragRelease || !TAB_DRAGS.some(type => types.includes(type))) return;
  dragRelease = holdWebOverlay('drag');
}

/** Ends the drag hold. A second end is a no-op. */
export function webOverlayDragEnd(): void {
  const release = dragRelease;
  dragRelease = null;
  release?.();
}

/** Listens for tab and group drags. Safe to call once from the web-view life cycle. */
export function installWebOverlayDragGuard(): void {
  if (dragGuard || typeof document === 'undefined') return;
  dragGuard = true;
  document.addEventListener('dragstart', event => {
    const types = event.dataTransfer?.types;
    const list: string[] = [];
    if (types) {
      for (let index = 0; index < types.length; index += 1) {
        const value = types[index];
        if (value) list.push(value);
      }
    }
    webOverlayDragStart(list);
  });
  document.addEventListener('dragend', () => webOverlayDragEnd());
}

/** Test seam. Pass null to restore the native commands. */
export function setWebOverlayTransport(next: WebOverlayTransport | null): void {
  transport = next ?? nativeTransport;
}

/** Drops holds, tracked panes and the queue. Tests start from an empty window. */
export function resetWebOverlayForTests(): void {
  reasons.length = 0;
  tracked.clear();
  concealed = false;
  scheduled = false;
  dragRelease = null;
  chain = Promise.resolve();
  listeners.clear();
  transport = nativeTransport;
}
