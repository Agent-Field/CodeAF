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
export type GroupOffer = { key: string; basis: 'conversation' | 'folder'; ids: string[]; title?: string };

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
