import assert from 'node:assert/strict';
import { test } from 'node:test';
import { admit, formatSize, toOutgoing, type Attachment } from './attachments.ts';

const MB = 1024 * 1024;
const file = (name: string, size: number, type = 'text/plain') => new File([new ArrayBuffer(size)], name, { type });
const held = (f: File): Attachment => ({ id: f.name, file: f, picture: f.type.startsWith('image/') });

test('a picture over 10 MB is refused by name', () => {
  const { accepted, error } = admit([], [file('big.png', 11 * MB, 'image/png')]);
  assert.equal(accepted.length, 0);
  assert.ok(error?.includes('big.png'));
});

test('a non-picture over 10 MB is fine while the total fits', () => {
  assert.equal(admit([], [file('data.csv', 15 * MB)]).error, undefined);
});

test('the total is 20 MB, counting what is already attached', () => {
  const existing = [held(file('a.bin', 15 * MB))];
  const { accepted, error } = admit(existing, [file('b.bin', 4 * MB), file('c.bin', 2 * MB)]);
  assert.equal(accepted.length, 1);
  assert.ok(error?.includes('c.bin'));
});

test('sizes read plainly', () => {
  assert.equal(formatSize(512), '512 B');
  assert.equal(formatSize(2048), '2 KB');
  assert.equal(formatSize(3 * MB), '3.0 MB');
});

test('encoding happens at send time with the original name and type', async () => {
  const out = await toOutgoing([held(new File(['hi'], 'note.txt', { type: 'text/plain' }))]);
  assert.deepEqual(out, [{ name: 'note.txt', mime: 'text/plain', dataBase64: 'aGk=' }]);
});
