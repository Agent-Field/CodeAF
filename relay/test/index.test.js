// index.test.js checks the document's shape, its key order and the signature.

import test from 'node:test';
import assert from 'node:assert/strict';

import { buildIndex, signIndex } from '../src/index.js';

const CELLS = [
  {
    metric: 'role_quality', role: 'worker', model: 'z-ai/glm-5.3-flash',
    mean: 65.6, sd: 5.9, n: 62, installs: 4,
  },
];

test('buildIndex carries an integer version and keys in the fixed order', () => {
  const text = buildIndex(CELLS, {
    version: 1758117600, generated: '2026-09-17', minInstalls: 3, judges: ['j/x'],
  });
  assert.doesNotMatch(text, /\n/);
  const doc = JSON.parse(text);
  assert.equal(doc.schema, 1);
  assert.equal(doc.version, 1758117600);
  assert.equal(Number.isInteger(doc.version), true);
  assert.equal(doc.generated, '2026-09-17');
  assert.equal(doc.min_installs, 3);
  assert.deepEqual(doc.judges, ['j/x']);
  assert.deepEqual(doc.rubrics, { role_quality: 1, acceptable: 1 });
  assert.deepEqual(doc.metrics.acceptable, { kind: 'bernoulli', unit: 'share', dims: ['role', 'model', 'source'] });
  assert.deepEqual(doc.aliases, {});
  assert.deepEqual(Object.keys(doc.metrics.role_quality), ['kind', 'unit', 'dims']);
  assert.deepEqual(Object.keys(doc), [
    'schema', 'version', 'generated', 'min_installs', 'judges',
    'rubrics', 'aliases', 'metrics', 'cells',
  ]);
  assert.deepEqual(Object.keys(doc.cells[0]), ['metric', 'role', 'model', 'mean', 'sd', 'n', 'installs']);
});

test('buildIndex with no cells still renders a document with an empty cells list', () => {
  const doc = JSON.parse(buildIndex([], {
    version: 1, generated: '2026-09-17', minInstalls: 3, judges: [],
  }));
  assert.deepEqual(doc.cells, []);
});

test('signIndex round-trips under the public key through crypto.subtle.verify', async () => {
  const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']);
  const priv = await crypto.subtle.exportKey('jwk', pair.privateKey);
  const pub = await crypto.subtle.exportKey('jwk', pair.publicKey);
  const seedB64 = fromBase64Url(priv.d);
  const publicB64 = fromBase64Url(pub.x);

  const doc = buildIndex(CELLS, {
    version: 7, generated: '2026-09-17', minInstalls: 3, judges: ['j/x'],
  });
  const sigB64 = await signIndex(doc, seedB64, publicB64);

  const key = await crypto.subtle.importKey(
    'jwk', { kty: 'OKP', crv: 'Ed25519', x: pub.x }, { name: 'Ed25519' }, false, ['verify'],
  );
  const sig = Uint8Array.from(atob(sigB64), (c) => c.charCodeAt(0));
  const ok = await crypto.subtle.verify(
    { name: 'Ed25519' }, key, sig, new TextEncoder().encode(doc),
  );
  assert.equal(ok, true);

  // The same document under a different byte does not verify.
  const bad = await crypto.subtle.verify(
    { name: 'Ed25519' }, key, sig, new TextEncoder().encode(doc + ' '),
  );
  assert.equal(bad, false);
});

// fromBase64Url reassembles standard base64 from a JWK's base64url field.
function fromBase64Url(text) {
  const base = text.replace(/-/g, '+').replace(/_/g, '/');
  return base + '='.repeat((4 - (base.length % 4)) % 4);
}

test('buildIndex writes an acceptable cell with its source label, vendor taken off', () => {
  const doc = JSON.parse(buildIndex([
    {
      metric: 'acceptable', role: 'worker', model: 'z-ai/glm-5.3-flash', source: 'codeaf/grader',
      mean: 80, sd: 40, n: 5, installs: 1,
    },
  ], { version: 1, generated: '2026-09-17', minInstalls: 3, judges: ['codeaf/grader'] }));
  assert.deepEqual(Object.keys(doc.cells[0]), ['metric', 'role', 'model', 'source', 'mean', 'sd', 'n', 'installs']);
  assert.equal(doc.cells[0].source, 'grader');
});
