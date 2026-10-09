import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createStatCache } from './stat-cache.ts';
import { aspectOf, fileKind, looksLikePath, localPathFromHref, middleTruncate, monogramHue, registrableDomain, relativePath, splitPath } from './paths.ts';

test('paths split, classify and relativise', () => {
  assert.deepEqual(splitPath('src/app/main.go'), { name: 'main.go', dir: 'src/app' });
  assert.deepEqual(splitPath('README.md'), { name: 'README.md', dir: '' });
  assert.equal(fileKind('a.PNG'), 'image');
  assert.equal(fileKind('x.go'), 'code');
  assert.equal(fileKind('src', true), 'folder');
  assert.equal(relativePath('/w/proj/a/b.ts', '/w/proj'), 'a/b.ts');
  assert.equal(relativePath('/etc/passwd', '/w/proj'), null);
  assert.equal(relativePath('../x', '/w/proj'), null);
  assert.equal(relativePath('./a.ts', '/w/proj'), 'a.ts');
});

test('middle truncation keeps both ends', () => {
  const out = middleTruncate('internal/session/prompts/deep/nested', 16);
  assert.equal(out.length, 16);
  assert.ok(out.startsWith('internal') && out.endsWith('nested'));
});

test('domains and monogram hues are stable and bounded', () => {
  assert.equal(registrableDomain('docs.example.co.uk'), 'example.co.uk');
  assert.equal(registrableDomain('a.b.github.com'), 'github.com');
  const hue = monogramHue('github.com');
  assert.equal(hue, monogramHue('github.com'));
  for (const d of ['a.com', 'b.org', 'zed.io', 'react.dev']) assert.ok(monogramHue(d) >= 1 && monogramHue(d) <= 6);
});

test('only path-shaped text is worth a stat', () => {
  assert.ok(looksLikePath('src/main.go'));
  assert.ok(looksLikePath('README.md'));
  assert.ok(!looksLikePath('useState'));
  assert.ok(!looksLikePath('obj.method'));
  assert.ok(!looksLikePath('npm run check'));
  assert.ok(!looksLikePath('https://x.dev/a/b'));
  assert.equal(localPathFromHref('src/a%20b.ts#L4'), 'src/a b.ts');
  assert.equal(localPathFromHref('https://x.dev'), null);
  assert.equal(localPathFromHref('#top'), null);
  assert.equal(aspectOf('generated · 1024×512'), 2);
});

test('stat cache batches a tick, caps at 64 and remembers', async () => {
  const calls: string[][] = [];
  const cache = createStatCache(async paths => {
    calls.push(paths);
    return paths.map(path => ({ path, exists: true, dir: false, size: 1 }));
  }, 1);
  const many = Array.from({ length: 70 }, (_, i) => `f${i}.ts`);
  const facts = await cache.stat(many);
  assert.equal(facts.length, 70);
  assert.deepEqual(calls.map(c => c.length), [64, 6]);
  await cache.stat(['f1.ts']);
  assert.equal(calls.length, 2);
  assert.equal(cache.peek('f2.ts')?.exists, true);
});
