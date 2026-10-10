import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  LEGACY_ALLOW,
  THEME_GLOBS,
  isLegacyName,
  main,
  readSourceFiles,
  renderReport,
  reportLegacy,
  scanText,
} from './legacy-tokens.mjs';

// The fixture is the contract. The live tokens.json case only checks that
// the allow-list still classifies the tree the way the lane brief describes.

const FIXTURE = {
  foundation: {
    'font-size-caption': '10px',
    'font-sans': 'var(--sans)',
    'type-prose-size': '13px',
    duration: '220ms',
    'duration-fast': '140ms',
    'duration-sidebar': '320ms',
    'dur-fast': '120ms',
    'opacity-disabled': '0.65',
    'radius-swatch': '5px',
    'control-height': '28px',
    'chrome-ease': 'cubic-bezier(.2,.8,.2,1)',
    'focus-ring-width': '2px',
    'sidebar-width': '252px',
    sans: 'system-ui',
  },
  themes: {
    light: {
      sidebar: '#fff',
      text: 'var(--ink)',
      muted: '#888',
      ink: '#111',
      'chrome-canvas': '#eee',
      'control-hover': '#ddd',
    },
    dark: {
      sidebar: '#000',
      text: 'var(--ink)',
      muted: '#aaa',
      ink: '#eee',
      'chrome-canvas': '#111',
      'control-hover': '#222',
    },
  },
};

const FILES = [
  { path: 'src/one.css', text: '.sidebar { color: var(--sidebar); }\n.keep { color: var(--ink); }\n' },
  { path: 'src/type.css', text: '.a { color: var(--text); }\n.b { color: var(--muted); font-size: var(--font-size-caption); }\n' },
  { path: 'src/motion.css', text: '.a { transition: var(--duration-fast); }\n.b { transition: var(--duration-sidebar); }\n.c { width: var(--control-height); }\n.d { background: var(--control-hover); }\n' },
  {
    path: 'src/read.ts',
    text: [
      "const a = design.foundation['duration'];",
      "const b = design.foundation['duration-fast'];",
      "const c = (design.foundation as Record<string, string>)['opacity-disabled'];",
      'const d = design.foundation.duration;',
      "const e = design.themes[resolvedTheme]['chrome-canvas'];",
      "const f = design.foundation['type-prose-size'];",
      "const g = design.themes.light['ink'];",
      "const h = design.foundation?.['font-sans'];",
      "const i = design.themes.dark['sidebar'];",
      "redesign.foundation['duration'];",
      'myvar(--duration);',
    ].join('\n') + '\n',
  },
  { path: 'src/gone.css', text: '.old { width: var(--preview-width); }\n' },
  { path: 'src/decoy.css', text: '.text-muted { color: var(--focus-ring-width); }\n' },
];

function quiet() {
  return { write() {} };
}

test('the allow-list is the lane brief, with theme globs marked', () => {
  assert.deepEqual(LEGACY_ALLOW, [
    'sidebar',
    'text',
    'muted',
    'border',
    'menu-highlight',
    'menu-selected',
    'control-*',
    'overlay-surface',
    'chrome-*',
    'focus-ring',
    'palette-*',
    'shadow',
    'selection-shadow',
    'tab-selected',
    'tab-strip',
    'font-size-*',
    'font-weight-semibold',
    'font-weight-strong',
    'font-sans',
    'font-mono',
    'radius-sm',
    'radius-md',
    'radius-lg',
    'duration',
    'duration-fast',
    'duration-overlay',
    'duration-directional',
    'duration-work',
    'opacity-disabled',
    'swatch-width',
    'swatch-height',
    'radius-swatch',
    'preview-width',
  ]);
  assert.deepEqual([...THEME_GLOBS], ['control-*', 'chrome-*', 'palette-*']);
});

test('exact names do not swallow longer keys, and palette globs skip foundation measurements', () => {
  const cases = [
    ['duration', {}, true],
    ['duration-fast', { inFoundation: true }, true],
    ['duration-sidebar', { inFoundation: true }, false],
    ['dur-fast', { inFoundation: true }, false],
    ['shimmer-duration', { inFoundation: true }, false],
    ['sidebar', { inThemes: true }, true],
    ['sidebar-width', { inFoundation: true }, false],
    ['text', { inThemes: true }, true],
    ['success-text', { inThemes: true }, false],
    ['focus-ring', { inThemes: true }, true],
    ['focus-ring-width', { inFoundation: true }, false],
    ['shadow', { inThemes: true }, true],
    ['selection-shadow', { inThemes: true }, true],
    ['tab-strip', { inThemes: true }, true],
    ['tab-strip-height', { inFoundation: true }, false],
    ['tab', { inThemes: true }, false],
    ['font-size-caption', { inFoundation: true }, true],
    ['font-size-prose', { inFoundation: true }, true],
    ['site-icon-font-size', { inFoundation: true }, false],
    ['font-weight-semibold', { inFoundation: true }, true],
    ['font-weight-regular', { inFoundation: true }, false],
    ['control-hover', { inThemes: true }, true],
    ['control-height', { inFoundation: true }, false],
    ['control-height', { inFoundation: true, inThemes: true }, true],
    ['control-height', {}, true],
    ['chrome-canvas', { inThemes: true }, true],
    ['chrome-ease', { inFoundation: true }, false],
    ['palette-shadow', { inThemes: true }, true],
    ['palette-max-width', { inFoundation: true }, false],
    ['preview-width', { inFoundation: true }, true],
    ['preview-card-width', { inFoundation: true }, false],
    ['opacity-disabled', { inFoundation: true }, true],
    ['opacity-control-disabled', { inFoundation: true }, false],
    ['ink', { inThemes: true }, false],
  ];
  for (const [name, where, expected] of cases) {
    assert.equal(isLegacyName(name, where), expected, `${name} ${JSON.stringify(where)}`);
  }
});

