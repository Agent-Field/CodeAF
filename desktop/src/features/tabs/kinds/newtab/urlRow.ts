// The URL row of the new-tab field (Shell 3f/3k, "Pasting a URL opens a web tab"): pure, so a node test covers it.
// It exists only while the web kind is backed. An unbacked row would be a control that cannot work, so without the
// kind the address stays a question and the first row is the conversation row (NT1).
import { addressParts, looksLikeAddress, toAddress } from '../../../web/address.ts';
import type { NewTabRow } from './rows.ts';

const addressLimit = 40;
const clip = (text: string, limit: number) => (text.length <= limit ? text : `${text.slice(0, limit - 1).trimEnd()}…`);

/** What the row says for an address: the site and path as the web tab will show them. */
export function webLabel(url: string): string {
  const { site, rest } = addressParts(url);
  return `Open ${clip(`${site}${rest}`, addressLimit)} in a web tab`;
}

/** The web row for the typed text, or none when the text is not an http(s) address or the web kind is not backed. */
export function webRows(query: string, backed: boolean): NewTabRow[] {
  if (!backed || !query || !looksLikeAddress(query)) return [];
  const address = toAddress(query);
  return 'url' in address ? [{ id: 'web', kind: 'web', icon: 'web', label: webLabel(address.url), hint: '↵', target: address.url }] : [];
}
