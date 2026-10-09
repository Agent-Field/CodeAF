// The tab kinds, as plain data. Kept free of React and CSS so the pure workspace
// model (and its node tests) can import it; the renderers live in ./registry.

/** Every kind of tab the shell knows (design 3j). A kind is data here; its icon and renderers are registered in ./registry. */
export const tabKinds = ['conversation', 'task', 'file', 'diff', 'web', 'terminal', 'settings', 'history', 'newtab', 'inbox', 'home'] as const;
export type TabKind = (typeof tabKinds)[number];

export const defaultKind: TabKind = 'conversation';

export const isTabKind = (value: unknown): value is TabKind => typeof value === 'string' && (tabKinds as readonly string[]).includes(value);

/** A stored or opened tab with no (or an unknown) kind is a conversation: every v1 tab was one. */
export const kindOrDefault = (value: unknown): TabKind => (isTabKind(value) ? value : defaultKind);
