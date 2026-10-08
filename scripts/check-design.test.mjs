import { test } from 'node:test';
import assert from 'node:assert/strict';
import { design } from './design-output.mjs';
import { inspectSource, inspectCss } from './design-policy.mjs';
test('rejects per-screen icons, raw controls and inline styles', () => {
 const errors = inspectSource('src/features/new.tsx', `import { SearchIcon } from '@animateicons/react/lucide'; const view = <button style={{padding: '19px'}}><svg><path d="M0 0" /></svg></button>;`);
 assert.equal(errors.length, 5);
});
test('accepts approved shared primitives and icon boundary', () => {
 assert.deepEqual(inspectSource('src/features/new.tsx', `import { Button, Icon } from '../components/ui'; const view = <Button><Icon name="search" /></Button>;`), []);
 assert.deepEqual(inspectSource('src/components/ui/Icon.tsx', `import { SearchIcon } from '@animateicons/react/lucide/search-icon';`), []);
});
test('rejects raw CSS colors, dimensions, weights and unknown tokens', () => {
 const errors = inspectCss('src/features/new.css', '.card { padding: 19px; color: #abc; font-weight: 600; gap: var(--imaginary); }', design);
 assert.equal(errors.length, 4);
});
test('accepts token-based CSS and rejects invented breakpoints', () => {
 assert.deepEqual(inspectCss('src/features/new.css', '.card { padding: var(--space-4); color: var(--text); } @media (max-width: 850px) { .card { display: block; } }', design), []);
 assert.equal(inspectCss('src/features/new.css', '@media (max-width: 999px) { .card { display: block; } }', design).length, 1);
});

test('rejects named colors and per-screen stroke/font overrides', () => {
 const errors = inspectCss('src/features/new.css', '.card { color: red; stroke-width: 3; font-family: Arial; }', design);
 assert.equal(errors.length, 3);
});
