// What a person typed into an address field, turned into a web address or a
// refusal. Pure, so the new-tab field (integrator) and the web pane share it.
// The native side checks again (src-tauri/src/web/policy.rs); this copy exists
// so a refusal is said in the field, before anything is asked of the view.

export type Address = { url: string } | { refusal: string };

export const ADDRESS_MAX = 4096;
const SCHEME = /^([a-z][a-z0-9+.-]*):/i;
const HOSTLIKE = /^(localhost|\[[0-9a-f:]+\]|[0-9]{1,3}(\.[0-9]{1,3}){3}|([a-z0-9-]+\.)+[a-z][a-z0-9-]*)(:[0-9]{1,5})?([/?#].*)?$/i;
const LOOPBACK = /^(localhost|127\.0\.0\.1|\[::1\])(:|\/|$)/i;

/** The same sentences the native side uses, so one refusal reads one way. */
export const refusalSentence = {
  tooLong: 'That address is too long',
  malformed: 'That is not a web address',
  scheme: 'Only http and https pages open in codeaf',
  noHost: 'That address has no site',
  credentials: 'Addresses with a name and password are not opened',
  appOrigin: "codeaf's own pages cannot open in a web tab",
} as const;

/**
 * A full http(s) URL is taken as written; a bare host ("pkg.go.dev/x") gets
 * https, or http on this machine. Anything else is not an address, and the
 * caller decides what it is instead (a question, a file).
 */
export function toAddress(input: string): Address {
  const text = input.trim();
  if (!text) return { refusal: refusalSentence.malformed };
  if (text.length > ADDRESS_MAX) return { refusal: refusalSentence.tooLong };
  if (/\s/.test(text)) return { refusal: refusalSentence.malformed };
  const scheme = SCHEME.exec(text)?.[1]?.toLowerCase();
  const isPort = scheme !== undefined && /^[a-z0-9.-]+:[0-9]/i.test(text) && !text.includes('://');
  let candidate = text;
  if (scheme && !isPort) {
    if (scheme !== 'http' && scheme !== 'https') return { refusal: refusalSentence.scheme };
  } else {
    if (!HOSTLIKE.test(text)) return { refusal: refusalSentence.malformed };
    candidate = `${LOOPBACK.test(text) ? 'http' : 'https'}://${text}`;
  }
  let url: URL;
  try {
    url = new URL(candidate);
  } catch {
    return { refusal: refusalSentence.malformed };
  }
  if (!url.hostname) return { refusal: refusalSentence.noHost };
  if (url.username || url.password) return { refusal: refusalSentence.credentials };
  return { url: url.href };
}

/** True when the words read as an address rather than a question: for the new-tab field's URL row. */
export const looksLikeAddress = (input: string): boolean => 'url' in toAddress(input) && !/\s/.test(input.trim());

/** The address field's two tones: the site in ink, everything after it dimmed. */
export function addressParts(href: string): { site: string; rest: string } {
  try {
    const url = new URL(href);
    const site = url.host.replace(/^www\./, '');
    const rest = `${url.pathname === '/' ? '' : url.pathname}${url.search}${url.hash}`;
    return { site, rest };
  } catch {
    return { site: href, rest: '' };
  }
}

/** A title for a page that has not said its own: its site. */
export const siteOf = (href: string): string => addressParts(href).site;

/** The mark source of a tab: a web pane's site, from its address. Other kinds have none. */
export const monogramOf = (pane: { kind: string; target?: { url?: string } }): string | undefined => (pane.kind === 'web' && pane.target?.url ? siteOf(pane.target.url) : undefined);
