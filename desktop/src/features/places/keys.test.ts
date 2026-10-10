import assert from 'node:assert/strict';
import test from 'node:test';
import { shortcutOf } from '../../design/keyboard.ts';
import { placeKeyCommand, type PlaceKeyContext } from './keys.ts';

const context: PlaceKeyContext = { place: 'pl_aaaaaaaaaaaaaaaa', order: ['pl_1', 'pl_2'] };
const key = (k: string, extra: Record<string, unknown> = {}) => ({ key: k, code: '', metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...extra }) as Parameters<typeof shortcutOf>[0];
const run = (event: Parameters<typeof shortcutOf>[0], platform: Parameters<typeof shortcutOf>[1]) => {
  const shortcut = shortcutOf(event, platform);
  return shortcut && placeKeyCommand(shortcut, context);
};

for (const [name, mac, primary, slot] of [['mac', true, { metaKey: true }, { ctrlKey: true }], ['linux', false, { ctrlKey: true }, { altKey: true }]] as const) {
  test(`${name}: the place chords map to navigation commands`, () => {
    assert.deepEqual(run(key('p', primary), { mac }), { type: 'go-to-chooser' });
    assert.deepEqual(run(key('P', { ...primary, shiftKey: true }), { mac }), { type: 'all-places' });
    assert.deepEqual(run(key('0', primary), { mac }), { type: 'home' });
    assert.deepEqual(run(key('W', { ...primary, shiftKey: true }), { mac }), { type: 'close-place' });
    assert.deepEqual(run(key('n', primary), { mac }), { type: 'new-window' });
    assert.deepEqual(run(key('z', primary), { mac }), { type: 'undo' });
  });

  test(`${name}: slot 0 is Now, 1–9 are Pinned then Open, an empty slot is free`, () => {
    assert.deepEqual(run(key('0', { ...slot, code: 'Digit0' }), { mac }), { type: 'jump', place: 'now' });
    assert.deepEqual(run(key('1', { ...slot, code: 'Digit1' }), { mac }), { type: 'jump', place: 'pl_1' });
    assert.deepEqual(run(key('2', { ...slot, code: 'Digit2' }), { mac }), { type: 'jump', place: 'pl_2' });
    assert.equal(run(key('3', { ...slot, code: 'Digit3' }), { mac }), undefined);
  });

  test(`${name}: up is ⌘↑ on a Home only`, () => {
    assert.deepEqual(run(key('ArrowUp', primary), { mac, home: true }), { type: 'up' });
    assert.notDeepEqual(run(key('ArrowUp', primary), { mac, home: false }), { type: 'up' });
  });

  test(`${name}: Space is Quick Look for the surface, never a place command or a text-field key`, () => {
    assert.equal(run(key(' '), { mac }), undefined);
    assert.equal(shortcutOf({ ...key(' '), target: { tagName: 'TEXTAREA', value: '' } as unknown as EventTarget }, { mac }), undefined);
    assert.equal(shortcutOf({ ...key(' '), target: { tagName: 'INPUT', value: 'x' } as unknown as EventTarget }, { mac }), undefined);
  });
}

test('⌘[ is focus history back (I2.6), not a place command', () => {
  assert.equal(run(key('[', { metaKey: true, code: 'BracketLeft' }), { mac: true }), undefined);
});
