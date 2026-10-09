import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MAX_NAMESPACES, browserNamespace, nativeNamespace, scrollStorageBase, storageKeyFor, touchNamespace } from './scrollStorage.ts';

const fake = (initial: Record<string, string> = {}) => {
  const map = new Map(Object.entries(initial));
  return { map, getItem: (k: string) => map.get(k) ?? null, setItem: (k: string, v: string) => { map.set(k, v); }, removeItem: (k: string) => { map.delete(k); } };
};

test('a browser tab keeps its token across a reload and never shares it with another tab', () => {
  const tabA = fake(); const tabB = fake();
  const first = browserNamespace(tabA, () => 'aaaa1111');
  assert.equal(browserNamespace(tabA, () => 'zzzz9999'), first);
  assert.notEqual(browserNamespace(tabB, () => 'bbbb2222'), first);
  assert.equal(storageKeyFor(first).startsWith(scrollStorageBase), true);
});

test('a token that is not ours is replaced, and blocked storage still yields a namespace', () => {
  const dirty = fake({ 'codeaf.desktop.windowToken': '../../x' });
  assert.equal(browserNamespace(dirty, () => 'clean0001'), 'browser.clean0001');
  assert.match(browserNamespace(undefined, () => 'solo0001'), /^browser\./);
});

test('native windows are named by label, so a relaunch finds its own place and another window cannot', () => {
  assert.equal(nativeNamespace('main'), 'native.main');
  assert.notEqual(storageKeyFor(nativeNamespace('w-2')), storageKeyFor(nativeNamespace('main')));
  assert.equal(nativeNamespace('a/b c'), 'native.a_b_c');
});

test('the oldest namespaces are removed beyond the cap', () => {
  const store = fake();
  for (let i = 0; i < MAX_NAMESPACES + 2; i++) { store.setItem(storageKeyFor(`n${i}`), '{}'); touchNamespace(store, `n${i}`, i); }
  assert.equal(store.getItem(storageKeyFor('n0')), null);
  assert.equal(store.getItem(storageKeyFor('n1')), null);
  assert.notEqual(store.getItem(storageKeyFor(`n${MAX_NAMESPACES + 1}`)), null);
});
