// A system notification was clicked: its conversation should open with that question in front of the tray. The tab is
// opened by the link path; the question travels on this tiny channel, because the conversation it names may not be on
// screen yet, and its questions arrive from the engine after the view attaches. A view takes the request once it holds
// that exact question, so a request never lands on a different conversation or a different question.
import type { NoticeQuestion } from '../../design/nativeControls.ts';

/** A request no view took in this long is spent: a late attach must not pull the tray around minutes later. */
export const QUESTION_FOCUS_TTL_MS = 60_000;

type Asked = { kind: string; id: number };

export function createQuestionFocus(clock: () => number = Date.now) {
  const pending = new Map<string, { question: NoticeQuestion; at: number }>();
  const listeners = new Set<() => void>();
  const live = (chatId: string) => {
    const wanted = pending.get(chatId);
    if (wanted && clock() - wanted.at >= QUESTION_FOCUS_TTL_MS) pending.delete(chatId);
    return pending.get(chatId);
  };
  return {
    /** Asks the conversation `chatId` to bring `question` forward once it shows it. A newer click replaces an older one. */
    request(chatId: string, question: NoticeQuestion) {
      pending.set(chatId, { question, at: clock() });
      listeners.forEach(listener => listener());
    },
    /** The question among `questions` that was asked for, handed over once; nothing for any other conversation. */
    take<Q extends Asked>(chatId: string, questions: readonly Q[]): Q | undefined {
      const wanted = live(chatId);
      if (!wanted) return undefined;
      const match = questions.find(q => q.kind === wanted.question.kind && q.id === wanted.question.id);
      if (match) pending.delete(chatId);
      return match;
    },
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
}

export const questionFocus = createQuestionFocus();
