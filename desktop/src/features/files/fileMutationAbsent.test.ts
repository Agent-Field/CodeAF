import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// Shell 3e draws Changes, File, and Open in. A save, rename, or delete on
// these files would be a control the design does not draw.
const drawn = [
  'FileHeader.tsx',
  'FileLines.tsx',
  'FileSurface.tsx',
  'EditorHandoff.tsx',
  'DiffBody.tsx',
  'OpenFile.tsx',
  'ImageView.tsx',
];
const refused = ['Save', 'Rename', 'Delete', 'contentEditable', 'files/write', 'files/rename', 'files/delete', 'files/move'];

test('the file tab draws no write, rename, or delete', () => {
  const dir = dirname(fileURLToPath(import.meta.url));
  for (const name of drawn) {
    const text = readFileSync(join(dir, name), 'utf8');
    for (const word of refused) {
      assert.equal(text.includes(word), false, `${name} contains ${word}`);
    }
  }
});
