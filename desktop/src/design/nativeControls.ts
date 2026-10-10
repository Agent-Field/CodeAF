// The renderer's one door to codeaf's native controls: windows, choosers,
// notifications and the dock badge. Every call goes to a typed Rust command
// (src-tauri/src/windows.rs, dialogs.rs, notifications.rs). The capability
// still names dialog:allow-open and notification:default, because that is the
// reviewed allow-list for main and w-*; shell and filesystem grants stay off.
//
// Kept free of React so node tests can import it. The bridge is injectable; the
// default one talks to Tauri, and outside Tauri every call answers honestly that
// the desktop app is needed rather than pretending to work.

import { invoke as tauriInvoke, isTauri } from '@tauri-apps/api/core';
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow';
import type { Pane } from '../features/tabs/types.ts';
import { isTabKind, type TabKind } from '../features/tabs/kinds/types.ts';
import { cleanView, type TabView } from '../features/tabs/view-state.ts';
import { legacyTerminalView } from '../features/terminal/target.ts';

// ---------------------------------------------------------------------------
// Shapes. These mirror the Rust structs field for field; Rust refuses unknown
// fields, so a field added here and not there fails loudly, not silently.

/** `now` (unplaced), `root` (All places) or a placegraph id. */
export type PlaceKey = 'now' | 'root' | `pl_${string}`;

export type TabHandoff = {
  kind: TabKind;
  title: string;
  draft?: string;
  titleSource?: 'message' | 'engine' | 'manual';
  sessionFile?: string;
  path?: string;
  file?: { path: string; view?: 'changes' | 'file' };
  route?: { taskId?: string; back: string[]; forward: string[] };
  target?: { sessionId?: string; path?: string; terminalId?: string; url?: string };
};

export type ClaimedHandoff = TabHandoff & { handoffId: string };
export type Point = { x: number; y: number };
export type WindowRow = { label: string; placeKey: PlaceKey; focused: boolean; title: string };
export type WindowContext = { label: string; placeKey: PlaceKey };

export type PickedPath = { path: string; name: string };
export type PickResult =
  | { status: 'picked'; paths: PickedPath[] }
  | { status: 'cancelled' }
  | { status: 'busy' }
  /** Outside the desktop app there is no chooser that can hand back a real path. */
  | { status: 'unavailable' };

export type AttentionKind = 'needsYou' | 'failed' | 'running';
/** A question as its conversation's tray names it: the engine's question kind and id. */
export type NoticeQuestion = { kind: string; id: number };
/** Where a click on a system notification lands: a conversation and, while it waits, the question to focus. */
export type NoticeTarget = { chatId: string; question?: NoticeQuestion };
export type AttentionItem = { id: string; kind: AttentionKind; chatTitle: string; text: string; placeId?: string; placeName?: string } & Partial<NoticeTarget>;
export type NotificationPermission = { state: 'granted' | 'denied' | 'unavailable'; verified: boolean };
export type NotifyResult = { posted: number; groups: number; skipped: 'focused' | 'nothing-new' | 'denied' | 'stale' | 'unavailable' | null };
/** On Linux `applied` means the launcher was asked; whether it draws a count depends on the desktop. */
export type BadgeResult = { applied: boolean; reason?: 'unavailable' | 'launcher-dependent' | 'stale' };

// ---------------------------------------------------------------------------
// Limits, the same numbers windows.rs enforces.

export const HANDOFF_LIMITS = { title: 200, draft: 64 * 1024, path: 4096, id: 256, route: 64, url: 8192 } as const;
const DESKTOP_ONLY = 'That needs the desktop app';

const plain = (text: string) => !/[\u0000-\u001f\u007f-\u009f]/.test(text);
const bounded = (text: string, max: number) => text.length <= max && plain(text);
const idLike = (text: string) => text !== '' && bounded(text, HANDOFF_LIMITS.id);
const relative = (text: string) => text !== '' && bounded(text, HANDOFF_LIMITS.path);
const absolute = (text: string) => bounded(text, HANDOFF_LIMITS.path) && (text.startsWith('/') || /^[A-Za-z]:[\\/]/.test(text));

