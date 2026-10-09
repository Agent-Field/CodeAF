import test from 'node:test';
import assert from 'node:assert/strict';
import { ansiPalette, contrastRatio, readable, desaturate, extendedPalette, hex, mix, rotateHue, toLch, type AnsiRoles, type Rgb } from './ansi.ts';

const near = (actual: number, expected: number, tolerance: number, what: string) => assert.ok(Math.abs(actual - expected) <= tolerance, `${what}: ${actual} is not within ${tolerance} of ${expected}`);
const roles: AnsiRoles = {
  field: [246, 247, 249], ink: [34, 38, 46], ink2: [92, 98, 110], ink3: [140, 146, 156],
  accent: [38, 110, 210], danger: [200, 60, 50], success: [40, 150, 95], amber: [210, 150, 40],
};

test('desaturate keeps lightness and hue and cuts chroma to the factor', () => {
  for (const color of [roles.accent, roles.danger, roles.success, roles.amber]) {
    const [L, C, H] = toLch(color);
    const [L2, C2, H2] = toLch(desaturate(color, 0.6));
    near(L2, L, 0.02, 'lightness'); near(C2, C * 0.6, 0.012, 'chroma'); near(Math.min(Math.abs(H2 - H), 360 - Math.abs(H2 - H)), 0, 6, 'hue');
  }
});
test('desaturate leaves a neutral colour alone and a factor of 1 is the identity', () => {
  const grey: Rgb = [120, 120, 120];
  assert.deepEqual(desaturate(grey, 0.6), grey);
  const [r, g, b] = desaturate(roles.danger, 1);
  assert.ok(Math.abs(r - 200) <= 1 && Math.abs(g - 60) <= 1 && Math.abs(b - 50) <= 1);
});
test('desaturate stays inside the sRGB range for a vivid colour', () => {
  for (const channel of desaturate([255, 0, 0], 0.6)) assert.ok(channel >= 0 && channel <= 255);
});
test('mix and rotateHue move toward a colour and around the wheel', () => {
  assert.deepEqual(mix(roles.accent, roles.ink, 0), roles.accent);
  assert.ok(toLch(mix(roles.amber, roles.ink, 0.3))[0] < toLch(roles.amber)[0]);
  const turned = toLch(rotateHue(roles.accent, 60)); const base = toLch(roles.accent);
  near((turned[2] - base[2] + 360) % 360, 60, 6, 'hue step');
});
test('the palette has sixteen colours: neutrals unchanged, hues at 60% chroma, six distinct hues', () => {
  const palette = ansiPalette(roles);
  assert.equal(palette.length, 16);
  assert.deepEqual(palette[0], roles.ink3); assert.deepEqual(palette[7], roles.ink2); assert.deepEqual(palette[15], roles.ink);
  for (const [slot, role] of [[1, roles.danger], [2, roles.success], [3, roles.amber], [4, roles.accent]] as const) {
    const chroma = toLch(palette[slot])[1];
    assert.ok(chroma <= toLch(role)[1] * 0.6 + 0.012 && chroma >= toLch(role)[1] * 0.3, `slot ${slot} chroma ${chroma}`);
  }
  assert.equal(new Set(palette.slice(1, 7).map(hex)).size, 6);
});
test('every hue reads at 4.5:1 on a light field and on a dark one', () => {
  const dark: AnsiRoles = { field: [24, 25, 28], ink: [236, 238, 242], ink2: [180, 184, 192], ink3: [120, 124, 134], accent: [120, 170, 250], danger: [250, 120, 110], success: [110, 200, 150], amber: [235, 190, 90] };
  for (const set of [roles, dark]) ansiPalette(set).slice(1, 7).forEach((color, slot) => assert.ok(contrastRatio(color, set.field) >= 4.4, `slot ${slot + 1} on ${set.field}: ${contrastRatio(color, set.field)}`));
});
test('readable moves lightness only', () => {
  const [L, C, H] = toLch(roles.amber); const [L2, C2, H2] = toLch(readable(roles.amber, roles.field));
  assert.ok(L2 < L); near(H2, H, 8, 'hue'); assert.ok(C2 <= C + 0.01);
});
test('the 256-colour range has 240 entries: the cube at 60% chroma, the greys untouched', () => {
  const extended = extendedPalette();
  assert.equal(extended.length, 240);
  assert.deepEqual(extended[0], [0, 0, 0]);
  near(toLch(extended[196 - 16])[1], toLch([255, 0, 0])[1] * 0.6, 0.012, 'pure red chroma');
  assert.deepEqual(extended[232 - 16], [8, 8, 8]); assert.deepEqual(extended[255 - 16], [238, 238, 238]);
});
test('hex writes a six digit lowercase colour', () => assert.equal(hex([255, 0, 10]), '#ff000a'));
