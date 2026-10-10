// Reports tokens Foundations and Components do not name, and every remaining
// read of those tokens. The allow-list is the whole definition of "legacy"
// (F-COL-27, F-TYPE-16, F-SP-17, F-RAD-7, F-SH-5, F-MO-3). A name on that
// list that is no longer in tokens.json is a removed key: --strict exits
// non-zero when source still references one. Keys that are still defined
// are only reported, so the inventory can shrink one key at a time.
//
// Usage: node desktop/scripts/legacy-tokens.mjs [--strict]
// The npm script name design:legacy is added by the integrator, not here.
// scripts/ is not scanned: this file and the design generator mention the
// names on purpose, and counting those mentions would report the inventory
// as its own consumer.

import { existsSync, readFileSync, readdirSync, realpathSync } from 'node:fs';
import { extname, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

// Exact names match the whole key. A trailing * is a prefix of one or more
// characters. control-*, chrome-* and palette-* are the old chrome palette
// (theme colours). font-size-* is the retired type scale and matches in
// either home. A foundation-only key that merely shares a palette prefix
// (control-height, palette-max-width, chrome-ease) is a measurement the
// components still use, so it stays off the report while it exists.
export const LEGACY_ALLOW = [
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
];

export const THEME_GLOBS = new Set(['control-*', 'chrome-*', 'palette-*']);

const SOURCE_EXT = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.css']);
const SKIP_DIR = new Set(['node_modules', 'dist', 'coverage']);

const desktopRoot = fileURLToPath(new URL('..', import.meta.url));
const tokensPath = fileURLToPath(new URL('../src/design/tokens.json', import.meta.url));
const defaultRoots = [
  fileURLToPath(new URL('../src', import.meta.url)),
  fileURLToPath(new URL('../tests', import.meta.url)),
];

// The cast group stops before ")" and "[" so `as Record<string, string>`
// does not swallow the key that follows the cast.
const CAST = '(?:\\s+as\\s+[A-Za-z0-9_.<>,\\s|&\'"-]+)?\\)?';
const MEMBER = '(?:\\?\\.|\\.)';
const VAR_USE = new RegExp('(?<![\\w$-])var\\(\\s*--([A-Za-z0-9-]+)', 'g');
const FOUNDATION_KEY = new RegExp(`(?<![\\w$])design${MEMBER}foundation${CAST}(?:\\?\\.)?\\s*\\[\\s*(['"\`])([^'"\`]+)\\1`, 'g');
const FOUNDATION_DOT = new RegExp(`(?<![\\w$])design${MEMBER}foundation${MEMBER}([A-Za-z_][A-Za-z0-9_]*)(?![\\w-])`, 'g');
const THEMES_KEY = new RegExp(`(?<![\\w$])design${MEMBER}themes(?:${MEMBER}[A-Za-z_][A-Za-z0-9_]*|(?:\\?\\.)?\\[[^\\]]*\\])${CAST}(?:\\?\\.)?\\s*\\[\\s*(['"\`])([^'"\`]+)\\1`, 'g');

export function isLegacyName(name, where = {}) {
  for (const pattern of LEGACY_ALLOW) {
    if (!pattern.endsWith('*') && name === pattern) return true;
  }
  for (const pattern of LEGACY_ALLOW) {
    if (!pattern.endsWith('*')) continue;
    const prefix = pattern.slice(0, -1);
    if (!name.startsWith(prefix) || name.length === prefix.length) continue;
    // Theme globs name colours. A foundation measurement with the same
    // prefix is not that colour, but a name that has already been deleted
    // still matches: the read is what --strict is for.
    if (THEME_GLOBS.has(pattern) && where.inFoundation && !where.inThemes) return false;
    return true;
  }
  return false;
}

export function tokenHomes(tokens) {
  const homes = new Map();
  const add = (name, home) => {
    const row = homes.get(name) ?? { inFoundation: false, inThemes: false };
    row[home] = true;
    homes.set(name, row);
  };
  const foundation = tokens?.foundation;
  if (foundation && typeof foundation === 'object') {
    for (const name of Object.keys(foundation)) add(name, 'inFoundation');
  }
  const themes = tokens?.themes;
  if (themes && typeof themes === 'object') {
    for (const theme of Object.values(themes)) {
      if (!theme || typeof theme !== 'object' || Array.isArray(theme)) continue;
      for (const name of Object.keys(theme)) add(name, 'inThemes');
    }
  }
  return homes;
}

function homeLabel(where) {
  if (where.inFoundation && where.inThemes) return 'foundation+themes';
  if (where.inFoundation) return 'foundation';
  return 'themes';
}

function hits(re, text, formFor) {
  re.lastIndex = 0;
  const found = [];
  for (const match of text.matchAll(re)) {
    const token = match[match.length - 1];
    found.push({ token, form: formFor(token), index: match.index });
  }
  return found;
}

// Every var(--name) and every literal design.foundation / design.themes
// read. A local alias of design.foundation is not followed: the name has
// to be written on the design object itself.
export function scanText(path, text) {
  const raw = [
    ...hits(VAR_USE, text, token => `var(--${token})`),
    ...hits(FOUNDATION_KEY, text, () => 'design.foundation'),
    ...hits(FOUNDATION_DOT, text, () => 'design.foundation'),
    ...hits(THEMES_KEY, text, () => 'design.themes'),
  ];
  raw.sort((a, b) => a.index - b.index);
  let line = 1;
  let cursor = 0;
  return raw.map(hit => {
    while (cursor < hit.index) {
      if (text.charCodeAt(cursor) === 10) line += 1;
      cursor += 1;
    }
    return { token: hit.token, form: hit.form, path, line, index: hit.index };
  });
}

export function reportLegacy({ tokens, files }) {
  const homes = tokenHomes(tokens);
  const present = [];
  for (const [token, where] of homes) {
    if (isLegacyName(token, where)) present.push({ token, home: homeLabel(where), consumers: 0 });
  }
  present.sort((a, b) => a.token.localeCompare(b.token));
  const presentSet = new Set(present.map(row => row.token));
  const consumers = [];
  const removed = [];
  for (const file of files) {
    for (const hit of scanText(file.path, file.text)) {
      const where = homes.get(hit.token);
      if (where) {
        if (presentSet.has(hit.token)) consumers.push(hit);
      } else if (isLegacyName(hit.token, {})) {
        removed.push(hit);
      }
    }
  }
  const bySite = (a, b) => a.token.localeCompare(b.token)
    || a.path.localeCompare(b.path)
    || a.line - b.line
    || a.index - b.index;
  consumers.sort(bySite);
  removed.sort(bySite);
  const counts = new Map();
  for (const hit of consumers) counts.set(hit.token, (counts.get(hit.token) ?? 0) + 1);
  for (const row of present) row.consumers = counts.get(row.token) ?? 0;
  const publish = hit => ({ token: hit.token, form: hit.form, path: hit.path, line: hit.line });
  return { present, consumers: consumers.map(publish), removed: removed.map(publish) };
}

function markdownTable(headers, rows) {
  const lines = [
    `| ${headers.join(' | ')} |`,
    `| ${headers.map(() => '---').join(' | ')} |`,
  ];
  if (rows.length === 0) lines.push('| (none) | | |');
  else for (const row of rows) lines.push(`| ${row.join(' | ')} |`);
  return lines.join('\n');
}

export function renderReport(report) {
  const removedNames = new Set(report.removed.map(row => row.token));
  const removedWord = removedNames.size === 1 ? 'key' : 'keys';
  const lines = [
    `legacy tokens: ${report.present.length} still defined, ${report.consumers.length} consumers, ${removedNames.size} removed ${removedWord} still referenced`,
    '',
    'still defined',
    '',
    markdownTable(
      ['token', 'home', 'consumers'],
      report.present.map(row => [row.token, row.home, String(row.consumers)]),
    ),
    '',
    'consumers',
    '',
    markdownTable(
      ['token', 'form', 'location'],
      report.consumers.map(row => [row.token, row.form, `${row.path}:${row.line}`]),
    ),
    '',
    'removed keys still referenced',
    '',
    markdownTable(
      ['token', 'form', 'location'],
      report.removed.map(row => [row.token, row.form, `${row.path}:${row.line}`]),
    ),
    '',
  ];
  return lines.join('\n');
}

export function readSourceFiles(roots, displayRoot) {
  const files = [];
  const walk = dir => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (SKIP_DIR.has(entry.name)) continue;
        walk(`${dir}/${entry.name}`);
        continue;
      }
      if (!SOURCE_EXT.has(extname(entry.name))) continue;
      const absolute = `${dir}/${entry.name}`;
      const path = displayRoot
        ? relative(displayRoot, absolute).split(sep).join('/')
        : absolute;
      files.push({ path, text: readFileSync(absolute, 'utf8') });
    }
  };
  for (const root of roots) {
    if (existsSync(root)) walk(root);
  }
  return files;
}