export function isPlaceKey(key: unknown): key is PlaceKey {
  return typeof key === 'string' && (key === 'now' || key === 'root' || /^pl_[0-9a-f]{16}$/.test(key));
}

/** Only http(s) with a host and no `user:password@` travels with a moved tab. */
export function isSafeWebUrl(url: string): boolean {
  const match = /^https?:\/\/([^/?#]*)/.exec(url);
  if (!match || url.length > HANDOFF_LIMITS.url || /[\s\u0000-\u001f\u007f]/.test(url)) return false;
  const authority = match[1];
  return !authority.includes('@') && authority.split(':')[0] !== '';
}

/** The reason a handoff would be refused by Rust, or undefined when it is sound. */
export function handoffProblem(tab: TabHandoff): string | undefined {
  const route = tab.route;
  const target = tab.target;
  const ok =
    isTabKind(tab.kind) &&
    bounded(tab.title, HANDOFF_LIMITS.title) &&
    (tab.draft === undefined || (tab.draft.length <= HANDOFF_LIMITS.draft && !tab.draft.includes('\0'))) &&
    (tab.sessionFile === undefined || absolute(tab.sessionFile)) &&
    (tab.path === undefined || relative(tab.path)) &&
    (tab.file === undefined || relative(tab.file.path)) &&
    (route === undefined ||
      ((route.taskId === undefined || idLike(route.taskId)) &&
        route.back.length <= HANDOFF_LIMITS.route &&
        route.forward.length <= HANDOFF_LIMITS.route &&
        [...route.back, ...route.forward].every(id => bounded(id, HANDOFF_LIMITS.id)))) &&
    (target === undefined ||
      ((target.sessionId === undefined || idLike(target.sessionId)) &&
        (target.terminalId === undefined || idLike(target.terminalId)) &&
        (target.path === undefined || relative(target.path)) &&
        (target.url === undefined || isSafeWebUrl(target.url))));
  return ok ? undefined : 'That tab cannot move to another window';
}

/**
 * The movable part of a pane. Window-local reading state (folds, open
 * disclosures, the tasks panel, the selected task row) and the captured `shot`
 * stay behind; the target window draws its own.
 */
export function handoffFromPane(pane: Pane): TabHandoff {
  const tab: TabHandoff = { kind: pane.kind, title: pane.title };
  if (pane.draft) tab.draft = pane.draft;
  if (pane.titleSource) tab.titleSource = pane.titleSource;
  if (pane.sessionFile) tab.sessionFile = pane.sessionFile;
  if (pane.path) tab.path = pane.path;
  if (pane.file) tab.file = pane.file.view ? { path: pane.file.path, view: pane.file.view } : { path: pane.file.path };
  if (pane.route) tab.route = { ...(pane.route.taskId ? { taskId: pane.route.taskId } : {}), back: [...pane.route.back], forward: [...pane.route.forward] };
  // A terminal tab saved before its target was durable still names its shell in the legacy binding; carry that.
  const view = pane.kind === 'terminal' ? legacyTerminalView(pane) : undefined;
  if (view?.sessionFile) tab.sessionFile = view.sessionFile;
  const named = view?.target ?? pane.target;
  if (named) {
    const { sessionId, path, terminalId, url } = pane.kind === 'terminal' ? { ...named, sessionId: undefined } : named;
    const target = Object.fromEntries(Object.entries({ sessionId, path, terminalId, url }).filter(([, v]) => typeof v === 'string' && v)) as TabHandoff['target'];
    if (target && Object.keys(target).length) tab.target = target;
  }
  return tab;
}

/** A claimed handoff as a fresh pane with the receiving window's own id; every view field is re-validated. */
export function paneFromHandoff(tab: TabHandoff, id: string): Pane {
  const view: TabView = cleanView(tab as unknown as Record<string, unknown>);
  return { id, kind: isTabKind(tab.kind) ? tab.kind : 'conversation', title: tab.title, draft: tab.draft ?? '', ...(tab.titleSource ? { titleSource: tab.titleSource } : {}), ...view };
}

/** The event Rust emits to the one window a notification click was queued for (src-tauri/src/activation.rs). */
export const NOTICE_ACTIVATED_EVENT = 'notification://activated';

const NOTICE_ID = /^[A-Za-z0-9_-]{1,128}$/;
const QUESTION_KIND = /^[A-Za-z0-9_-]{1,64}$/;
const isQuestion = (q: unknown): q is NoticeQuestion => !!q && typeof q === 'object'
  && typeof (q as NoticeQuestion).kind === 'string' && QUESTION_KIND.test((q as NoticeQuestion).kind)
  && Number.isSafeInteger((q as NoticeQuestion).id) && (q as NoticeQuestion).id > 0;

/**
 * The click target an attention item may carry, by the rules activation.rs checks. A conversation or question that
 * does not fit is left off rather than sent, because Rust refuses the WHOLE list over one bad item.
 */
export function noticeTarget(chatId: string | undefined, question?: { kind: string; id?: number }): Partial<NoticeTarget> {
  if (!chatId || !NOTICE_ID.test(chatId)) return {};
  const asked = question && { kind: question.kind, id: question.id ?? 0 };
  return isQuestion(asked) ? { chatId, question: asked } : { chatId };
}

/** A target as claimed from Rust, or nothing when it is not that shape. */
export function claimedTarget(raw: unknown): NoticeTarget | undefined {
  if (!raw || typeof raw !== 'object') return undefined;
  const { chatId, question } = raw as Record<string, unknown>;
  if (typeof chatId !== 'string' || !NOTICE_ID.test(chatId)) return undefined;
  if (question === undefined) return { chatId };
  return isQuestion(question) ? { chatId, question: { kind: question.kind, id: question.id } } : undefined;
}

/** The badge counts questions waiting on the person; failures and running work do not. */
/** A world-feed sequence as Rust accepts it: a non-negative safe integer, else 0 (the oldest reading). */
export const feedSeq = (seq: number): number => (Number.isSafeInteger(seq) && seq > 0 ? seq : 0);

export function needsYouCount(items: readonly AttentionItem[]): number {
  return new Set(items.filter(item => item.kind === 'needsYou').map(item => item.id)).size;
}

/**
 * The places-routes `Attention` row as a notification item. A chat can have one
 * open question and several running tasks; the id keeps them apart.
 */
export function attentionItem(row: { kind: 'needsYou' | 'running' | 'failed'; chatId: string; chatTitle: string; placeId?: string; placeName?: string; text: string; taskId?: string }): AttentionItem {
  const item: AttentionItem = { id: row.taskId ? `${row.chatId}:${row.taskId}` : row.chatId, kind: row.kind, chatTitle: row.chatTitle, text: row.text };
  if (row.placeId && row.placeId !== 'now' && row.placeId !== 'root') item.placeId = row.placeId;
  if (row.placeName && item.placeId) item.placeName = row.placeName;
  return item;
}

/** The place a window was opened on, from its own address. Main and an unknown value are `now`. */
export function placeFromSearch(search: string): PlaceKey {
  const key = new URLSearchParams(search).get('place');
  return isPlaceKey(key) ? key : 'now';
}

// ---------------------------------------------------------------------------
// The bridge.

export type Unlisten = () => void;
export type NativeBridge = {
  desktop: boolean;
  invoke<T>(command: string, args?: Record<string, unknown>): Promise<T>;
  /** Listens for events addressed to THIS window only. */
  listen<T>(event: string, handler: (payload: T) => void): Promise<Unlisten>;
  /** The address of this document, for the place key and the browser fallback. */
  location: { pathname: string; search: string };
  openBrowserTab(url: string): void;
};

function defaultBridge(): NativeBridge {
  const desktop = isTauri();
  return {
    desktop,
    invoke: (command, args) => tauriInvoke(command, args),
    // A global `listen` also hears events emitted to OTHER windows; scoping the
    // listener to this window is what keeps a handoff with the window it names.
    listen: async (event, handler) => getCurrentWebviewWindow().listen(event, e => handler(e.payload as never)),
    location: globalThis.location ?? { pathname: '/', search: '' },
    openBrowserTab: url => {
      globalThis.open?.(url, '_blank', 'noopener,noreferrer');
    },
  };
}

export type NativeControls = ReturnType<typeof createNativeControls>;

export function createNativeControls(bridge: NativeBridge = defaultBridge()) {
  /** Handoffs this window started; only these may remove a tab here. */
  const started = new Map<string, string>();

  const needDesktop = () => {
    if (!bridge.desktop) throw new Error(DESKTOP_ONLY);
  };

  return {
    desktop: bridge.desktop,

    /** This window's label and place. In a browser the label is `main` and the place comes from the address. */
    async currentWindow(): Promise<WindowContext> {
      if (!bridge.desktop) return { label: 'main', placeKey: placeFromSearch(bridge.location.search) };
      return bridge.invoke<WindowContext>('window_context');
    },

    /**
     * Opens a window on a place. With a pane, it is MOVED: call `removeOnClaim`
     * with the returned handoff id and the tab leaves this window only once the
     * new window has claimed it. In a browser a tab opens on the place; nothing
     * can be moved there, so `moved` is false.
     */
    async openPlaceWindow(placeKey: PlaceKey, options: { pane?: Pane; at?: Point; focusTab?: string } = {}): Promise<{ label?: string; handoffId?: string; moved: boolean }> {
      if (!isPlaceKey(placeKey)) throw new Error('That is not a place codeaf knows');
      if (options.focusTab !== undefined && !idLike(options.focusTab)) throw new Error('That tab cannot be focused');
      const handoff = options.pane ? handoffFromPane(options.pane) : undefined;
      const problem = handoff && handoffProblem(handoff);
      if (problem) throw new Error(problem);
      if (!bridge.desktop) {
        bridge.openBrowserTab(`${bridge.location.pathname}?place=${encodeURIComponent(placeKey)}`);
        return { moved: false };
      }
      const request: Record<string, unknown> = { placeKey };
      if (handoff) request.handoff = handoff;
      if (options.at) request.at = options.at;
      if (options.focusTab) request.focusTab = options.focusTab;
      const result = await bridge.invoke<string | { label: string; handoffId?: string }>('window_open', { request });
      const opened = typeof result === 'string' ? { label: result } : result;
      if (opened.handoffId && options.pane) started.set(opened.handoffId, options.pane.id);
      return { label: opened.label, handoffId: opened.handoffId, moved: Boolean(opened.handoffId) };
    },

    /** Moves a pane into another open window. The source keeps it until that window claims it. */
    async moveTabToWindow(label: string, pane: Pane): Promise<{ handoffId: string }> {
      needDesktop();
      const handoff = handoffFromPane(pane);
      const problem = handoffProblem(handoff);
      if (problem) throw new Error(problem);
      const moved = await bridge.invoke<{ handoffId: string }>('window_move_tab', { request: { to: label, handoff } });
      started.set(moved.handoffId, pane.id);
      return moved;
    },

    /** The tab handed to this window, once. Call at boot and on every `onHandoffReady`. */
    async claimHandoff(): Promise<ClaimedHandoff | undefined> {
      if (!bridge.desktop) return undefined;
      return (await bridge.invoke<ClaimedHandoff | null>('window_claim_handoff')) ?? undefined;
    },

    /** Another window has a tab waiting for this one. */
    onHandoffReady(handler: () => void): Promise<Unlisten> {
      if (!bridge.desktop) return Promise.resolve(() => {});
      return bridge.listen<{ handoffId: string }>('window://handoff-ready', () => handler());
    },

    /**
     * The target has the tab: remove it here. Only handoffs this window started
     * are honoured, so a stray or forged event cannot close an unrelated tab.
     */
    onHandoffClaimed(removePane: (paneId: string) => void): Promise<Unlisten> {
      if (!bridge.desktop) return Promise.resolve(() => {});
      return bridge.listen<{ handoffId: string }>('window://handoff-claimed', payload => {
        const paneId = started.get(payload?.handoffId);
        if (!paneId) return;
        started.delete(payload.handoffId);
        removePane(paneId);
      });
    },

    async listWindows(): Promise<WindowRow[]> {
      if (!bridge.desktop) return [];
      return bridge.invoke<WindowRow[]>('window_list');
    },

    async focusWindow(label: string): Promise<void> {
      needDesktop();
      await bridge.invoke('window_focus', { label });
    },

    /** Names this window after its place ("Marketing — codeaf"); empty means plain "codeaf". */
    async setWindowTitle(title: string): Promise<void> {
      if (!bridge.desktop) return;
      await bridge.invoke('window_set_title', { title });
    },

    pickFolder(title?: string): Promise<PickResult> {
      return pick({ kind: 'folder', ...(title ? { title } : {}) });
    },

    pickFiles(options: { multiple?: boolean; title?: string } = {}): Promise<PickResult> {
      return pick({ kind: 'file', multiple: Boolean(options.multiple), ...(options.title ? { title: options.title } : {}) });
    },

    async notificationPermission(ask = false): Promise<NotificationPermission> {
      if (!bridge.desktop) return { state: 'unavailable', verified: false };
      return bridge.invoke<NotificationPermission>(ask ? 'notify_request_permission' : 'notify_permission');
    },

    /**
     * Hands the whole current attention list to Rust, which announces only what
     * is new, only while no codeaf window is focused, one notification per place.
     * Send the full list every time: an item missing from it counts as answered. `seq` is the world-feed sequence the
     * list was derived from; a window behind another window's reading is ignored ('stale'), so it cannot bring an
     * answered question back.
     */
    async notifyAttention(items: readonly AttentionItem[], seq: number, epoch?: string): Promise<NotifyResult> {
      if (!bridge.desktop) return { posted: 0, groups: 0, skipped: 'unavailable' };
      return bridge.invoke<NotifyResult>('notify_attention', { items, seq: feedSeq(seq), epoch: epoch ?? '' });
    },

    /**
     * The notification clicks queued for THIS window, oldest first, each handed over once. Rust queues one only from
     * the platform's own activation of a notification it posted, with the target fixed when it was posted.
     */
    async claimNotices(): Promise<NoticeTarget[]> {
      if (!bridge.desktop) return [];
      const claimed = await bridge.invoke<unknown>('notify_claim');
      return Array.isArray(claimed) ? claimed.flatMap(raw => claimedTarget(raw) ?? []) : [];
    },

    /** Calls back whenever a notification click has been queued for this window. */
    onNoticeActivated(handler: () => void): Promise<Unlisten> {
      if (!bridge.desktop) return Promise.resolve(() => {});
      return bridge.listen(NOTICE_ACTIVATED_EVENT, () => handler());
    },

    /** Sets the dock or launcher badge to the needs-you count; 0 clears it. `seq` is as for notifyAttention. */
    async setBadge(count: number, seq: number, epoch?: string): Promise<BadgeResult> {
      if (!bridge.desktop) return { applied: false, reason: 'unavailable' };
      const value = Number.isFinite(count) ? Math.max(0, Math.min(9999, Math.floor(count))) : 0;
      return bridge.invoke<BadgeResult>('badge_set', { count: value, seq: feedSeq(seq), epoch: epoch ?? '' });
    },
  };

  async function pick(request: { kind: 'folder' | 'file'; multiple?: boolean; title?: string }): Promise<PickResult> {
    if (!bridge.desktop) return { status: 'unavailable' };
    const result = await bridge.invoke<PickResult>('dialog_pick', { request });
    // Never hand on a path the chooser did not return in the expected shape.
    if (result?.status === 'picked') {
      const paths = Array.isArray(result.paths) ? result.paths.filter(p => typeof p?.path === 'string' && p.path && typeof p.name === 'string') : [];
      return paths.length ? { status: 'picked', paths } : { status: 'cancelled' };
    }
    return result?.status === 'busy' ? { status: 'busy' } : { status: 'cancelled' };
  }
}

let shared: NativeControls | undefined;
/** The app-wide instance; the handoffs a window started live with it. */
export function nativeControls(): NativeControls {
  shared ??= createNativeControls();
  return shared;
}
