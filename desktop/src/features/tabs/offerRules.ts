// Pure rules for the group suggestion pill (Shell 2b, "a group suggestion"): which open tabs belong together, and how long a
// decision about them is remembered. No React, no CSS, no model call: every offer is read off facts the tabs already carry.
import { newConversationTitle } from './helpers.ts';
import type { Tab } from './types.ts';

/** Design: "only for 3 or more tabs on one repo or topic". */
export const offerMinimum = 3;
/** Design (Interactions, place suggestion): "Not now hides it for 30 days". A group suggestion borrows the same lifetime. */
export const offerMemoryDays = 30;
export const offerStorageKey = 'codeaf.desktop.groupOffers.v1';
const offerStorageLimit = 200;
const day = 86_400_000;

/**
 * One suggestion. `key` names the SET (its source), not its members, so the same source is offered once however its tabs
 * come and go. `title` is a real tab title or a real folder name; it is never composed, so an absent one stays absent.
 */
export type GroupOffer = { key: string; basis: 'conversation' | 'folder' | 'repo' | 'topic'; ids: string[]; title?: string };

/** How long a settled set of chat ids waits before one organizing question. A selection change is not a new set. */
export const offerSettleMs = 400;

/** A canonical chat id is the session id the engine minted: 16 lower-case hex digits, never a path. */
const canonicalChatID = /^[0-9a-f]{16}$/;

export type ChatSummary = { sessionId?: string };

/** The chat id a loose conversation tab is showing, from the engine summary, else the tab's own target. A path is not an id. */
export function chatIDOf(tab: Tab, summary?: ChatSummary): string | undefined {
  if (tab.kind !== 'conversation' || !candidate(tab)) return undefined;
  const id = summary?.sessionId || tab.target?.sessionId;
  return id && canonicalChatID.test(id) ? id : undefined;
}

/** Distinct canonical chat ids of the loose conversation tabs, in strip order. */
export function looseChatIDs(tabs: readonly Tab[], summaries: Readonly<Record<string, ChatSummary | undefined>> = {}): string[] {
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const tab of tabs) {
    const id = chatIDOf(tab, summaries[tab.id]);
    if (id && !seen.has(id)) { seen.add(id); ids.push(id); }
  }
  return ids;
}

/** The set's own key. Order does not matter, so a strip reorder is not a new question. */
export function canonicalSetKey(ids: readonly string[]): string {
  return `canonical:${[...ids].sort().join('|')}`;
}

/** One organizing question per new set of at least three chats, and none for a set already asked this launch. */
export function shouldAskCanonical(ids: readonly string[], asked: ReadonlySet<string>): boolean {
  return ids.length >= offerMinimum && !asked.has(canonicalSetKey(ids));
}

/**
 * Maps an engine offer's chat ids onto the loose conversation tabs that show them.
 * A chat the strip is not showing, or a tab that is pinned, grouped, split or not a conversation, is left out.
 */
export function tabOffersForChats(tabs: readonly Tab[], summaries: Readonly<Record<string, ChatSummary | undefined>>, engine: readonly GroupOffer[]): GroupOffer[] {
  const tabOf = new Map<string, string>();
  for (const tab of tabs) {
    const id = chatIDOf(tab, summaries[tab.id]);
    if (id && !tabOf.has(id)) tabOf.set(id, tab.id);
  }
  const offers: GroupOffer[] = [];
  for (const offer of engine) {
    if (offer.basis !== 'repo' && offer.basis !== 'topic') continue;
    const ids = offer.ids.flatMap(id => { const tab = tabOf.get(id); return tab ? [tab] : []; });
    if (ids.length < offerMinimum) continue;
    offers.push({ key: offer.key, basis: offer.basis, ids, title: offer.title });
  }
  return offers;
}

/** Local offers keep the tabs they claimed. An engine offer that then has fewer than three tabs is dropped. */
export function mergeOffers(local: readonly GroupOffer[], extra: readonly GroupOffer[]): GroupOffer[] {
  const claimed = new Set(local.flatMap(offer => offer.ids));
  const offers = [...local];
  for (const offer of extra) {
    const ids = offer.ids.filter(id => !claimed.has(id));
    if (ids.length < offerMinimum) continue;
    ids.forEach(id => claimed.add(id));
    offers.push({ ...offer, ids });
  }
  return offers;
}

