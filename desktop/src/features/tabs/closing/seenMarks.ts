// The closing seam for "this failure was looked at". It sends the engine's durable mark (chat/world-client.ts
// `markFailureSeen`) and keeps the Inbox honest about the gap while the request is in flight:
//   - the row leaves at once (a pending mark, in memory, keyed to the failure's landing instant);
//   - if the engine refuses or cannot be reached, the pending mark is withdrawn and the failure comes back with one
//     sentence saying why, instead of looking seen on this window and unseen everywhere else;
//   - an engine that predates the mark, or a row that carries no failure identity, falls back to the window-local record.
import { markFailureSeen, WorldError, type FailureId, type WorldTransport } from '../../chat/world-client.ts';
import type { PendingSeen } from './failedSeen.ts';

export type SeenMarks = {
  getPending: () => PendingSeen;
  subscribe: (listener: () => void) => () => void;
  /** Marks one conversation's failure. Resolves when the engine has answered; never rejects. */
  mark: (chatId: string, failure: FailureId) => Promise<void>;
};

export function createSeenMarks(options: { transport: WorldTransport; onRefused: (message: string) => void }): SeenMarks {
  let pending: PendingSeen = {};
  const listeners = new Set<() => void>();
  const set = (next: PendingSeen) => { pending = next; listeners.forEach(listener => listener()); };
  return {
    getPending: () => pending,
    subscribe(listener) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    async mark(chatId, failure) {
      if (pending[chatId] === failure.at) return;
      set({ ...pending, [chatId]: failure.at });
      try {
        await markFailureSeen(options.transport, { session: chatId, at: failure.at, task: failure.task });
      } catch (error) {
        const { [chatId]: _withdrawn, ...rest } = pending;
        set(rest);
        const gone = error instanceof WorldError && error.status === 409;
        options.onRefused(gone ? 'That failure is no longer on record.' : error instanceof WorldError && error.unreachable ? 'Could not reach the engine to mark that failure as seen.' : 'Could not mark that failure as seen.');
      }
    },
  };
}
