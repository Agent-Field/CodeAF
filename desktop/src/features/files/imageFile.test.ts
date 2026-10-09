import test from 'node:test';
import assert from 'node:assert/strict';
import { imageVerdict, isRasterImagePath, maxImageBytes } from './imageFile.ts';

test('raster extensions are pictures; svg, html and code are not', () => {
  for (const path of ['a/b.png', 'x.JPG', 'x.jpeg', 'x.gif', 'x.webp', 'x.avif', 'x.bmp', 'x.ico']) assert.equal(isRasterImagePath(path), true, path);
  for (const path of ['x.svg', 'x.html', 'x.go', 'x.png.txt', 'png']) assert.equal(isRasterImagePath(path), false, path);
});

test('a small raster answer becomes a data url with the normalised mime', () => {
  assert.deepEqual(imageVerdict({ mime: 'IMAGE/PNG; charset=binary', size: 4, dataBase64: 'AAAA' }), { ok: true, url: 'data:image/png;base64,AAAA' });
});

test('svg, html and unknown mimes are refused as unsafe', () => {
  for (const mime of ['image/svg+xml', 'text/html', 'application/octet-stream', 'image/png,<script>']) {
    assert.deepEqual(imageVerdict({ mime, size: 4, dataBase64: 'AAAA' }), { ok: false, reason: 'unsafe' }, mime);
  }
});

test('data that is not base64 is refused so it cannot break out of the url', () => {
  assert.deepEqual(imageVerdict({ mime: 'image/png', size: 4, dataBase64: 'AA"onerror=x' }), { ok: false, reason: 'unsafe' });
});

test('the size bound is inclusive and also checks the payload, not only the claimed size', () => {
  assert.equal(imageVerdict({ mime: 'image/png', size: maxImageBytes, dataBase64: 'AAAA' }).ok, true);
  assert.deepEqual(imageVerdict({ mime: 'image/png', size: maxImageBytes + 1, dataBase64: 'AAAA' }), { ok: false, reason: 'too-large' });
  assert.deepEqual(imageVerdict({ mime: 'image/png', size: 1, dataBase64: 'A'.repeat(Math.ceil(maxImageBytes / 3) * 4 + 4) }), { ok: false, reason: 'too-large' });
});
