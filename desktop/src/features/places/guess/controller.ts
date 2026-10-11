import { GUESS_BUDGET_MS, GUESS_DEBOUNCE_MS, readGuess, type Guess } from './model.ts';

export type DraftAsk = { draft: string; workspace: string; chatId: string };
type Clock = { now: () => number; later: (fn: () => void, ms: number) => unknown; cancel: (id: unknown) => void };
const clock: Clock = { now: () => performance.now(), later: (fn, ms) => setTimeout(fn, ms), cancel: id => clearTimeout(id as ReturnType<typeof setTimeout>) };

/** Every edit hides the old answer immediately. A deadline and a generation guard also cover transports that ignore abort. */
export function createGuessController(request: (ask: DraftAsk, signal: AbortSignal) => Promise<unknown>, publish: (guess: Guess | undefined) => void, time: Clock = clock) {
  let generation = 0;
  let debounce: unknown;
  let deadline: unknown;
  let abort: AbortController | undefined;
  const clear = () => { generation++; time.cancel(debounce); time.cancel(deadline); abort?.abort(); };
  return {
    update(ask: DraftAsk) {
      clear();
      publish(undefined);
      if (!ask.draft.trim()) return;
      const current = generation;
      debounce = time.later(() => {
        abort = new AbortController();
        const signal = abort.signal;
        const started = time.now();
        deadline = time.later(() => { abort?.abort(); if (current === generation) publish(undefined); }, GUESS_BUDGET_MS);
        void request(ask, signal).then(value => {
          if (current !== generation || signal.aborted) return;
          time.cancel(deadline);
          publish(time.now() - started <= GUESS_BUDGET_MS ? readGuess(value) : undefined);
        }).catch(() => { if (current === generation) { time.cancel(deadline); publish(undefined); } });
      }, GUESS_DEBOUNCE_MS);
    },
    dispose() { clear(); },
  };
}
