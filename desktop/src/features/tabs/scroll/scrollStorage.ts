// Which storage entry holds THIS window's scroll memory. Pure (storage is passed in) so a node test can pin it.
// Tabs, groups and drafts are shared between windows on one place; focus and scroll are not (PLACES-ARCHITECTURE 3.4, section 8), so the memory
// is namespaced by window. A native window is named by its label, which survives a relaunch ("main", "w-2"). A browser has no such name, so a
// token kept in sessionStorage stands in: it survives a reload of that tab and is never shared with another tab or window.

export const scrollStorageBase = 'codeaf.desktop.tabScroll.v2';
const tokenKey = 'codeaf.desktop.windowToken';
const indexKey = `${scrollStorageBase}.index`;
/** Namespaces kept in localStorage. A browser tab that was closed leaves its token behind; the oldest are dropped past this. */
export const MAX_NAMESPACES = 8;

type Reader = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

export const nativeNamespace = (label: string) => `native.${label.replace(/[^\w.-]/g, '_').slice(0, 64)}`;

/** The browser tab's own token, minted once per tab. Without sessionStorage a fresh one per load is the safe answer: nothing is shared, nothing restores. */
export function browserNamespace(session: Reader | undefined, mint: () => string = () => Math.random().toString(36).slice(2, 12)): string {
  let token: string | null = null;
  try { token = session?.getItem(tokenKey) ?? null; } catch { /* Storage can be blocked. */ }
  if (!token || !/^[a-z0-9]{4,32}$/.test(token)) {
    token = mint();
    try { session?.setItem(tokenKey, token); } catch { /* A token that cannot be kept just does not survive a reload. */ }
  }
  return `browser.${token}`;
}

export const storageKeyFor = (namespace: string) => `${scrollStorageBase}.${namespace}`;

/** Records that `namespace` was used now and removes the entries of the namespaces beyond the newest MAX_NAMESPACES. */
export function touchNamespace(store: Reader, namespace: string, now: number = Date.now()): void {
  let index: Record<string, number> = {};
  try {
    const raw = JSON.parse(store.getItem(indexKey) ?? '{}');
    if (raw && typeof raw === 'object' && !Array.isArray(raw)) for (const [name, at] of Object.entries(raw)) if (typeof at === 'number' && Number.isFinite(at)) index[name] = at;
  } catch { index = {}; }
  index[namespace] = now;
  const newest = Object.entries(index).sort((a, b) => b[1] - a[1]);
  for (const [name] of newest.slice(MAX_NAMESPACES)) { delete index[name]; try { store.removeItem(storageKeyFor(name)); } catch { /* Best effort. */ } }
  try { store.setItem(indexKey, JSON.stringify(index)); } catch { /* A full store only costs the cleanup. */ }
}
