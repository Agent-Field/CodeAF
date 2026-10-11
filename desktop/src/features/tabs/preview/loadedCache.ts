// Preview reads survive unmounting. Only a compact projection is kept by resource, never a transcript or a renderer.
type Entry = { revision?: string; value?: unknown; pending?: Promise<unknown> };
const capacity = 256;

/** An in-memory cache shares concurrent reads and keeps the last known value while its revision refreshes. */
export function createLoadedCache() {
  const entries = new Map<string, Entry>();
  function keep(key: string, entry: Entry) {
    entries.delete(key);
    entries.set(key, entry);
    if (entries.size > capacity) entries.delete(entries.keys().next().value!);
  }
  return {
    read<T>(key: string | undefined): T | undefined { return key ? entries.get(key)?.value as T | undefined : undefined; },
    seed<T>(key: string, value: T) {
      const entry = entries.get(key) ?? {};
      if (entry.pending) return;
      entry.value = value;
      keep(key, entry);
    },
    load<T>(key: string, revision: string, load: () => Promise<T>): Promise<T> {
      const known = entries.get(key);
      if (known && (known.revision === revision || known.revision === undefined)) {
        known.revision = revision;
        keep(key, known);
        if (known.pending) return known.pending as Promise<T>;
        if (known.value !== undefined) return Promise.resolve(known.value as T);
      }
      const entry: Entry = { revision, value: known?.value };
      const pending = Promise.resolve().then(load).then(value => {
        entry.value = value;
        entry.pending = undefined;
        return value;
      }, error => {
        // Failed reads can be tried again; no invented value is kept for a failed read.
        if (entries.get(key) === entry) entries.delete(key);
        throw error;
      });
      entry.pending = pending;
      keep(key, entry);
      return pending;
    },
  };
}

export const loadedCache = createLoadedCache();
export const resourceKey = (kind: string, session: string, target: string) => JSON.stringify([kind, session, target]);
