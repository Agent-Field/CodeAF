// The typed, shared tab actions: the things a tab can DO that reach outside the workspace (another window, the clipboard).
// The tab menu, the overview, the rail and the keys all call this one object, so what is offered and what is said when it
// fails cannot drift apart. Free of React so node tests drive it with a stub native bridge.
import type { NativeControls, PlaceKey } from '../../design/nativeControls.ts';
import type { Toasts } from '../../design/toasts.ts';
import type { Tab } from './model.ts';

/** The slice of the native adapter these actions use; the real one is `nativeControls()`. */
export type NativeForTabs = Pick<NativeControls, 'desktop' | 'currentWindow' | 'openPlaceWindow' | 'moveTabToWindow'>;
export type TabActionsDeps = {
  native: NativeForTabs;
  toasts: Pick<Toasts, 'show'>;
  writeClipboard?: (text: string) => Promise<void>;
  /** The place this window's strip shows now. A Places window can go to another place after it opened, so the window's
   * opening place is only the fallback. */
  place?: () => string | undefined;
};

export type TabActions = {
  /**
   * The canonical link that reopens this tab, or undefined when there is none. TODAY THERE IS NONE: the desktop shell
   * registers no deep-link scheme (src-tauri/tauri.conf.json has no deep-link plugin or schemes), and a tab's route lives
   * in its window's own state. A menu therefore leaves "Copy link" OFF rather than drawing it disabled, and nothing may
   * invent a `codeaf://` address. When a scheme lands, this is the one function that changes.
   */
  linkFor(tab: Tab): string | undefined;
  /** Copies `linkFor(tab)`. False when there is no link or the clipboard refused. */
  copyLink(tab: Tab): Promise<boolean>;
  /** True when the tab can travel: the desktop app, one pane (a split is separated first), and not the Inbox. */
  canMove(tab: Tab): boolean;
  /**
   * Opens a new window on this window's place carrying the tab. TWO-PHASE: the tab stays here until the new window has
   * claimed it (the workspace removes it on `window://handoff-claimed`), so a failed or abandoned move loses nothing.
   * Failures are said in a toast; the tab is never touched.
   */
  moveToNewWindow(tab: Tab): Promise<boolean>;
  /** The same, into a window that is already open. */
  moveToWindow(tab: Tab, label: string): Promise<boolean>;
};

export function createTabActions({ native, toasts, writeClipboard = text => navigator.clipboard.writeText(text), place }: TabActionsDeps): TabActions {
  // A place's Home never leaves its strip; the Inbox is the window's own.
  const canMove = (tab: Tab) => native.desktop && !tab.split && tab.kind !== 'inbox' && tab.kind !== 'home';
  const failed = (tab: Tab, what: string) => toasts.show({ message: ['Could not move ', { strong: tab.title }, ` ${what}. It is still here.`], tone: 'danger' });
  return {
    linkFor: () => undefined,
    async copyLink(tab) {
      const link = this.linkFor(tab);
      if (!link) return false;
      try { await writeClipboard(link); return true; } catch { return false; }
    },
    canMove,
    async moveToNewWindow(tab) {
      if (!canMove(tab)) return false;
      try {
        const placeKey = (place?.() as PlaceKey | undefined) ?? (await native.currentWindow()).placeKey;
        const opened = await native.openPlaceWindow(placeKey, { pane: tab });
        if (opened.moved) return true;
      } catch { /* said below */ }
      failed(tab, 'to a new window');
      return false;
    },
    async moveToWindow(tab, label) {
      if (!canMove(tab)) return false;
      try { await native.moveTabToWindow(label, tab); return true; } catch { failed(tab, 'to that window'); return false; }
    },
  };
}