test('scan ignores lookalikes and keeps the full custom-property name', () => {
  const hits = scanText('src/a.ts', [
    "redesign.foundation['duration']",
    'myvar(--duration)',
    'var( --duration )',
    'var(--duration-sidebar)',
    "design.foundation['duration-sidebar']",
    "(design.foundation as Record<string, string>)['opacity-disabled']",
    "design.themes[resolvedTheme]['chrome-canvas']",
    "design.foundation?.['font-sans']",
    "design?.themes?.light?.['sidebar']",
  ].join('\n'));
  assert.deepEqual(hits.map(hit => `${hit.line}:${hit.token}:${hit.form}`), [
    '3:duration:var(--duration)',
    '4:duration-sidebar:var(--duration-sidebar)',
    '5:duration-sidebar:design.foundation',
    '6:opacity-disabled:design.foundation',
    '7:chrome-canvas:design.themes',
    '8:font-sans:design.foundation',
    '9:sidebar:design.themes',
  ]);
});

test('a fixture tokens object lists legacy keys and their readers', () => {
  const report = reportLegacy({ tokens: FIXTURE, files: FILES });
  assert.deepEqual(report.present.map(row => `${row.token}|${row.home}|${row.consumers}`), [
    'chrome-canvas|themes|1',
    'control-hover|themes|1',
    'duration|foundation|2',
    'duration-fast|foundation|2',
    'font-sans|foundation|1',
    'font-size-caption|foundation|1',
    'muted|themes|1',
    'opacity-disabled|foundation|1',
    'radius-swatch|foundation|0',
    'sidebar|themes|2',
    'text|themes|1',
  ]);
  assert.deepEqual(
    report.consumers.filter(row => row.token === 'duration').map(row => `${row.path}:${row.line}:${row.form}`),
    ["src/read.ts:1:design.foundation", 'src/read.ts:4:design.foundation'],
  );
  assert.deepEqual(
    report.consumers.filter(row => row.token === 'duration-fast').map(row => row.form),
    ['var(--duration-fast)', 'design.foundation'],
  );
  assert.equal(report.consumers.some(row => row.token === 'radius-swatch'), false);
  assert.equal(report.consumers.some(row => row.token === 'ink' || row.token === 'control-height' || row.token === 'type-prose-size'), false);
  assert.deepEqual(report.removed, [
    { token: 'preview-width', form: 'var(--preview-width)', path: 'src/gone.css', line: 1 },
  ]);
  assert.match(
    renderReport(report),
    /legacy tokens: 11 still defined, 13 consumers, 1 removed key still referenced/,
  );
});

test('the printed report is three tables, and an empty tree still has headers', () => {
  const report = reportLegacy({ tokens: FIXTURE, files: FILES });
  const text = renderReport(report);
  assert.match(text, /\| token \| home \| consumers \|/);
  assert.match(text, /\| radius-swatch \| foundation \| 0 \|/);
  assert.match(text, /\| preview-width \| var\(--preview-width\) \| src\/gone\.css:1 \|/);
  assert.match(text, /\| duration \| design\.foundation \| src\/read\.ts:1 \|/);
  const empty = renderReport(reportLegacy({ tokens: { foundation: { ink: '#111' }, themes: { light: {}, dark: {} } }, files: [] }));
  assert.match(empty, /legacy tokens: 0 still defined, 0 consumers, 0 removed keys still referenced/);
  assert.match(empty, /\| \(none\) \|/);
});

