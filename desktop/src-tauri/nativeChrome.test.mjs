import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// Shell 2a/2d, F-MAT-*: the macOS window config and the design tokens are two copies of one fact (where the traffic lights sit
// and how much room the web content leaves for them), so they are read against each other rather than trusted separately.
const read = (path) => JSON.parse(readFileSync(new URL(path, import.meta.url), 'utf8'));
const conf = read('./tauri.macos.conf.json');
const tokens = read('../src/design/tokens.json');
const win = conf.app.windows[0];
const lights = { count: 3, diameter: 12, gap: 8 };

test('trafficLightPosition equals the nativeWindow token', () => {
  assert.deepEqual(win.trafficLightPosition, tokens.nativeWindow.trafficLightPosition);
});

test('the window is an overlay titlebar with the sidebar vibrancy effect', () => {
  assert.equal(win.titleBarStyle, 'Overlay');
  assert.equal(win.transparent, true);
  assert.ok(win.windowEffects.effects.includes('sidebar'));
  assert.equal(win.windowEffects.state, 'followsWindowActiveState');
});

test('native-controls-inset clears the three lights and their gaps', () => {
  const inset = parseInt(tokens.color?.['native-controls-inset'] ?? findToken(tokens, 'native-controls-inset'), 10);
  const right = win.trafficLightPosition.x + lights.count * lights.diameter + (lights.count - 1) * lights.gap;
  assert.ok(Number.isFinite(inset), 'token native-controls-inset is a px value');
  assert.ok(inset >= right, `inset ${inset}px must clear the lights' right edge at ${right}px`);
});

function findToken(node, key) {
  if (node && typeof node === 'object') {
    if (key in node) return node[key];
    for (const v of Object.values(node)) { const hit = findToken(v, key); if (hit !== undefined) return hit; }
  }
  return undefined;
}
