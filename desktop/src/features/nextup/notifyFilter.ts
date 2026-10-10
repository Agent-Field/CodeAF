import type { AttentionBlocking, AttentionItem as WorldAttention } from '../chat/world-client.ts';

/** Older feeds omit blocking because every question stopped the turn. */
export function shouldNotify(item: { kind: string; blocking?: boolean | AttentionBlocking; decidedBy?: string }): boolean {
  if (item.decidedBy) return false;
  if (item.kind === 'failed') return true;
  if (['done', 'running', 'decided', 'suggestion'].includes(item.kind)) return false;
  if (typeof item.blocking === 'boolean') return item.blocking;
  return item.blocking === undefined || item.blocking.turn || (item.blocking.tasks ?? []).some(task => task.trim() !== '');
}

/** The dock includes the current conversation and questions that do not block work. */
export function totalNeedsYou(items: readonly WorldAttention[], rows: readonly { session: string; needsYou: boolean; archived?: boolean }[]): number {
  const counts = new Map<string, Set<string>>();
  for (const item of items) {
    if (item.decidedBy || ['failed', 'done', 'running', 'decided'].includes(item.kind)) continue;
    const keys = counts.get(item.session) ?? new Set<string>();
    keys.add(item.key);
    counts.set(item.session, keys);
  }
  for (const row of rows) {
    if (row.archived) counts.delete(row.session);
    else if (row.needsYou && !counts.has(row.session)) counts.set(row.session, new Set([row.session]));
  }
  return [...counts.values()].reduce((total, keys) => total + keys.size, 0);
}

/** Failures use the engine's landing instant so every window names the same item. */
export function notificationAttention(items: readonly WorldAttention[], rows: readonly { session: string; title?: string; unseenFailed?: number; failed: number; failure?: { at: string }; archived?: boolean }[]) {
  return [
    ...items.filter(shouldNotify).map(item => ({ id: item.key, chatId: item.session, kind: 'question', title: item.title, head: item.text, blocking: true })),
    ...rows.filter(row => !row.archived && (row.unseenFailed ?? row.failed) > 0 && row.failure?.at).map(row => ({
      id: `failed:${row.session}:${row.failure!.at}`, chatId: row.session, kind: 'failed', title: row.title,
    })),
  ];
}
