// Both directions of a tab's link, for THIS window. In: claim the codeaf links queued for this window (at boot, for a
// link that launched the app, and whenever Rust says another arrived) and focus or open the tab each names. Out: the
// Copy link chord copies the active tab's link through the one TabActions object the menu uses.
// A clicked system notification arrives the same way: Rust queues its conversation for this window, the conversation
// opens through the same link path, and the question it named is brought forward in that conversation's tray.
// Outside the desktop app nothing can arrive, so only the chord is live there.
import { useEffect, useRef, type Dispatch } from 'react';
import { nativeLinks, type NativeLinks } from '../../../design/nativeLinks';
import { nativeControls, type NativeControls } from '../../../design/nativeControls';
import { questionFocus } from '../../conversation/questionFocus';
import { isCopyLinkShortcut } from '../../../design/keyboard';
import { toasts } from '../../../design/toasts';
import type { TabActions } from '../actions';
import { focusedPane, type WorkspaceAction, type WorkspaceState } from '../model';
import { linkOf, parseDeepLink } from './deepLinks';
import { engineLinks } from './engine';
import { resolveLink, type LinkEngine } from './openLink';

type Options = {
  enabled: boolean;
  state: WorkspaceState;
  dispatch: Dispatch<WorkspaceAction>;
  actions: Pick<TabActions, 'copyLink' | 'linkFor'>;
  native?: NativeLinks;
  notices?: Pick<NativeControls, 'desktop' | 'claimNotices' | 'onNoticeActivated'>;
  engine?: LinkEngine;
};

export function useTabLinks({ enabled, state, dispatch, actions, native = nativeLinks(), notices = nativeControls(), engine = engineLinks }: Options) {
  const latest = useRef(state);
  latest.current = state;

  useEffect(() => {
    if (!native.desktop) return;
    /** Links being resolved now, by canonical spelling: the same link twice opens one tab. */
    const pending = new Set<string>();
    let gone = false;
    async function open(raw: string) {
      const parsed = parseDeepLink(raw);
      const key = parsed.ok ? parsed.canonical : raw;
      if (pending.has(key)) return;
      pending.add(key);
      try {
        const outcome = await resolveLink(raw, () => latest.current.tabs, engine);
        // The claim is made: even if this effect was torn down meanwhile, the link must land, or it would open nowhere.
        if (outcome.kind === 'focus') dispatch({ type: 'select', id: outcome.id });
        else if (outcome.kind === 'open') dispatch({ type: 'open', tab: outcome.tab, background: false });
        else toasts.show({ message: [outcome.sentence], tone: 'danger' });
      } finally { pending.delete(key); }
    }
    async function claim() {
      let links: string[];
      try { links = await native.claim(); } catch { return; }
      for (const link of links) void open(link);
    }
    async function claimNotices() {
      let targets: Awaited<ReturnType<typeof notices.claimNotices>>;
      try { targets = await notices.claimNotices(); } catch { return; }
      for (const target of targets) {
        // Asked for before the tab opens, so a conversation that mounts for it finds the request already waiting.
        if (target.question) questionFocus.request(target.chatId, target.question);
        void open(linkOf({ kind: 'chat', chatId: target.chatId }));
      }
    }
    let stop: (() => void) | undefined;
    void native.onReady(() => void claim()).then(off => { if (gone) off(); else stop = off; }, () => undefined);
    void claim();
    let stopNotices: (() => void) | undefined;
    if (notices.desktop) {
      void notices.onNoticeActivated(() => void claimNotices()).then(off => { if (gone) off(); else stopNotices = off; }, () => undefined);
      void claimNotices();
    }
    return () => { gone = true; stop?.(); stopNotices?.(); };
  }, [dispatch, native, notices, engine]);

  useEffect(() => {
    if (!enabled) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !isCopyLinkShortcut(event)) return;
      const active = latest.current.tabs.find(tab => tab.id === latest.current.activeId);
      if (!active) return;
      const kind = focusedPane(active).kind;
      // File and diff tabs keep the chord as Copy path; a terminal keeps it as the shell's own copy.
      if (kind === 'file' || kind === 'diff' || (event.target instanceof Element && event.target.closest('.terminal-field'))) return;
      if (!actions.linkFor(active)) return;
      event.preventDefault();
      void actions.copyLink(active);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [enabled, actions]);
}
