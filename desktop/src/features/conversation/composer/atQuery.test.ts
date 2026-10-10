import assert from 'node:assert/strict';
import { test } from 'node:test';
import { activeAt, applyAtPath, rankAtFiles, type AtFile } from './atQuery.ts';

const file = (path: string, name = path.slice(path.lastIndexOf('/') + 1), dir = path.slice(0, path.lastIndexOf('/'))): AtFile => ({ path, name, dir });

test('an @ at the start of the field, or after whitespace, is the token and the query stops at the caret', () => {
  assert.deepEqual(activeAt('@src/fo', 7), { at: 0, end: 7, query: 'src/fo' });
  assert.deepEqual(activeAt('see @src/fo', 11), { at: 4, end: 11, query: 'src/fo' });
  assert.deepEqual(activeAt('see\n@src/fo', 11), { at: 4, end: 11, query: 'src/fo' });
  const tabbed = 'see\t@src';
  assert.deepEqual(activeAt(tabbed, tabbed.length), { at: 4, end: tabbed.length, query: 'src' });
  const partial = 'see @src/foo';
  assert.deepEqual(activeAt(partial, 6), { at: 4, end: partial.length, query: 's' });
  assert.deepEqual(activeAt('see @', 5), { at: 4, end: 5, query: '' });
});

test('a caret outside the token, or an @ that does not begin a word, is not a query', () => {
  assert.equal(activeAt('see @src next', 12), null);
  assert.equal(activeAt('see @src next', 4), null);
  assert.equal(activeAt('@src', 0), null);
  assert.equal(activeAt('(@src)', 5), null);
  assert.equal(activeAt('plain words', 5), null);
  assert.equal(activeAt('@src', -1), null);
  assert.equal(activeAt('@src', 1.5), null);
});

test('an email, or any @ inside a word, does not open a query', () => {
  assert.equal(activeAt('user@example.com', 10), null);
  assert.equal(activeAt('mail me at user@example.com today', 22), null);
  assert.equal(activeAt('user@example.com', 5), null);
  assert.equal(activeAt('pkg.Foo@v1.2.3', 12), null);
  assert.equal(applyAtPath('mail me at user@example.com today', 22, 'src/foo.go'), null);
});

test('a path that itself contains @ still belongs to the @ that begins the word', () => {
  const scoped = 'use @packages/@scope/name';
  assert.deepEqual(activeAt(scoped, scoped.length), { at: 4, end: scoped.length, query: 'packages/@scope/name' });
});

test('the chosen path replaces the whole token and the caret sits just after it', () => {
  // The caret is inside `@src/foo.go` (after "sr"); the unread suffix is the same token.
  const mid = applyAtPath('see @src/foo.go next', 7, 'src/foo.go');
  assert.deepEqual(mid, { text: 'see src/foo.go next', caret: 14 });
  assert.equal(mid?.text[mid.caret], ' ');

  const end = applyAtPath('see @src/fo', 11, 'src/foo.go');
  assert.deepEqual(end, { text: 'see src/foo.go', caret: 14 });

  const line = applyAtPath('look\n@lex', 9, 'internal/lexer.go');
  assert.deepEqual(line, { text: 'look\ninternal/lexer.go', caret: 5 + 'internal/lexer.go'.length });

  const accent = applyAtPath('@caf', 4, 'notes/café.md');
  assert.deepEqual(accent, { text: 'notes/café.md', caret: 'notes/café.md'.length });
});

test('name-prefix rows lead and the engine order holds inside each group', () => {
  const files = [
    file('internal/lex/parse.go'),
    file('zebra.go'),
    file('lexer.go'),
    file('cmd/lex.go'),
    file('Lexicon.ts'),
  ];
  assert.deepEqual(
    rankAtFiles(files, 'lex').map(row => row.path),
    ['lexer.go', 'cmd/lex.go', 'Lexicon.ts', 'internal/lex/parse.go', 'zebra.go'],
  );
  assert.deepEqual(rankAtFiles(files, '').map(row => row.path), files.map(row => row.path));
  assert.deepEqual(rankAtFiles([], 'lex'), []);
  assert.deepEqual(rankAtFiles(files, 'lex'), rankAtFiles(files, 'Lex'));
});
