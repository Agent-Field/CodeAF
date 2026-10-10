// Opening an address as a web tab from outside the strip (a link chip, a page's own link). The caller does not hold
// the strip, so the open rides the desktop tab-action event, exactly as a job open does (jobs/open.ts). The address
// is validated here, once, so a refused scheme never reaches the strip and the caller keeps its own fallback.
import { desktopTabEvent } from '../../lib/desktopMenuRoute.ts';
import { newTab, panesOf } from '../tabs/helpers.ts';
import type { Tab } from '../tabs/types.ts';
import { displayParts, parseAddress } from './model.ts';

/** `background` is the ⌘-click / middle-click gesture: the tab does not take focus. `afterId` is the tab it follows. */
export type WebOpenOptions = { background?: boolean; afterId?: string };

/** What the strip receives. `tab` is the web tab to add. */
export type OpenWebDetail = { type: 'open-web'; background: boolean; afterId?: string; tab: Tab };

/** What the strip should do with one request. `at` is the strip index right after `afterId`, absent for the end. */
export type WebTabAction = { type: 'open'; tab: Tab; background: boolean; at?: number };

/** True when `value` is a web-open detail this module built. A menu command is a string and fails here. */
export function isOpenWebDetail(value: unknown): value is OpenWebDetail {
  if (!value || typeof value !== 'object') return false;
  const detail = value as Partial<OpenWebDetail>;
  const tab = detail.tab;
  if (detail.type !== 'open-web' || typeof detail.background !== 'boolean') return false;
  if (detail.afterId !== undefined && typeof detail.afterId !== 'string') return false;
  return !!tab && tab.kind === 'web' && typeof tab.id === 'string' && typeof tab.title === 'string' && typeof tab.web?.url === 'string';
}

/** The detail for one open, or null when the text is not an http(s) address. The host is the title until the page names itself. */
export function openWebDetail(url: string, options: WebOpenOptions = {}): OpenWebDetail | null {
  const address = parseAddress(url);
  if (!address) return null;
  const tab = newTab({ kind: 'web', title: displayParts(address).host, titleSource: 'message', web: { url: address.href }, target: { url: address.href } });
  return { type: 'open-web', background: options.background === true, afterId: options.afterId, tab };
}

/** Places the tab right after `afterId` (or after the split that holds that pane); an unknown id means the end. */
export function webOpenAction(tabs: readonly Tab[], detail: OpenWebDetail): WebTabAction {
  const index = detail.afterId === undefined ? -1 : tabs.findIndex(tab => tab.id === detail.afterId || panesOf(tab).some(pane => pane.id === detail.afterId));
  return { type: 'open', tab: detail.tab, background: detail.background, ...(index >= 0 ? { at: index + 1 } : {}) };
}

/**
 * Opens `url` as a web tab by dispatching on the desktop tab-action event. Returns the tab that will be added, or
 * null when the scheme is refused, in which case nothing is dispatched and the caller keeps its fallback.
 */
export function openWebTab(url: string, options: WebOpenOptions = {}): Tab | null {
  const detail = openWebDetail(url, options);
  if (!detail) return null;
  globalThis.window?.dispatchEvent(new CustomEvent(desktopTabEvent, { detail }));
  return detail.tab;
}
