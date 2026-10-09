// Which failures this window has already looked at. A world row's `failed` is a COUNT of landed-failed task rows and it
// never goes down, so without a record of what was seen every old failure would sit in the Inbox forever.
//
// THIS IS WINDOW-LOCAL VIEW STATE, not a canonical fact. The engine has no durable "failure seen" mark (nothing in the
// world feed or the session index carries one), so another window or a reinstall starts unseen. That gap is documented in
// run/menus-integration.md; do not present this as shared state.
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

export const hasUnseenFailure = (seen: FailedSeen, chatId: string, failed: number): boolean => failed > (seen[chatId] ?? 0);

export function saveFailedSeen(seen: FailedSeen, storage: Pick<Storage, 'setItem'> | undefined = globalThis.localStorage): void {
  try { storage?.setItem(failedSeenKey, JSON.stringify(seen)); } catch { /* A full store only means the failure shows again. */ }
}
