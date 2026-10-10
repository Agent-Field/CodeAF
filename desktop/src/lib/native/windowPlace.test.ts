import assert from 'node:assert/strict';
import test from 'node:test';
import { eventIsForWindow, isWindowLabel, switchPlace, windowPlace, windowPlaceStorageKey, type WindowPlaceDeps } from './windowPlace.ts';

const PL = 'pl_0123456789abcdef';

function deps(over: { label?: string; search?: string; saved?: Record<string, string> } = {}) {
  const saved = { ...over.saved };
  const titles: string[] = [];
  const d: WindowPlaceDeps = {
    label: () => over.label ?? 'main',
    search: () => over.search ?? '',
    saved: (label) => saved[label] ?? null,
    remember: (label, key) => { saved[label] = key; },
    setDocumentTitle: (t) => { titles.push(t); },
  };
  return { d, saved, titles };
}

test('an address naming a place wins in any window', () => {
  assert.deepEqual(windowPlace(deps({ label: 'w-2', search: `?place=${PL}` }).d), { label: 'w-2', placeKey: PL });
  assert.deepEqual(windowPlace(deps({ search: `?place=${PL}`, saved: { main: 'now' } }).d), { label: 'main', placeKey: PL });
});

test('a new window with no address opens on Now, ignoring what main remembered', () => {
  assert.equal(windowPlace(deps({ label: 'w-3', saved: { main: PL, 'w-3': PL } }).d).placeKey, 'now');
});

test('main returns to the place it last showed', () => {
  assert.equal(windowPlace(deps({ saved: { main: PL } }).d).placeKey, PL);
});

test('junk in the address or the store falls back to Now', () => {
  assert.equal(windowPlace(deps({ search: '?place=../etc' }).d).placeKey, 'now');
  assert.equal(windowPlace(deps({ search: '?place=../etc', saved: { main: 'bogus' } }).d).placeKey, 'now');
  assert.equal(windowPlace(deps({ label: 'w-1', search: '?place=' }).d).placeKey, 'now');
});

test('switchPlace remembers the place for this window and names the document', () => {
  const { d, saved, titles } = deps({ label: 'w-4' });
  assert.deepEqual(switchPlace(PL as never, 'Marketing', d), { label: 'w-4', placeKey: PL });
  assert.equal(saved['w-4'], PL);
  assert.deepEqual(titles, ['Marketing']);
  assert.equal(windowPlaceStorageKey('w-4'), 'codeaf.desktop.window.w-4.place');
});

test('switchPlace refuses a malformed key before touching anything', () => {
  const { d, saved, titles } = deps();
  assert.throws(() => switchPlace('nope' as never, 'x', d));
  assert.deepEqual(saved, {});
  assert.deepEqual(titles, []);
});

test('only this window takes an event addressed to it', () => {
  assert.equal(eventIsForWindow(undefined, 'w-2'), true);
  assert.equal(eventIsForWindow('w-2', 'w-2'), true);
  assert.equal(eventIsForWindow('w-3', 'w-2'), false);
});

test('window labels are main or w- and digits', () => {
  assert.equal(isWindowLabel('main'), true);
  assert.equal(isWindowLabel('w-12'), true);
  assert.equal(isWindowLabel('web-1'), false);
  assert.equal(isWindowLabel('w-'), false);
});
