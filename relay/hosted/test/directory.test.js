import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Directory } from '../src/directory.js';
import { AMENDED, STAGE1 } from '../src/rules.js';
import { memorySql } from './sql.js';
import { init } from './helpers.js';

function open(policy = STAGE1) {
  const clock = { now: 1_000_000 };
  return { clock, dir: new Directory(memorySql(), 'id_t', () => clock.now, policy) };
}
const code = (fn) => {
  try {
    fn();
    return 'ok';
  } catch (e) {
    return e.code;
  }
};

test('create makes a held cell, and a second create is refused', () => {
  const { dir } = open();
  const { cell } = dir.create('c1', init(), 'dev_a');
  assert.equal(cell.lease.fence, 1);
  assert.equal(code(() => dir.create('c1', init(), 'dev_b')), 'exists');
});

test('a live lease refuses another device, an expired one does not, force takes a live one', () => {
  const { dir, clock } = open();
  dir.create('c1', init(), 'dev_a');
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'lease_held');
  assert.equal(dir.acquire('c1', 'dev_b', true).cell.lease.fence, 2);
  clock.now += 31_000;
  assert.equal(dir.acquire('c1', 'dev_c').cell.lease.fence, 3);
});

test('the fence turns away the old holder after a takeover, and the head turns away a stale publish', () => {
  const { dir } = open();
  dir.create('c1', init(), 'dev_a');
  dir.acquire('c1', 'dev_b', true);
  const p = (fence, old, head) => () => dir.publish('c1', fence === 1 ? 'dev_a' : 'dev_b', { fence, old_head: old, head, size: 1, class: 'work', pending: 0 });
  assert.equal(code(p(1, 'h0', 'h1')), 'fence_stale');
  assert.equal(code(p(2, 'wrong', 'h1')), 'head_moved');
  assert.equal(code(p(2, 'h0', 'h1')), 'ok');
});


test('a refused rule leaves the stored cell exactly as it was', () => {
  const { dir } = open();
  const before = dir.create('c1', init(), 'dev_a');
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'lease_held');
  assert.deepEqual(dir.cell('c1').cell, before.cell);
});

test('a publish renews the lease under the amended policy and not under the frozen one', () => {
  for (const [policy, renews] of [[STAGE1, false], [AMENDED, true]]) {
    const { dir, clock } = open(policy);
    const { cell } = dir.create('c1', init(), 'dev_a');
    clock.now += 10_000;
    const after = dir.publish('c1', 'dev_a', { fence: 1, old_head: 'h0', head: 'h1', size: 0, class: 'work', pending: 0 }).cell;
    assert.equal(after.lease.expires > cell.lease.expires, renews);
  }
});

test('release, archive, vault and devices are kept, and the listing shows them', () => {
  const { dir } = open();
  dir.create('c1', init(), 'dev_a');
  dir.release('c1', 'dev_a', 1);
  dir.archive('c1');
  dir.setVault('', 'sealed');
  assert.equal(code(() => dir.setVault('', 'other')), 'cas');
  dir.putDevice('dev_a', { V: 1, name: 'n', revoked: false });
  const list = dir.list();
  assert.equal(list.cells.c1.archived, true);
  assert.equal(list.identity.vault, 'sealed');
  assert.deepEqual(Object.keys(list.devices), ['dev_a']);
});

test('a device is revoked only when its own record says so', () => {
  const { dir } = open();
  assert.equal(dir.revoked('dev_a'), false);
  dir.putDevice('dev_a', { V: 1, revoked: false });
  assert.equal(dir.revoked('dev_a'), false);
  dir.putDevice('dev_a', { V: 1, revoked: true });
  assert.equal(dir.revoked('dev_a'), true);
});

test('a cell that is not there is not_found for every verb that needs one', () => {
  const { dir } = open();
  for (const verb of [() => dir.cell('x'), () => dir.acquire('x', 'd'), () => dir.release('x', 'd', 1), () => dir.archive('x')]) {
    assert.equal(code(verb), 'not_found');
  }
});
