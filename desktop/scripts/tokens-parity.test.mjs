import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { design } from './design-output.mjs';

// Source: Foundations F-COL-*, F-TINT-*, F-MO-18. Only the shimmer and the running breath loop; every other glyph motion plays once.
const { light, dark } = design.themes;
const iconSource = readFileSync(new URL('../src/components/ui/Icon.tsx', import.meta.url), 'utf8');
const iconNames = [...iconSource.match(/export const iconNames = \[([^\]]*)\]/)[1].matchAll(/'([^']+)'/g)].map(match => match[1]);
const motion = design.icons.motionByName;
// The glyphs whose own choreography plays once on a state change; adding one is a design decision, not a drive-by edit.
const playOnce = ['check', 'chevronRight', 'send', 'copy', 'pause', 'ban', 'triangleAlert', 'panelRight', 'sparkles', 'archive'];
// Deliberate aliases: two roles that must read as one colour. Anything else sharing a literal is a duplicate to reference with var().
const aliases = [];

test('light and dark declare exactly the same colour roles', () => {
 assert.deepEqual(Object.keys(light).filter(key => !(key in dark)), [], 'in light only');
 assert.deepEqual(Object.keys(dark).filter(key => !(key in light)), [], 'in dark only');
});

test('every icon name has a motion entry and only the play-once list uses once', () => {
 assert.ok(iconNames.length > 50, 'iconNames parsed');
 assert.deepEqual(iconNames.filter(name => !(name in motion)), []);
 const once = Object.keys(motion).filter(name => motion[name] === 'once').sort();
 assert.deepEqual(once, [...playOnce].sort());
});

const duplicates = theme => {
 const byValue = new Map();
 for (const [key, value] of Object.entries(theme)) if (/^oklch\(/.test(value)) byValue.set(value, [...(byValue.get(value) ?? []), key]);
 return [...byValue.values()].filter(keys => keys.length > 1 && !aliases.some(pair => keys.length === pair.length && pair.every(key => keys.includes(key))));
};

// Wave E removes history-added / preview-add-ink duplicating success; until it lands this stays todo.
test('no two colour roles share a literal oklch value except documented aliases', { todo: 'until wave E lands' }, () => {
 assert.deepEqual(duplicates(light), [], 'light');
 assert.deepEqual(duplicates(dark), [], 'dark');
});

test('every tint hue declares h, a and a swatch', () => {
 const hues = Object.entries(design.tints.hues);
 assert.ok(hues.length > 0);
 assert.ok(design.tints.default in design.tints.hues, 'default hue exists');
 for (const [name, hue] of hues) for (const field of ['h', 'a', 'swatch']) assert.ok(hue[field], `${name}.${field}`);
});
