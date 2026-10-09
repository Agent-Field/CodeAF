// A system notification was clicked: its conversation should open with that question in front of the tray. The tab is
// opened by the link path; the question travels on this tiny channel, because the conversation it names may not be on
// screen yet, and its questions arrive from the engine after the view attaches. A view takes the request once it holds
// that exact question, so a request never lands on a different conversation or a different question.
import type { NoticeQuestion } from '../../design/nativeControls.ts';

/** A request no view took in this long is spent: a late attach must not pull the tray around minutes later. */
export const QUESTION_FOCUS_TTL_MS = 60_000;
/**
 * At most this many conversations wait for a view at once. Clicks on notifications for conversations that never mount
 * would otherwise each leave a request behind; the oldest goes first. It matches the Rust side's route bound.
 */
export const QUESTION_FOCUS_MAX = 64;

type Asked = { kind: string; id: number };

export function createQuestionFocus(clock: () => number = Date.now) {
  const pending = new Map<string, { question: NoticeQuestion; at: number }>();
  const listeners = new Set<() => void>();
  // Every request and every take sweeps out the expired ones, so a request for a conversation nobody ever queries
  // goes too, not only the one being asked about.
  const prune = (now: number) => {
    for (const [chatId, wanted] of pending) if (now - wanted.at >= QUESTION_FOCUS_TTL_MS) pending.delete(chatId);
  };
  return {
    /** Asks the conversation `chatId` to bring `question` forward once it shows it. A newer click replaces an older one. */
    request(chatId: string, question: NoticeQuestion) {
      const now = clock();
      prune(now);
      // Re-inserted, so a repeated click counts as the newest; a Map iterates in insertion order.
      pending.delete(chatId);
      pending.set(chatId, { question, at: now });
      for (const oldest of pending.keys()) {
        if (pending.size <= QUESTION_FOCUS_MAX) break;
        pending.delete(oldest);
      }
      listeners.forEach(listener => listener());
    },
    /** The question among `questions` that was asked for, handed over once; nothing for any other conversation. */
    take<Q extends Asked>(chatId: string, questions: readonly Q[]): Q | undefined {
      prune(clock());
      const wanted = pending.get(chatId);
      if (!wanted) return undefined;
      const match = questions.find(q => q.kind === wanted.question.kind && q.id === wanted.question.id);
      if (match) pending.delete(chatId);
      return match;
    },
    /** How many requests are waiting; for tests. */
    size: () => pending.size,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
}

export const questionFocus = createQuestionFocus();
