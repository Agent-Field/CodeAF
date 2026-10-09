import test from 'node:test';
import assert from 'node:assert/strict';
import { fileEventTouches, sameWorkspaceFile } from './fileRefresh.ts';

test('an edit of this file matches a relative or absolute path', () => {
  const args = JSON.stringify({ path: 'internal/auth/auth_test.go' });
  assert.equal(fileEventTouches('edit', args, 'internal/auth/auth_test.go'), true);
  assert.equal(fileEventTouches('write', JSON.stringify({ file_path: '/work/internal/auth/auth_test.go' }), 'internal/auth/auth_test.go'), true);
  assert.equal(fileEventTouches('edit', args, 'other.go'), false);
  assert.equal(fileEventTouches('bash', JSON.stringify({ command: 'rm -rf /' }), 'internal/auth/auth_test.go'), false);
  assert.equal(fileEventTouches('edit', 'not json', 'internal/auth/auth_test.go'), false);
});

test('same file ignores a trailing slash and a leading dot', () => {
  assert.equal(sameWorkspaceFile('./a.go/', 'a.go'), true);
  assert.equal(sameWorkspaceFile('dir/a.go', 'a.go.bak'), false);
});

test('a relative edit elsewhere in the tree is not this file', () => {
  assert.equal(sameWorkspaceFile('cmd/a.go', 'a.go'), false);
  assert.equal(sameWorkspaceFile('a.go', 'cmd/a.go'), false);
  assert.equal(sameWorkspaceFile('/work/cmd/a.go', 'cmd/a.go'), true);
  assert.equal(sameWorkspaceFile('cmd/a.go', '/work/cmd/a.go'), true);
});
