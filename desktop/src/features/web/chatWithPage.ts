// Chat-plus (Shell 3d, 3k): a new conversation immediately after the web tab,
// focused, with the page's address and title attached and nothing sent. The
// page body is never read. The picture, when the desktop app already captured
// one, stays the file hand-off pageContext already builds.

import { panesOf } from '../tabs/helpers.ts';
import type { Tab } from '../tabs/types.ts';
import { siteOf } from './address.ts';
import { pageAttachment, pageDraft, type PageContext } from './pageContext.ts';

/** The page as a link: its address and title, and nothing of its body. */
export type PageLink = { url: string; title: string };

export type ChatWithPage = {
  action: { type: 'open'; tab: Tab; background: false; at: number };
  files: File[];
  link: PageLink;
};

/** Title trimmed, so a page that has not named itself contributes no title words. */
export function pageLink(page: Pick<PageContext, 'url' | 'title'>): PageLink {
  return { url: page.url, title: page.title.trim() };
}

/**
 * The open that chat-plus dispatches. `at` is the strip index immediately after
 * the web tab (or the split that holds that pane). An unknown pane appends.
 * A grouped web tab shares its group, so the conversation stays inside that run
 * rather than being pushed past it. The draft is the unsent line the page
 * hand-off already writes; the link is the same address and title.
 */
export function chatWithPage(tabs: readonly Tab[], webPaneId: string, page: PageContext, id = crypto.randomUUID()): ChatWithPage {
  const link = pageLink(page);
  const { files } = pageAttachment(page);
  const index = tabs.findIndex(tab => tab.id === webPaneId || panesOf(tab).some(pane => pane.id === webPaneId));
  const holder = index >= 0 ? tabs[index] : undefined;
  const tab: Tab = {
    id,
    kind: 'conversation',
    title: link.title || siteOf(page.url),
    titleSource: 'message',
    pinned: false,
    draft: pageDraft(page),
    ...(holder?.groupId && !holder.pinned ? { groupId: holder.groupId } : {}),
  };
  return {
    action: { type: 'open', tab, background: false, at: index >= 0 ? index + 1 : tabs.length },
    files,
    link,
  };
}
