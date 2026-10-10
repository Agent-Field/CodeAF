import test from 'node:test';
import assert from 'node:assert/strict';
import { imageVerdict, imageVerdictOf, isImagePath, maxImageBytes, settleImage } from './imageFile.ts';

test('image kinds include SVG; html and code are not', () => {
  for (const path of ['a/b.png', 'x.JPG', 'x.jpeg', 'x.gif', 'x.webp', 'x.avif', 'x.bmp', 'x.ico', 'x.svg']) assert.equal(isImagePath(path), true, path);
  for (const path of ['x.html', 'x.go', 'x.png.txt', 'png']) assert.equal(isImagePath(path), false, path);
});

test('a small raster answer becomes a data url with the normalised mime', () => {
  assert.deepEqual(imageVerdict({ mime: 'IMAGE/PNG; charset=binary', size: 4, dataBase64: 'AAAA' }), { ok: true, url: 'data:image/png;base64,AAAA' });
});

test('html and unknown mimes are refused as unsafe', () => {
  for (const mime of ['text/html', 'application/octet-stream', 'image/png,<script>']) {
    assert.deepEqual(imageVerdict({ mime, size: 4, dataBase64: 'AAAA' }), { ok: false, reason: 'unsafe' }, mime);
  }
});

test('data that is not base64 is refused so it cannot break out of the url', () => {
  assert.deepEqual(imageVerdict({ mime: 'image/png', size: 4, dataBase64: 'AA"onerror=x' }), { ok: false, reason: 'unsafe' });
});

test('the bound is the engine read cap, 16 MiB, and is inclusive; the payload is checked too', () => {
  assert.equal(maxImageBytes, 16 << 20);
  assert.equal(imageVerdict({ mime: 'image/png', size: maxImageBytes, dataBase64: 'AAAA' }).ok, true);
  assert.deepEqual(imageVerdict({ mime: 'image/png', size: maxImageBytes + 1, dataBase64: 'AAAA' }), { ok: false, reason: 'too-large' });
  assert.deepEqual(imageVerdict({ mime: 'image/png', size: 1, dataBase64: 'A'.repeat(Math.ceil(maxImageBytes / 3) * 4 + 4) }), { ok: false, reason: 'too-large' });
  assert.equal(imageVerdict({ mime: 'image/png', size: 8 * 1024 * 1024 + 1, dataBase64: 'AAAA' }).ok, true, 'the old 8 MiB frontend limit is gone');
});

test('a size that is not a finite non-negative integer is refused, never defaulted', () => {
  for (const size of [NaN, Infinity, -1, 1.5, undefined as unknown as number, '4' as unknown as number]) {
    assert.deepEqual(imageVerdict({ mime: 'image/png', size, dataBase64: 'AAAA' }), { ok: false, reason: 'unsafe' }, String(size));
  }
});

test('only canonical base64 is accepted', () => {
  for (const data of ['AAA', 'AA=A', 'A===', 'AAAA=', 'AA AA', 'AAAA\n']) assert.equal(imageVerdict({ mime: 'image/png', size: 4, dataBase64: data }).ok, false, JSON.stringify(data));
  for (const data of ['', 'AAAA', 'AAA=', 'AA==', 'AAAAAAA=']) assert.equal(imageVerdict({ mime: 'image/png', size: 4, dataBase64: data }).ok, true, JSON.stringify(data));
});

test('the verdict is computed once per engine answer', () => {
  const file = { mime: 'image/png', size: 4, dataBase64: 'AAAA' };
  assert.equal(imageVerdictOf(file), imageVerdictOf(file));
  assert.notEqual(imageVerdictOf(file), imageVerdictOf({ ...file }));
});

test('an answer read for another session or path, or with no key, is loading', () => {
  const ready = (key: string) => ({ status: 'ready' as const, value: { key } });
  assert.deepEqual(settleImage('s1\0a.png', ready('s1\0a.png')), ready('s1\0a.png'));
  assert.deepEqual(settleImage('s2\0a.png', ready('s1\0a.png')), { status: 'loading' });
  assert.deepEqual(settleImage('s1\0b.png', ready('s1\0a.png')), { status: 'loading' });
  assert.deepEqual(settleImage(null, ready('s1\0a.png')), { status: 'loading' });
  assert.deepEqual(settleImage('s1\0a.png', { status: 'loading' }), { status: 'loading' });
  assert.deepEqual(settleImage('s1\0a.png', { status: 'failed', message: 'no' }), { status: 'failed', message: 'no' });
});

test('SVG is accepted for a restricted image document', () => {
  assert.equal(imageVerdict({ mime: 'image/svg+xml', size: 4, dataBase64: 'AAAA' }).ok, true);
});

test('a real cap-sized payload validates without overflowing the regexp stack', () => {
  const payload = 'A'.repeat(Math.ceil(maxImageBytes / 3) * 4 - 2) + '==';
  assert.equal(imageVerdict({ mime: 'image/png', size: maxImageBytes, dataBase64: payload }).ok, true);
  assert.deepEqual(imageVerdict({ mime: 'image/png', size: 1, dataBase64: payload.slice(0, -2) + 'AA' }), { ok: false, reason: 'too-large' });
});
