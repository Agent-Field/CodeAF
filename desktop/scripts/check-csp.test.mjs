import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// BE-SEC-08: arch-decisions “Security” and code-bridge §3 require NOTHING new
// in the app CSP for web tabs or multiwindow. Web tabs are native views, not
// iframes. The integrator must rewrite forwarded http://localhost: connections to
// http://127.0.0.1: in Rust's forwarded_connection() before caching them;
// widening connect-src to accommodate localhost would break this contract.
const allowed = {
  'default-src': ["'self'"],
  'style-src': ["'self'", "'unsafe-inline'"],
  'img-src': ["'self'", 'asset:', 'data:', 'blob:'],
  'media-src': ["'self'", 'data:'],
  'connect-src': ['ipc:', 'http://ipc.localhost', 'http://127.0.0.1:*'],
  'frame-src': ["'self'"],
};

function checkContract(csp) {
  assert.equal(typeof csp, 'string', 'app.security.csp must remain an enabled string policy');
  const directives = new Map();
  for (const directive of csp.split(';')) {
    const [name, ...sources] = directive.trim().split(/\s+/u);
    if (!name) continue;
    assert.ok(!directives.has(name), `Duplicate CSP directive: ${name}`);
    assert.ok(Object.hasOwn(allowed, name), `Unreviewed CSP directive: ${name}`);
    assert.deepEqual([...sources].sort(), [...allowed[name]].sort(),
      `${name} must retain its exact local source allow-list`);
    directives.set(name, sources);
  }
  // With no frame-src, default-src already restricts frames to self. Explicit
  // self is equivalent; every other directive remains mandatory.
  for (const name of Object.keys(allowed)) {
    if (name === 'frame-src') continue;
    assert.ok(directives.has(name), `Missing CSP directive: ${name}`);
  }
}

const baseline = Object.entries(allowed).filter(([name]) => name !== 'frame-src')
  .map(([name, sources]) => `${name} ${sources.join(' ')}`).join('; ');

test('HEAD app.security.csp retains the exact native local-only contract', () => {
  const config = JSON.parse(readFileSync(new URL('../src-tauri/tauri.conf.json', import.meta.url), 'utf8'));
  checkContract(config.app?.security?.csp);
});

test('permits equivalent ordering, whitespace and explicit self-only frames', () => {
  checkContract(baseline);
  checkContract(`${baseline}; frame-src 'self'`);
  checkContract(Object.entries(allowed).reverse()
    .map(([name, sources]) => ` ${name}\t${[...sources].reverse().join('  ')} `).join(';') + ';');
});

test('rejects remote origins and every extra source in every permitted directive', async t => {
  for (const name of Object.keys(allowed)) {
    for (const source of ['https://example.com', 'http://example.com', 'https:', 'http:',
      '*', "'unsafe-eval'", "'unsafe-inline'", 'http://localhost:*', 'ws://127.0.0.1:*']) {
      if (allowed[name].includes(source)) continue;
      await t.test(`${name} ${source}`, () => {
        const policy = Object.entries(allowed).map(([directive, sources]) =>
          `${directive} ${[...sources, ...(directive === name ? [source] : [])].join(' ')}`).join('; ');
        assert.throws(() => checkContract(policy), /exact local source allow-list/u);
      });
    }
  }
});

test('rejects new directives even when they have no remote origin', () => {
  for (const directive of ["script-src 'self' 'unsafe-eval'", "script-src 'self'",
    'frame-ancestors *', 'form-action https://example.com', 'sandbox allow-scripts']) {
    assert.throws(() => checkContract(`${baseline}; ${directive}`), /Unreviewed CSP directive/u);
  }
});

test('rejects disabled, duplicate and incomplete policies including missing favicon data support', () => {
  for (const csp of [undefined, null, false, {}, { 'default-src': ["'self'"] }]) {
    assert.throws(() => checkContract(csp), /enabled string policy/u);
  }
  assert.throws(() => checkContract(''), /Missing CSP directive/u);
  assert.throws(() => checkContract(`${baseline}; connect-src https://example.com`), /Duplicate CSP directive/u);
  for (const [name, sources] of Object.entries(allowed)) {
    if (name === 'frame-src') continue;
    const withoutDirective = baseline.split('; ').filter(value => !value.startsWith(`${name} `)).join('; ');
    assert.throws(() => checkContract(withoutDirective), /Missing CSP directive/u);
    for (const source of sources) {
      const policy = baseline.replace(`${name} ${sources.join(' ')}`,
        `${name} ${sources.filter(value => value !== source).join(' ')}`);
      assert.throws(() => checkContract(policy), /exact local source allow-list/u);
    }
  }
});
