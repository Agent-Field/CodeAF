import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { css, design } from './design-output.mjs';

test('wave A preserves canonical swatch colours and emits only scoped tint rules', () => {
 const colours = { sand: 'oklch(.65 .1 65)', sage: 'oklch(.6 .09 150)', tide: 'oklch(.58 .13 240)', iris: 'oklch(.58 .14 285)', rose: 'oklch(.6 .13 12)', graphite: 'oklch(.55 .012 250)' };
 for (const [name, colour] of Object.entries(colours)) {
  const key = `places-swatch-${name}`;
  for (const theme of ['light', 'dark']) assert.equal(design.themes[theme][key], colour);
  assert.equal(design.tints.hues[name].swatch, `var(--${key})`);
  assert.ok(css.includes(`[data-tint="${name}"] { --h: ${design.tints.hues[name].h}; --a: ${design.tints.hues[name].a}; --swatch: var(--${key}); }`));
 }
 assert.ok(!css.includes('.tint-'));
 assert.equal(readFileSync(new URL('../src/styles/tokens.css', import.meta.url), 'utf8'), css);
});

test('wave A defines themed materials and routes landed backdrops through scrim', () => {
 assert.equal(design.foundation['frame-blur'], '40px');
 assert.equal(design.foundation['frame-saturate'], '1.4');
 assert.equal(design.foundation['places-quicklook-scrim'], 'var(--scrim)');
 for (const [theme, scrim, glass, tab] of [
  ['light', 'oklch(0 0 0 / .2)', 'oklch(.93 .035 var(--h) / .8)', 'oklch(1 0 0 / .55)'],
  ['dark', 'oklch(0 0 0 / .4)', 'oklch(.27 .035 var(--h) / .84)', 'oklch(1 0 0 / .09)'],
 ]) {
  assert.equal(design.themes[theme].scrim, scrim);
  assert.equal(design.themes[theme]['frame-glass'], glass);
  assert.equal(design.themes[theme].tab, tab);
  assert.equal(design.themes[theme]['palette-backdrop'], 'var(--scrim)');
 }
});
