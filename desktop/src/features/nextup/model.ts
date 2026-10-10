// The Next up queue for one window. Pure (no React, no storage) so a node test can pin
// the order, the skip, and which questions Accept N may answer. The world feed is the
// list of questions still waiting; this module only arranges the ones that are not in
// the conversation on screen.
//
// I2.1 walks blocking work first and anything that cannot be undone last. Inside a
// group, the question that was asked first comes first (P-22). Skip sends the front
// question to the back and leaves it waiting. I2.2: the frame pill never counts the
// conversation you are in, so that conversation's questions are not in this queue.

import { blockingOf, type AttentionItem } from '../chat/world-client.ts';

/**
 * The header chip's place in the walk. `index` is 1-based. The chip draws
 * nothing when this is absent, which is an empty queue.
 */
export type NextUpProgress = { index: number; total: number };

/** What the window knows when it asks for the queue. */
export type NextUpInput = {
  /** Questions still waiting, as the world feed listed them. */
  items: readonly AttentionItem[];
  /**
   * The conversation on screen, the attention item's `session`.
   * Empty leaves every conversation in the queue.
   */
  conversationKey?: string;
  /** Question keys already sent to the back, the earliest skip first. */
  skipped?: readonly string[];
};

export type NextUpQueue = {
  /** Walk order. The front is the question Next up opens. */
  items: readonly AttentionItem[];
  /** How many questions the frame pill counts. Zero draws no pill. */
  count: number;
  /** The chip's place while the front question is showing. Absent when `count` is 0. */
  progress?: NextUpProgress;
  /** "1 of 5", or "" when there is nothing to walk. The chip draws this and no zero. */
  progressText: string;
  /** Reversible questions in this queue that already have a suggestion. Accept N answers these. */
  acceptable: readonly AttentionItem[];
};

/** Holding up a turn or a named task. These come first, unless the question cannot be undone. */
const BAND_BLOCKING = 0;
/** Not holding work up, and still undoable. A suggestion that does not block sits here. */
const BAND_REST = 1;
/**
 * Cannot be undone. Last, even when the question is also blocking, so Next up
 * does not open a delete before the work that is only waiting.
 */
const BAND_IRREVERSIBLE = 2;

type Ranked = { item: AttentionItem; band: number; asked?: number; index: number };

/** "1 of 5". Empty when the pair is missing or could not be a place in the walk. */
export function nextUpProgressText(progress: NextUpProgress | undefined): string {
  if (!progress) return '';
  const { index, total } = progress;
  if (!Number.isInteger(index) || !Number.isInteger(total) || index < 1 || index > total) return '';
  return `${index} of ${total}`;
}

function blocksWork(item: AttentionItem): boolean {
  // An older feed omits `blocking`. blockingOf reads that as blocking the turn,
  // which keeps the question near the front instead of burying it.
  const blocking = blockingOf(item);
  if (blocking.turn) return true;
  const tasks = blocking.tasks;
  if (!Array.isArray(tasks)) return false;
  // A blank name does not name a task, so it does not pull the question forward.
  return tasks.some(task => typeof task === 'string' && task !== '');
}

function bandOf(item: AttentionItem): number {
  if (item.stakes === 'irreversible') return BAND_IRREVERSIBLE;
  return blocksWork(item) ? BAND_BLOCKING : BAND_REST;
}

/** Milliseconds since the epoch, or nothing when the feed did not give a time we can order. */
function askedOf(item: AttentionItem): number | undefined {
  if (typeof item.asked !== 'string' || item.asked === '') return undefined;
  const ms = Date.parse(item.asked);
  return Number.isFinite(ms) ? ms : undefined;
}

function hasSuggestion(item: AttentionItem): boolean {
  return typeof item.suggestion?.key === 'string' && item.suggestion.key !== '';
}

function compareRanked(a: Ranked, b: Ranked): number {
  if (a.band !== b.band) return a.band - b.band;
  if (a.asked !== undefined && b.asked !== undefined && a.asked !== b.asked) return a.asked - b.asked;
  // A known time has been waiting a known while. An unknown time follows it, and
  // two unknown times keep the order the feed listed (the same rule as a tie).
  if (a.asked !== undefined && b.asked === undefined) return -1;
  if (a.asked === undefined && b.asked !== undefined) return 1;
  return a.index - b.index;
}

/**
 * One question per key, and never the conversation on screen. A repeated key keeps
 * the first copy: the key is the question's identity, and answering it twice would
 * be a second decision.
 */
function eligible(items: readonly AttentionItem[], conversationKey: string): AttentionItem[] {
  const seen = new Set<string>();
  const out: AttentionItem[] = [];
  for (const item of items) {
    if (!item || typeof item.key !== 'string' || item.key === '') continue;
    if (seen.has(item.key)) continue;
    seen.add(item.key);
    if (conversationKey !== '' && item.session === conversationKey) continue;
    out.push(item);
  }
  return out;
}

/** Skipped keys leave the sorted front and sit at the back in the order they were skipped. */
function applySkips(ordered: readonly AttentionItem[], skipped: readonly string[] | undefined): AttentionItem[] {
  if (!skipped || skipped.length === 0) return ordered.slice();
  const byKey = new Map(ordered.map(item => [item.key, item]));
  const backKeys = new Set<string>();
  const back: AttentionItem[] = [];
  for (const key of skipped) {
    if (typeof key !== 'string' || backKeys.has(key)) continue;
    const item = byKey.get(key);
    if (!item) continue;
    backKeys.add(key);
    back.push(item);
  }
  if (back.length === 0) return ordered.slice();
  return ordered.filter(item => !backKeys.has(item.key)).concat(back);
}

/**
 * The queue the frame pill counts and Next up walks.
 * A question that is no longer in `items` is gone: answered elsewhere, it is not
 * waiting, so it is not skipped and it is not counted.
 */
export function nextUpQueue(input: NextUpInput): NextUpQueue {
  const conversationKey = typeof input.conversationKey === 'string' ? input.conversationKey : '';
  const waiting = eligible(Array.isArray(input.items) ? input.items : [], conversationKey);
  const ranked = waiting.map((item, index) => ({ item, band: bandOf(item), asked: askedOf(item), index }));
  ranked.sort(compareRanked);
  const items = applySkips(ranked.map(row => row.item), input.skipped);
  const count = items.length;
  const progress = count === 0 ? undefined : { index: 1, total: count };
  return {
    items,
    count,
    progress,
    progressText: nextUpProgressText(progress),
    acceptable: items.filter(item => item.stakes === 'reversible' && hasSuggestion(item)),
  };
}
