import { nextUpQueue, type NextUpProgress } from './model.ts';
import type { AttentionItem } from '../chat/world-client.ts';
import type { NoticeTarget } from '../../design/nativeControls.ts';

/** A notification starts the existing queue at its own question, even in the current conversation. */
export function createNotificationWalk() {
  let keys: string[] = [];
  let index = 0;
  let current: AttentionItem | undefined;
  let progress: NextUpProgress | undefined;
  const listeners = new Set<() => void>();
  const publish = () => listeners.forEach(listener => listener());
  return {
    start(target: NoticeTarget, items: readonly AttentionItem[]) {
      const item = items.find(item => target.itemId ? item.key === target.itemId : item.session === target.chatId && item.kind === target.question?.kind && item.id === target.question.id);
      if (!item || item.decidedBy) { keys = []; current = undefined; progress = undefined; publish(); return; }
      keys = [item.key, ...nextUpQueue({ items: items.filter(item => !item.decidedBy) }).items.filter(row => row.key !== item.key).map(row => row.key)];
      index = 0; current = item; progress = { index: 1, total: keys.length }; publish();
    },
    update(items: readonly AttentionItem[]) {
      if (!current || items.some(item => item.key === current!.key && !item.decidedBy)) return;
      const pending = new Map(items.filter(item => !item.decidedBy).map(item => [item.key, item]));
      do { index++; } while (index < keys.length && !pending.has(keys[index]));
      current = pending.get(keys[index]);
      progress = current ? { index: index + 1, total: keys.length } : undefined;
      publish();
    },
    current: () => current,
    progress: () => progress,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
}

export const notificationWalk = createNotificationWalk();
