// Source law: the identity Durable Object is request-scoped. Cloudflare bills a Durable Object for
// as long as it holds a timer or an outbound connection, and either one defeats WebSocket
// hibernation, so nothing the identity object imports may open one.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const SRC = join(dirname(fileURLToPath(import.meta.url)), '..', 'src');
// Other Durable Object classes: the identity object reaches them only by stub RPC, so their
// sources are not part of its request scope.
const OTHER_OBJECTS = (file) => file.startsWith('pair/') || file === 'newcomers.js';
const FLIGHT = 'flight.js';

// stripNoise blanks comments and string literals so that words inside them never match. A
// regex literal that holds a quote character would confuse it; none exists in this source.
export const stripNoise = (source) =>
  source.replace(/\/\*[\s\S]*?\*\/|\/\/[^\n]*|'(?:\\.|[^'\\\n])*'|"(?:\\.|[^"\\\n])*"|`(?:\\.|[^`\\])*`/g, ' ');

const count = (code, pattern) => (code.match(pattern) || []).length;

// violations lists every way one file breaks the law; flight.js alone may hold a single
// setTimeout, and only when a clearTimeout pairs with it.
export function violations(file, source) {
  const code = stripNoise(source);
  const found = [];
  const flag = (token, n) => n > 0 && found.push(`${file}: forbidden ${token} (x${n})`);
  const timers = file === FLIGHT ? 1 : 0;
  flag('setInterval', count(code, /\bsetInterval\b/g));
  flag('setImmediate', count(code, /\bsetImmediate\b/g));
  flag('setTimeout', Math.max(0, count(code, /\bsetTimeout\b/g) - timers));
  if (file === FLIGHT && count(code, /\bclearTimeout\b/g) !== 1) found.push(`${file}: setTimeout needs exactly one clearTimeout`);
  flag('fetch call', count(code, /(?:await\s+|return\s+|[=(!,.]\s*)fetch\s*\(/g));
  flag('connect call', count(code, /\bconnect\s*\(/g));
  flag('new WebSocket', count(code, /new\s+WebSocket\b/g));
  if (file !== 'watch.js') flag('WebSocketPair', count(code, /\bWebSocketPair\b/g));
  return found;
}

// importsOf returns the relative modules one source file imports, as paths under src.
const importsOf = (file, source) =>
  [...stripImports(source).matchAll(/from\s+'(\.[^']+)'/g)].map((m) => join(dirname(file), m[1]));
const stripImports = (source) => source.replace(/\/\*[\s\S]*?\*\/|\/\/[^\n]*/g, ' ');

// requestScope walks the import graph from the identity object and returns the files it owns.
export function requestScope(start = 'identity.js') {
  const seen = new Set();
  const visit = (file) => {
    if (seen.has(file) || OTHER_OBJECTS(file)) return;
    seen.add(file);
    importsOf(file, readFileSync(join(SRC, file), 'utf8')).forEach(visit);
  };
  visit(start);
  return [...seen].sort();
}

test('identity object imports no timer and opens no outbound connection', () => {
  const files = requestScope();
  assert.ok(files.length >= 8, `scope found only ${files.length} files: ${files}`);
  assert.ok(files.includes(FLIGHT) && files.includes('tenant.js'), `scope misses a known file: ${files}`);
  const all = files.flatMap((file) => violations(file, readFileSync(join(SRC, file), 'utf8')));
  assert.deepEqual(all, []);
  const flight = stripNoise(readFileSync(join(SRC, FLIGHT), 'utf8'));
  assert.equal(count(flight, /\bsetTimeout\b/g), 1, 'flight.js holds exactly one setTimeout');
});

test('the law reports a stray interval and an outbound fetch', () => {
  assert.match(violations('x.js', 'setInterval(() => {}, 1);').join(), /x\.js: forbidden setInterval/);
  assert.match(violations('x.js', 'const r = await fetch(url);').join(), /x\.js: forbidden fetch call/);
  assert.deepEqual(violations('x.js', '// setInterval and "fetch(" are only words\nclass A { fetch(request) {} }'), []);
});