// Flags are whatever follows the script path. A test can pass a short argv
// of only flags; a real process argv includes node and the script.
export function parseArgs(argv) {
  const scriptAt = argv.findIndex(arg => arg.endsWith('legacy-tokens.mjs'));
  const args = scriptAt >= 0 ? argv.slice(scriptAt + 1) : argv.filter(arg => arg.startsWith('-'));
  return {
    strict: args.includes('--strict'),
    unknown: args.filter(arg => arg !== '--strict'),
  };
}

export function main(options = {}) {
  const stdout = options.stdout ?? process.stdout;
  const stderr = options.stderr ?? process.stderr;
  const { strict, unknown } = parseArgs(options.argv ?? process.argv);
  if (unknown.length > 0) {
    stderr.write(`unknown argument ${unknown.join(' ')}\n`);
    return 2;
  }
  let tokens = options.tokens;
  if (!tokens) {
    try {
      tokens = JSON.parse(readFileSync(options.tokensPath ?? tokensPath, 'utf8'));
    } catch (error) {
      stderr.write(`${error.message}\n`);
      return 2;
    }
  }
  const files = options.files ?? readSourceFiles(options.roots ?? defaultRoots, options.desktopRoot ?? desktopRoot);
  const report = reportLegacy({ tokens, files });
  stdout.write(renderReport(report));
  if (strict && report.removed.length > 0) {
    const count = new Set(report.removed.map(row => row.token)).size;
    const verb = count === 1 ? 'key is' : 'keys are';
    stderr.write(`strict: ${count} removed ${verb} still referenced\n`);
    return 1;
  }
  return 0;
}

function invokedDirectly() {
  const entry = process.argv[1];
  if (!entry) return false;
  try {
    return realpathSync(fileURLToPath(import.meta.url)) === realpathSync(entry);
  } catch {
    return false;
  }
}

if (invokedDirectly()) process.exit(main());
