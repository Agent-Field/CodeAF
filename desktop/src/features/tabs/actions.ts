// The typed, shared tab actions: the things a tab can DO that reach outside the workspace (another window, the clipboard).
// The tab menu, the overview, the rail and the keys all call this one object, so what is offered and what is said when it
// fails cannot drift apart. Free of React so node tests drive it with a stub native bridge.
import type { NativeControls } from '../../design/nativeControls.ts';
import type { Toasts } from '../../design/toasts.ts';
import type { Tab } from './model.ts';
import { linkForTab } from './links/deepLinks.ts';

/** The slice of the native adapter these actions use; the real one is `nativeControls()`. */
export type NativeForTabs = Pick<NativeControls, 'desktop' | 'currentWindow' | 'openPlaceWindow' | 'moveTabToWindow'>;
export type TabActionsDeps = {
  native: NativeForTabs;
  toasts: Pick<Toasts, 'show'>;
  writeClipboard?: (text: string) => Promise<void>;
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
   * Failures are said in a toast; the tab is never touched.
   */
  moveToNewWindow(tab: Tab): Promise<boolean>;
  /** The same, into a window that is already open. */
  moveToWindow(tab: Tab, label: string): Promise<boolean>;
};

export function createTabActions({ native, toasts, writeClipboard = text => navigator.clipboard.writeText(text) }: TabActionsDeps): TabActions {
  const canMove = (tab: Tab) => native.desktop && !tab.split && tab.kind !== 'inbox';
  const failed = (tab: Tab, what: string) => toasts.show({ message: ['Could not move ', { strong: tab.title }, ` ${what}. It is still here.`], tone: 'danger' });
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
    async moveToNewWindow(tab) {
      if (!canMove(tab)) return false;
      try {
        const { placeKey } = await native.currentWindow();
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