/** The kinds whose tabs say where they came from: a saved session, or a path inside the workspace. */
const sourced = new Set(['conversation', 'task', 'file', 'diff']);

/** Loose, ordinary tabs only: a pinned tab, a split, the Inbox and anything already in a group are never suggested. */
const candidate = (tab: Tab) => !tab.pinned && !tab.groupId && !tab.split && sourced.has(tab.kind);

/** A file or diff tab's workspace-relative path, from either place the model keeps it. */
const pathOf = (tab: Tab) => tab.file?.path ?? tab.path ?? tab.target?.path;

/** The first folder of a nested workspace-relative path; a file at the root, or an absolute or climbing path, names no folder. */
export function folderOf(path: string | undefined): string | undefined {
  if (!path || path.startsWith('/') || path.includes('\\')) return undefined;
  const parts = path.split('/');
  if (parts.length < 2 || parts.some(part => !part || part === '..' || part === '.')) return undefined;
  return parts[0];
}

/** The conversation's own name, unless it is still the placeholder a new conversation carries. */
const namedConversation = (tabs: readonly Tab[]) => tabs.find(tab => tab.kind === 'conversation' && tab.title && tab.title !== newConversationTitle && tab.titleSource)?.title;

/**
 * Every set of three or more loose tabs that share a source, in strip order. Two sources, both facts rather than guesses:
 * tabs reading the same saved session (the conversation and the tasks, files and diffs it opened), then file and diff tabs
 * in the same top folder of the workspace. A tab belongs to the first set that claims it. There is no title-word "topic"
 * here: the canonical topic grouping runs in the engine's place organizer and the desktop has no tab-level reading of it.
 */
export function findOffers(tabs: readonly Tab[]): GroupOffer[] {
  const loose = tabs.filter(candidate);
  const offers: GroupOffer[] = [];
  const claimed = new Set<string>();
  const collect = (basis: GroupOffer['basis'], keyOf: (tab: Tab) => string | undefined, label: (key: string, members: Tab[]) => string | undefined) => {
    const sets = new Map<string, Tab[]>();
    for (const tab of loose) {
      const key = claimed.has(tab.id) ? undefined : keyOf(tab);
      if (key) sets.set(key, [...(sets.get(key) ?? []), tab]);
    }
    for (const [key, members] of sets) {
      if (members.length < offerMinimum) continue;
      members.forEach(member => claimed.add(member.id));
      offers.push({ key: `${basis}:${key}`, basis, ids: members.map(member => member.id), title: label(key, members) });
    }
  };
  collect('conversation', tab => tab.sessionFile, (_, members) => namedConversation(members));
  collect('folder', tab => folderOf(pathOf(tab)), key => key);
  return offers;
}

/** What has been decided, by set key and the time it was decided. Anything unreadable is treated as nothing decided. */
export type OfferMemory = Record<string, number>;

/** Reads the remembered decisions, dropping anything malformed or older than the memory. */
export function readMemory(storage: Pick<Storage, 'getItem'> | undefined, now: number): OfferMemory {
  try {
    const parsed = JSON.parse(storage?.getItem(offerStorageKey) ?? 'null') as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    return Object.fromEntries(Object.entries(parsed).filter((entry): entry is [string, number] => typeof entry[1] === 'number' && Number.isFinite(entry[1]) && entry[1] <= now && now - entry[1] < offerMemoryDays * day));
  } catch { return {}; }
}

/** Remembers a decision (Group or dismiss) for the memory's length, keeping the newest entries if the store grows. */
export function remember(memory: OfferMemory, key: string, now: number): OfferMemory {
  const next = { ...memory, [key]: now };
  return Object.fromEntries(Object.entries(next).sort((a, b) => b[1] - a[1]).slice(0, offerStorageLimit));
}

export function writeMemory(storage: Pick<Storage, 'setItem'> | undefined, memory: OfferMemory): void {
  try { storage?.setItem(offerStorageKey, JSON.stringify(memory)); } catch { /* A full or unavailable store only means the question may come back next launch. */ }
}

/**
 * The offer to show now: the first whose set is not decided (Group or dismissed within the memory) and has not already
 * been shown in this launch. `shown` is the launch's own record, so a set that appeared and went away is not offered again.
 */
export function nextOffer(offers: readonly GroupOffer[], memory: OfferMemory, shown: ReadonlySet<string>): GroupOffer | undefined {
  return offers.find(offer => !(offer.key in memory) && !shown.has(offer.key));
}