test('--strict fails only when a removed allow-list key is still referenced', () => {
  let out = '';
  let err = '';
  const capture = {
    stdout: { write(chunk) { out += chunk; } },
    stderr: { write(chunk) { err += chunk; } },
  };
  const strict = main({
    argv: ['node', 'legacy-tokens.mjs', '--strict'],
    tokens: FIXTURE,
    files: FILES,
    ...capture,
  });
  assert.equal(strict, 1);
  assert.match(err, /^strict: 1 removed key is still referenced\n$/);
  assert.match(out, /\| preview-width \| var\(--preview-width\) \| src\/gone\.css:1 \|/);

  out = '';
  err = '';
  const open = main({
    argv: ['node', 'legacy-tokens.mjs'],
    tokens: FIXTURE,
    files: FILES,
    stdout: { write(chunk) { out += chunk; } },
    stderr: { write(chunk) { err += chunk; } },
  });
  assert.equal(open, 0);
  assert.equal(err, '');
  assert.match(out, /\| sidebar \| themes \| 2 \|/);

  const defined = {
    ...FIXTURE,
    foundation: { ...FIXTURE.foundation, 'preview-width': '240px' },
  };
  assert.equal(main({
    argv: ['node', 'legacy-tokens.mjs', '--strict'],
    tokens: defined,
    files: FILES,
    stdout: quiet(),
    stderr: quiet(),
  }), 0);

  assert.equal(main({
    argv: ['node', 'legacy-tokens.mjs', '--strict'],
    tokens: FIXTURE,
    files: FILES.filter(file => file.path !== 'src/gone.css'),
    stdout: quiet(),
    stderr: quiet(),
  }), 0);

  err = '';
  assert.equal(main({
    argv: ['node', 'legacy-tokens.mjs', '--json'],
    tokens: FIXTURE,
    files: [],
    stdout: quiet(),
    stderr: { write(chunk) { err += chunk; } },
  }), 2);
  assert.match(err, /unknown argument --json/);
});

test('source walk keeps code under src and tests and skips the rest', () => {
  const root = mkdtempSync(join(tmpdir(), 'legacy-tok-'));
  try {
    mkdirSync(join(root, 'src', 'nested'), { recursive: true });
    mkdirSync(join(root, 'src', 'node_modules'), { recursive: true });
    mkdirSync(join(root, 'tests'), { recursive: true });
    writeFileSync(join(root, 'src', 'nested', 'a.css'), 'var(--muted)\n');
    writeFileSync(join(root, 'src', 'note.md'), 'var(--muted)\n');
    writeFileSync(join(root, 'src', 'node_modules', 'x.css'), 'var(--muted)\n');
    writeFileSync(join(root, 'tests', 'b.ts'), "design.foundation['duration']\n");
    const files = readSourceFiles([join(root, 'src'), join(root, 'tests'), join(root, 'missing')], root);
    assert.deepEqual(files.map(file => file.path).sort(), ['src/nested/a.css', 'tests/b.ts']);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('real tokens.json classifies the chrome palette and leaves component tokens alone', () => {
  const tokens = JSON.parse(readFileSync(new URL('../src/design/tokens.json', import.meta.url), 'utf8'));
  const names = new Set(reportLegacy({ tokens, files: [] }).present.map(row => row.token));
  for (const name of ['sidebar', 'text', 'muted', 'border', 'menu-highlight', 'menu-selected', 'overlay-surface', 'focus-ring', 'shadow', 'selection-shadow', 'tab-selected', 'tab-strip', 'chrome-canvas', 'control-hover', 'palette-shadow', 'font-size-caption', 'font-weight-semibold', 'font-weight-strong', 'font-sans', 'font-mono', 'radius-md', 'duration', 'duration-fast', 'duration-overlay', 'duration-directional', 'duration-work', 'opacity-disabled', 'swatch-width', 'swatch-height', 'radius-swatch', 'preview-width']) {
    assert.equal(names.has(name), true, name);
  }
  for (const name of ['ink', 'dur-fast', 'type-prose-size', 'control-height', 'control-gap', 'control-pad', 'chrome-ease', 'palette-max-width', 'palette-blur', 'focus-ring-width', 'font-weight-regular', 'duration-sidebar', 'duration-none', 'radius-chip', 'tab', 'tab-hover', 'tab-strip-height', 'sidebar-width', 'sh-1', 'opacity-control-disabled', 'site-icon-font-size']) {
    assert.equal(names.has(name), false, name);
  }
});

test('the desktop tree prints a table and does not fail closed while the keys still exist', () => {
  let out = '';
  const code = main({
    argv: ['node', 'legacy-tokens.mjs'],
    stdout: { write(chunk) { out += chunk; } },
    stderr: quiet(),
  });
  assert.equal(code, 0);
  assert.match(out, /\| token \| home \| consumers \|/);
  assert.match(out, /\| sidebar \| themes \|/);
  assert.match(out, /\| text \| themes \|/);
  assert.match(out, /var\(--text\)/);
  assert.match(out, /src\/styles\/ui\.css:\d+/);
  assert.match(out, /\| chrome-canvas \| design\.themes \| src\/design\/ThemeProvider\.tsx:\d+ \|/);
  assert.doesNotMatch(out, /\/home\//);
});
