// The list the system notifications and the badge follow: what the whole machine is waiting on, read from the engine's
// world feed and from nothing that belongs to one window.
//
// SUPERSEDED INBOX PRESENTATION (I2.1). Every window posts this list to Rust, and Rust treats an id missing from the newest
// list as answered. The superseded Inbox hid a failure the moment this window sends its "seen" mark (before the engine echoes
// it), listed only the five newest, and kept a count record for old engines that each window reads once. Built from
// those, two windows looking at the same feed reading would post different lists, and the one without a failure would
// make the other announce it again. So a failure is named here by the engine's own fact: `unseenFailed` and the landing
// instant of the newest failure. Only a row from an engine that predates that mark falls back to this window's record,
// which is all such an engine allows; two windows can then disagree about it, and Rust announces each failure once.
import { noticeTarget, type AttentionItem as NativeAttention } from '../../../design/nativeControls.ts';
import type { WorldRow } from '../../chat/world-client.ts';
import type { WorldState } from '../../chat/world-store.ts';
import { recentFailureMs } from './background.ts';
import { hasUnseenFailure, type FailedSeen } from './failedSeen.ts';

/** Rust takes at most 256 items in one list; questions and failures share it so neither can crowd the other out. */
export const noticeQuestionLimit = 192;
export const noticeFailureLimit = 64;

const clip = (text: string, max: number) => (text.length > max ? `${text.slice(0, max - 1)}…` : text).replace(/[\u0000-\u001f\u007f-\u009f]/g, ' ');

/**
 * A failure's notification id. It changes when a newer failure lands, so a second failure in the same conversation is
 * announced, and it never comes back once that failure has been seen.
 */
export function failureNoticeId(row: Pick<WorldRow, 'session' | 'failed' | 'failure'>): string {
  return `failed:${row.session}:${row.failure?.at ?? `#${row.failed}`}`;
}

/** Whether the engine (or, for an engine without the mark, this window's record) says the row has a failure unseen. */
function unseen(row: WorldRow, local: FailedSeen): boolean {
  if (row.unseenFailed !== undefined) return row.unseenFailed > 0;
  return row.failed > 0 && hasUnseenFailure(local, row.session, row.failed);
}

export function attentionSignals(world: Pick<WorldState, 'status' | 'rows' | 'items'>, local: FailedSeen, now: number): NativeAttention[] {
  if (world.status !== 'live') return [];
  const rows = world.rows.filter(row => !row.archived && !row.deletionPending);
  const titles = new Map(rows.map(row => [row.session, row.title] as const));
  const asked = world.items.slice(0, noticeQuestionLimit).map(item => ({
    id: item.key, kind: 'needsYou' as const, chatTitle: clip(item.title || titles.get(item.session) || 'Conversation', 120), text: clip(item.text, 200), ...noticeTarget(item.session, item),
  }));
  const at = (row: WorldRow) => (row.at ? Date.parse(row.at) : NaN);
  const failed = rows
    .filter(row => unseen(row, local) && now - at(row) <= recentFailureMs)
    .sort((a, b) => at(b) - at(a) || (a.session < b.session ? -1 : a.session > b.session ? 1 : 0))
    .slice(0, noticeFailureLimit)
    .map(row => ({ id: failureNoticeId(row), kind: 'failed' as const, chatTitle: clip(row.title, 120), text: 'A task failed', ...noticeTarget(row.session) }));
  return [...asked, ...failed];
}
