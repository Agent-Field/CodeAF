import type { WorldRow } from '../../chat/world-client.ts';

// Which failures this window has already looked at. A world row's `failed` is a COUNT of landed-failed task rows and it
// never goes down, so without a record of what was seen every old failure would sit in the Inbox forever.
//
// THE CANONICAL FACT LIVES IN THE ENGINE. A world row carries `unseenFailed` and `failure` (the newest failure's task and
// landing instant); `markFailureSeen` (chat/world-client.ts) writes a watermark into the conversation's own metadata, so every
// window and device agrees and a later failure is unseen again. This file keeps two small things around that:
//   - `pending`: a mark this window has sent and the feed has not yet echoed, so the Inbox row leaves at once. It is
//     in memory, keyed to the failure's landing instant, and it is dropped if the engine refuses the mark.
//   - the count record below, used ONLY for a row from an engine that predates the mark (no `unseenFailed`). It is
//     window-local view state, not shared; it is the old behaviour kept for that case alone.
export const failedSeenKey = 'codeaf.desktop.inbox.failed-seen.v1';
export type FailedSeen = Readonly<Record<string, number>>;

const limit = 200;

/** A saved record that does not validate yields an empty one: worst case a failure is shown once more. */
export function readFailedSeen(storage: Pick<Storage, 'getItem'> | undefined = globalThis.localStorage): FailedSeen {
  try {
    const value = JSON.parse(storage?.getItem(failedSeenKey) ?? 'null') as unknown;
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    const entries = Object.entries(value as Record<string, unknown>).filter((entry): entry is [string, number] => !!entry[0] && Number.isSafeInteger(entry[1]) && (entry[1] as number) > 0);
    return Object.fromEntries(entries.slice(-limit));
  } catch { return {}; }
}

/** Records that `count` failures of this chat were seen. The count only goes up; a new failure raises `failed` past it. */
export function markFailedSeen(seen: FailedSeen, chatId: string, count: number): FailedSeen {
  if (!chatId || !Number.isSafeInteger(count) || count <= (seen[chatId] ?? 0)) return seen;
  const { [chatId]: _old, ...rest } = seen;
  return Object.fromEntries([...Object.entries(rest), [chatId, count]].slice(-limit));
}

/** Marks this window has sent and the feed has not echoed yet: chat id → the `failure.at` it covers. */
export type PendingSeen = Readonly<Record<string, string>>;

/**
 * Whether a world row still has a failure to look at. The engine's own count decides when it gives one; a pending mark for
 * the row's CURRENT failure hides it early, and never hides a newer one (a different landing instant).
 */
export function rowHasUnseenFailure(row: Pick<WorldRow, 'session' | 'failed' | 'unseenFailed' | 'failure'>, seen: FailedSeen, pending: PendingSeen = {}): boolean {
  if (row.unseenFailed === undefined) return row.failed > 0 && hasUnseenFailure(seen, row.session, row.failed);
  return row.unseenFailed > 0 && !(row.failure && pending[row.session] === row.failure.at);
}

export const hasUnseenFailure = (seen: FailedSeen, chatId: string, failed: number): boolean => failed > (seen[chatId] ?? 0);

export function saveFailedSeen(seen: FailedSeen, storage: Pick<Storage, 'setItem'> | undefined = globalThis.localStorage): void {
  try { storage?.setItem(failedSeenKey, JSON.stringify(seen)); } catch { /* A full store only means the failure shows again. */ }
}
