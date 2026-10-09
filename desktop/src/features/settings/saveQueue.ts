/** Saves for one choice run in order; unrelated choices can finish independently. */
export function createSaveQueue() {
  const tails = new Map<string, { signature: string; result: Promise<unknown> }>();
  return {
    enqueue<T>(key: string, signature: string, run: () => Promise<T>): Promise<T> {
      const before = tails.get(key);
      if (before?.signature === signature) return before.result as Promise<T>;
      const result = before ? before.result.then(run, run) : Promise.resolve().then(run);
      const entry = { signature, result };
      tails.set(key, entry);
      const remove = () => { if (tails.get(key) === entry) tails.delete(key); };
      void result.then(remove, remove);
      return result;
    },
  };
}
