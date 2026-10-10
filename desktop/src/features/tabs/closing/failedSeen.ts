// Which failures a window has already looked at, for the ONE engine that predates the engine's own mark. A world row's
// `failed` is a COUNT of landed-failed task rows and it never goes down; the canonical fact is the row's `unseenFailed`
// (the newest failure's landing instant is `failure`), which closing/signals.ts reads first. This count record is only
// the fallback for a row with no `unseenFailed`, and nothing writes it any more: with the Inbox gone there is no "Seen"
// gesture, so the record stays empty and such a row counts as unseen until its engine is updated.
export type FailedSeen = Readonly<Record<string, number>>;

export const hasUnseenFailure = (seen: FailedSeen, chatId: string, failed: number): boolean => failed > (seen[chatId] ?? 0);
