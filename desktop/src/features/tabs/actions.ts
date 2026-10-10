// The typed, shared tab actions: the things a tab can DO that reach outside the workspace (another window, the clipboard).
// The tab menu, the overview, the rail and the keys all call this one object, so what is offered and what is said when it
// fails cannot drift apart. Free of React so node tests drive it with a stub native bridge.
import type { NativeControls, PlaceKey, Point } from '../../design/nativeControls.ts';
import type { Toasts } from '../../design/toasts.ts';
import type { Tab } from './model.ts';
import { linkForTab } from './links/deepLinks.ts';

/** The slice of the native adapter these actions use; the real one is `nativeControls()`. */
export type NativeForTabs = Pick<NativeControls, 'desktop' | 'currentWindow' | 'openPlaceWindow' | 'moveTabToWindow'>;
/**
 * How long a handoff may stay unclaimed before we say so. The native side drops an unclaimed handoff after 60 s
 * (`HANDOFF_TTL` in src-tauri/src/windows.rs); a couple of seconds more lets a claim made at the last moment arrive.
 */
export const HANDOFF_CLAIM_WAIT_MS = 62_000;

export type TabActionsDeps = {
  native: NativeForTabs;
  toasts: Pick<Toasts, 'show'>;
  writeClipboard?: (text: string) => Promise<void>;
  /** The place this window's strip shows now. A Places window can go to another place after it opened, so the window's
   * opening place is only the fallback. */
  place?: () => string | undefined;
  /** True while this window still holds the tab. A claimed tab has been released and a closed one is no longer ours to report. */
  stillHere?: (tabId: string) => boolean;
  /** Opens another view of the same canonical tab set, without copying or closing a shared tab. */
  handoffView?: (tabId: string) => void;
  setTimer?: (fn: () => void, ms: number) => unknown;
};

export type TabActions = {
  /**
   * The link that reopens this tab, or undefined when there is none (links/deepLinks.ts says what each kind copies).
   * A saved conversation, task, file, diff or terminal copies a `codeaf://` link to its DURABLE target; a web tab copies
   * its own address. A new tab, Settings, History, the Inbox and a conversation never sent have none, and the menu then
   * leaves "Copy link" OFF rather than drawing it disabled. An absolute path is never offered as a link.
   */
  linkFor(tab: Tab): string | undefined;
  /** Copies `linkFor(tab)` and says so in the shared toast. False when there is no link or the clipboard refused. */
  copyLink(tab: Tab): Promise<boolean>;
  /** True when the tab can travel: the desktop app, one pane (a split is separated first), and not the Inbox. */
  canMove(tab: Tab): boolean;
  /**
   * Opens a new window on this window's place carrying the tab. TWO-PHASE: the tab stays here until the new window has
   * claimed it (the workspace removes it on `window://handoff-claimed`), so a failed or abandoned move loses nothing.
   * Failures are said in a toast; the tab is never touched. A handoff nobody claims within `HANDOFF_CLAIM_WAIT_MS` is
   * said once, in a toast, and the tab stays.
   * A drag passes `at`, the new window's top-left in logical screen coordinates, including a negative one.
   * The menu omits it and the window cascades.
   */
  moveToNewWindow(tab: Tab, at?: Point): Promise<boolean>;
  /** The same, into a window that is already open. */
  moveToWindow(tab: Tab, label: string): Promise<boolean>;
  /** This window stops showing a tab a new window just opened on. The tab stays in the strip. */
  releaseFocus(tab: Tab): void;
  /** A new window did not open. The tab is untouched, and the shared toast says it is still here. */
  moveFailed(tab: Tab): void;
};

export function createTabActions({ native, toasts, writeClipboard = text => navigator.clipboard.writeText(text), place, stillHere, handoffView, setTimer = (fn, ms) => setTimeout(fn, ms) }: TabActionsDeps): TabActions {
  // A place's Home never leaves its strip; the Inbox is the window's own.
  const canMove = (tab: Tab) => native.desktop && (handoffView !== undefined || !tab.split) && tab.kind !== 'inbox' && tab.kind !== 'home';
  const failed = (tab: Tab, what: string) => toasts.show({ message: ['Could not move ', { strong: tab.title }, ` ${what}. It is still here.`], tone: 'danger' });
  /** Said once, and only if nothing claimed the tab: the source kept it, so nothing is lost, but the person is told the window did not take it. */
  const watch = (tab: Tab, where: string) => {
    if (!stillHere) return;
    setTimer(() => { if (stillHere(tab.id)) toasts.show({ message: [{ strong: tab.title }, ` was not taken by ${where}. It is still here.`], tone: 'danger' }); }, HANDOFF_CLAIM_WAIT_MS);
  };
  return {
    linkFor: linkForTab,
    async copyLink(tab) {
      const link = linkForTab(tab);
      if (!link) return false;
      try { await writeClipboard(link); }
      catch {
        toasts.show({ message: ['Could not copy the link to ', { strong: tab.title }], tone: 'danger' });
        return false;
      }
      toasts.show({ message: ['Copied the link to ', { strong: tab.title }] });
      return true;
    },
    canMove,
    async moveToNewWindow(tab, at) {
      if (!canMove(tab)) return false;
      try {
        const placeKey = (place?.() as PlaceKey | undefined) ?? (await native.currentWindow()).placeKey;
        const where = at ? { at } : {};
        if (handoffView) {
          await native.openPlaceWindow(placeKey, { focusTab: tab.id, ...where });
          handoffView(tab.id);
          return true;
        }
        const opened = await native.openPlaceWindow(placeKey, { pane: tab, ...where });
        if (opened.moved) { watch(tab, 'the new window'); return true; }
      } catch { /* said below */ }
      failed(tab, 'to a new window');
      return false;
    },
    async moveToWindow(tab, label) {
      if (!canMove(tab)) return false;
      try { await native.moveTabToWindow(label, tab); watch(tab, 'that window'); return true; } catch { failed(tab, 'to that window'); return false; }
    },
    releaseFocus(tab) { handoffView?.(tab.id); },
    moveFailed(tab) { failed(tab, 'to a new window'); },
  };
}
