/**
 * One in-flight read per key. A later caller joins the same promise, and a
 * failure is forgotten so the next attempt can try again. A success stays:
 * the value does not change, and repeating the read would only repeat the GET.
 */
export function toolReadOnce<T>(cache: Map<string, Promise<T>>, key: string, load: () => Promise<T>): Promise<T> {
  const hit = cache.get(key);
  if (hit) return hit;
  const pending = load().then(
    (value) => value,
    (error: unknown) => {
      if (cache.get(key) === pending) cache.delete(key);
      throw error;
    },
  );
  cache.set(key, pending);
  return pending;
}
