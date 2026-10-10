import assert from 'node:assert/strict';
import { test } from 'node:test';
import { design, generatedFiles } from './design-output.mjs';

// Places 10a and 9c separate rail controls from tab controls and the MRU switcher.
test('the place rail exposes the measured row, marks, drag and switcher geometry', () => {
  const expected = {
    'rail-row-height': '32px',
    'rail-place-row-height': 'var(--rail-row-height)',
    'rail-row-font': '13px',
    'rail-row-gap': '1px',
    'rail-group-gap': '16px',
    'places-swatch-rail': '10px',
    'rail-dot-size': 'var(--mark-dot)',
    'rail-meta-font': 'var(--type-caption-size)',
    'rail-close-size': '20px',
    'rail-insertion-line': '2px',
    'rail-drag-ghost-opacity': '.95',
    'rail-switcher-width': '280px',
  };
  for (const [name, value] of Object.entries(expected)) {
    assert.equal(design.foundation[name], value, name);
    assert.ok(generatedFiles['src/styles/tokens.css'].includes(`--${name}: ${value};`), name);
  }
  assert.equal(design.foundation['mark-dot'], '6px');
  assert.equal(design.foundation['type-caption-size'], '11px');
  // A rail-specific switcher must not resize the existing tab MRU overlay.
  assert.equal(design.foundation['switcher-width'], '320px');
});
