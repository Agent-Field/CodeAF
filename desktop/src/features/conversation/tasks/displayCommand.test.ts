// @ts-nocheck -- the app tsconfig has no node types; this file runs under node --test.
import test from 'node:test';
import assert from 'node:assert/strict';
import { cutParts, displayCommand, firstLine } from './displayCommand.ts';

const command = 'cd /run/copy && go test ./... && plandb record x';
const cd = { Command: 'cd /run/copy', Separator: '&&', Start: 0, End: 12, SepEnd: 15, RunCopyPrefix: true };
const test_ = { Command: 'go test ./...', Separator: '&&', Start: 16, End: 29, SepEnd: 32 };
const record = { Command: 'plandb record x', Start: 33, End: 48, RecordAddressed: true };

test('drops the leading run-copy cd and the record-addressed shim', () => {
  assert.equal(displayCommand(command, [cd, test_, record]), 'go test ./...');
});

test('keeps separators between the parts that stay', () => {
  const build = { Command: 'make build', Start: 33, End: 43 };
  assert.equal(cutParts('cd /run/copy && go test ./... && make build', [cd, test_, build]), 'go test ./... && make build');
});

test('uses the part text when offsets are missing or out of range', () => {
  const noOffsets = [{ Command: 'cd x', RunCopyPrefix: true, Separator: '&&' }, { Command: 'ls -la' }];
  assert.equal(cutParts('whatever', noOffsets), 'ls -la');
  assert.equal(cutParts('short', [{ Command: 'ls', Start: 0, End: 99 }, { RecordAddressed: true, Command: 'z' }]), 'ls');
});

test('with nothing to cut, or everything cut, the command stays whole', () => {
  assert.equal(displayCommand('go build', []), 'go build');
  assert.equal(displayCommand('go build', undefined), 'go build');
  assert.equal(displayCommand('go build', [{ Command: 'go build', RecordAddressed: true, Start: 0, End: 8 }]), 'go build');
  assert.equal(displayCommand('go build', [{ Command: 'go build', Start: 0, End: 8 }]), 'go build');
});

test('shows the first line and an ellipsis when more follows', () => {
  assert.equal(firstLine('sed -n 1,40p x.go\ncat y\n'), 'sed -n 1,40p x.go…');
  assert.equal(firstLine('  one  '), 'one');
  assert.equal(firstLine(''), '');
});
