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

test('rejects native selects and Radix imports outside the themed boundary', () => {
 assert.ok(inspectSource('src/components/ui/Bad.tsx', 'const view = <select />;').length > 0);
 assert.ok(inspectSource('src/features/new.tsx', "import * as Select from '@radix-ui/react-select';").length > 0);
 assert.deepEqual(inspectSource('src/components/ui/Select.tsx', "import * as Select from '@radix-ui/react-select';"), []);
});

test('allows only the precise Radix package for each shared control', () => {
 assert.deepEqual(inspectSource('src/components/ui/Menu.tsx', "import * as Menu from '@radix-ui/react-context-menu'; import * as Dropdown from '@radix-ui/react-dropdown-menu';"), []);
 assert.ok(inspectSource('src/components/ui/Menu.tsx', "import * as Select from '@radix-ui/react-select';").length > 0);
 assert.ok(inspectSource('src/features/Tabs.tsx', "import * as Menu from '@radix-ui/react-context-menu';").length > 0);
});

test('keeps delayed previews inside their approved primitive boundary', () => {
 assert.deepEqual(inspectSource('src/components/ui/HoverPreview.tsx', "import * as Preview from '@radix-ui/react-hover-card';"), []);
 assert.ok(inspectSource('src/features/Tabs.tsx', "import * as Preview from '@radix-ui/react-hover-card';").length > 0);
 assert.ok(inspectSource('src/components/ui/HoverPreview.tsx', "import * as Menu from '@radix-ui/react-context-menu';").length > 0);
});
