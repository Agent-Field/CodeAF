import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';

// BE-SEC-03: the engine bearer token stays in renderer memory. The only legal
// spelling is the Authorization header. localStorage, sessionStorage, a URL
// and the console are sinks. This is a string law, the same shape as iconlaw:
// it reads the source, it does not execute it.
//
// Mutation: in desktop/src, add any one of
//   localStorage.setItem('t', connection.token)
//   sessionStorage.setItem('t', connection.token)
//   console.log(connection.token)
//   fetch(`${url}?token=${connection.token}`)
// tokenViolations then names the sink. The cases below fail if that detection
// is removed. A comment that mentions connection.token is not a use.

const bearer = /connection\.token|CODEAF_DESKTOP_TOKEN/g;
const sourceExt = new Set(['.ts', '.tsx', '.js', '.mjs', '.jsx']);

const sinks = [
  { name: 'storage', re: /\blocalStorage\b|\bsessionStorage\b/ },
  { name: 'console', re: /\bconsole\s*\.\s*(?:log|debug|info|warn|error|trace|dir)\b/ },
  { name: 'url', re: /\bURLSearchParams\b|\blocation\s*\.\s*(?:href|search|assign|replace)\b|\.searchParams\b|\bdocument\.cookie\b|[?&][a-zA-Z0-9_]*=\s*(?:\$\{(?:connection\.token|CODEAF_DESKTOP_TOKEN)\}|connection\.token|CODEAF_DESKTOP_TOKEN)/ },
];

function stripComments(source) {
  let out = '';
  let state = 'code';
  for (let i = 0; i < source.length; i++) {
    const c = source[i];
    const n = source[i + 1];
    if (state === 'line') {
      if (c === '\n') { state = 'code'; out += c; }
      continue;
    }
    if (state === 'block') {
      if (c === '*' && n === '/') { state = 'code'; i++; continue; }
      if (c === '\n') out += c;
      continue;
    }
    if (state === 'sq' || state === 'dq' || state === 'tpl') {
      out += c;
      if (c === '\\') { out += n ?? ''; i++; continue; }
      const end = state === 'sq' ? "'" : state === 'dq' ? '"' : '`';
      if (c === end) state = 'code';
      continue;
    }
    if (c === '/' && n === '/') { state = 'line'; i++; continue; }
    if (c === '/' && n === '*') { state = 'block'; i++; continue; }
    if (c === "'") state = 'sq';
    else if (c === '"') state = 'dq';
    else if (c === '`') state = 'tpl';
    out += c;
  }
  return out;
}

function balanced(text) {
  let paren = 0;
  let tick = 0;
  for (const c of text) {
    if (c === '(') paren++;
    else if (c === ')') paren--;
    else if (c === '`') tick++;
  }
  return paren === 0 && tick % 2 === 0;
}

function statementAt(source, index) {
  const lineStart = source.lastIndexOf('\n', index - 1) + 1;
  let end = source.indexOf('\n', index);
  if (end < 0) end = source.length;
  let start = lineStart;
  let slice = source.slice(start, end);
  while (!balanced(slice) && end < source.length) {
    const next = source.indexOf('\n', end + 1);
    end = next < 0 ? source.length : next;
    slice = source.slice(start, end);
  }
  while (!balanced(slice) && start > 0) {
    start = source.lastIndexOf('\n', start - 2) + 1;
    slice = source.slice(start, end);
  }
  return slice;
}

function allowedHeader(statement) {
  return /headers\.set\(\s*(['"])Authorization\1\s*,\s*`Bearer \$\{connection\.token\}`\s*\)/.test(statement);
}

function urlTemplate(statement) {
  const templates = statement.match(/`[^`]*`/g) ?? [];
  return templates.some(template =>
    /\$\{(?:connection\.token|CODEAF_DESKTOP_TOKEN)\}/.test(template) &&
    !/^`Bearer \$\{connection\.token\}`$/.test(template.trim()));
}

function consoleObject(statement) {
  // Logging the connection object prints the token. connection.url is the
  // loopback address, which is not the token.
  return /\bconsole\s*\.\s*(?:log|debug|info|warn|error|trace|dir)\s*\([^)]*\bconnection\b(?!\.url)/.test(statement);
}

export function tokenViolations(filename, source) {
  const stripped = stripComments(source);
  const found = [];
  const seen = new Set();
  for (const match of stripped.matchAll(bearer)) {
    const statement = statementAt(stripped, match.index);
    const key = `${filename}:${statement}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const names = [];
    if (!(allowedHeader(statement) && !sinks.some(sink => sink.name !== 'url' && sink.re.test(statement)) && !urlTemplate(statement))) {
      for (const sink of sinks) {
        if (sink.re.test(statement)) names.push(sink.name);
      }
      if (urlTemplate(statement)) names.push('url');
    }
    if (consoleObject(statement)) names.push('console');
    if (names.length > 0) found.push(`${filename}: ${[...new Set(names)].join(', ')}`);
  }
  // console.log(connection) prints the token without spelling connection.token.
  const objectLeak = /\bconsole\s*\.\s*(?:log|debug|info|warn|error|trace|dir)\s*\(([^)]*)\)/g;
  for (const match of stripped.matchAll(objectLeak)) {
    if (/\bconnection\b(?!\.url)/.test(match[1]) || /CODEAF_DESKTOP_TOKEN/.test(match[1])) {
      found.push(`${filename}: console`);
    }
  }
  return found;
}

function walk(dir) {
  const found = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      found.push(...walk(path));
      continue;
    }
    if (!sourceExt.has(extname(name))) continue;
    found.push(...tokenViolations(path, readFileSync(path, 'utf8')));
  }
  return found;
}

const src = fileURLToPath(new URL('../src', import.meta.url));

test('desktop/src never puts the engine token in storage, a URL or the console', () => {
  assert.deepEqual(walk(src), []);
});

test('a token written to storage, a URL or the console is rejected', () => {
  const bad = [
    ["localStorage.setItem('codeaf.token', connection.token);", /storage/],
    ["sessionStorage.setItem('codeaf.token', connection.token);", /storage/],
    ['console.log(connection.token);', /console/],
    ['console.warn("auth", connection.token);', /console/],
    ['console.log(connection);', /console/],
    ['const url = `${base}?token=${connection.token}`;', /url/],
    ['fetch(`/api/engine/health?access_token=${CODEAF_DESKTOP_TOKEN}`);', /url/],
    ['const params = new URLSearchParams({ token: connection.token });', /url/],
    ["location.href = base + '?token=' + connection.token;", /url/],
    ["document.cookie = 'token=' + connection.token;", /url/],
  ];
  for (const [source, pattern] of bad) {
    const found = tokenViolations('src/leak.ts', source);
    assert.ok(found.length > 0, source);
    assert.match(found.join('\n'), pattern, source);
  }
});

test('the authorization header and unrelated storage stay legal', () => {
  const ok = [
    "headers.set('Authorization', `Bearer ${connection.token}`);",
    'headers.set("Authorization", `Bearer ${connection.token}`);',
    'const kept = connection.token;',
    "localStorage.setItem('codeaf-theme', theme);",
    'sessionStorage.setItem(tokenKey, token);',
    'const base = new URL(connection.url);',
    '// connection.token stays in memory and is never stored',
    '/* console.log(connection.token) */ const n = 1;',
  ];
  for (const source of ok) {
    assert.deepEqual(tokenViolations('src/ok.ts', source), [], source);
  }
});
