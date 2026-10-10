import test from 'node:test';
import assert from 'node:assert/strict';
import { fileIcon, filePaneIcon } from './fileIcon.ts';

test('maps code, json, text and image', () => {
  assert.equal(fileIcon('src/lexer.go'), 'fileCode2');
  assert.equal(fileIcon('app.TS'), 'fileCode2');
  assert.equal(fileIcon('main.py'), 'fileCode2');

  assert.equal(fileIcon('x.json'), 'fileJson');
  assert.equal(fileIcon('dir/tsconfig.jsonc'), 'fileJson');
  assert.equal(fileIcon('data.JSON5'), 'fileJson');

  assert.equal(fileIcon('README.md'), 'file');
  assert.equal(fileIcon('notes.TXT'), 'file');
  assert.equal(fileIcon('Makefile'), 'file');

  assert.equal(fileIcon('logo.png'), 'image');
  assert.equal(fileIcon('photo.jpg'), 'image');
  assert.equal(fileIcon('photo.jpeg'), 'image');
  assert.equal(fileIcon('anim.gif'), 'image');
  assert.equal(fileIcon('shot.webp'), 'image');
  assert.equal(fileIcon('mark.svg'), 'image');
  assert.equal(fileIcon('pic.avif'), 'image');
  assert.equal(fileIcon('icon.bmp'), 'image');
  assert.equal(fileIcon('fav.ico'), 'image');

  // Q34: pdf, audio and anything else have no file-tab glyph, so they draw the plain file icon.
  assert.equal(fileIcon('doc.pdf'), 'file');
  assert.equal(fileIcon('clip.mp3'), 'file');
  assert.equal(fileIcon('clip.mp4'), 'file');
});

test('a renamed file tab keeps the icon of its path', () => {
  assert.equal(filePaneIcon({ title: 'notes', file: { path: 'x.json' } }), 'fileJson');
  assert.equal(filePaneIcon({ title: 'logo.png', path: 'src/app.go' }), 'fileCode2');
  assert.equal(filePaneIcon({ title: 'README.md' }), 'file');
});
