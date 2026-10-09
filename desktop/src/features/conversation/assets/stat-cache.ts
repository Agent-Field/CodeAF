import type { EnginePathFact } from '../../chat/engine-client';

export const statBatchLimit = 64;

type Fetcher = (paths: string[]) => Promise<EnginePathFact[]>;
type Waiter = { resolve: (fact: EnginePathFact) => void; reject: (error: unknown) => void };

/** Batches path lookups made in the same tick into calls of at most 64, and remembers every answer. */
export function createStatCache(fetch: Fetcher, tickMs = 8) {
  const known = new Map<string, EnginePathFact>();
  const waiting = new Map<string, Waiter[]>();
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function flush() {
    timer = undefined;
    const batch = [...waiting.keys()];
    for (let at = 0; at < batch.length; at += statBatchLimit) await send(batch.slice(at, at + statBatchLimit));
  }

  async function send(paths: string[]) {
    try {
      const facts = await fetch(paths);
      const byPath = new Map(facts.map(fact => [fact.path, fact]));
      for (const path of paths) settle(path, byPath.get(path) ?? { path, exists: false, dir: false, size: 0 });
    } catch (error) {
      for (const path of paths) fail(path, error);
    }
  }

  function settle(path: string, fact: EnginePathFact) {
    known.set(path, fact);
    waiting.get(path)?.forEach(waiter => waiter.resolve(fact));
    waiting.delete(path);
  }

  function fail(path: string, error: unknown) {
    waiting.get(path)?.forEach(waiter => waiter.reject(error));
    waiting.delete(path);
  }

  function lookup(path: string): Promise<EnginePathFact> {
    const hit = known.get(path);
    if (hit) return Promise.resolve(hit);
    return new Promise((resolve, reject) => {
      const list = waiting.get(path) ?? [];
      list.push({ resolve, reject });
      waiting.set(path, list);
      timer ??= setTimeout(() => void flush(), tickMs);
    });
  }

  return {
    peek: (path: string) => known.get(path),
    stat: (paths: string[]) => Promise.all([...new Set(paths)].map(lookup)),
    forget: () => known.clear(),
  };
}

export type StatCache = ReturnType<typeof createStatCache>;
