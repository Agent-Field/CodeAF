import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Directory } from '../src/directory.js';
import { STAGE1 } from '../src/rules.js';
import { CLOSE_REVOKED, CLOSE_ROTATED } from '../src/watch.js';
import { memorySql } from './sql.js';
import { init } from './helpers.js';

const DEVICE = { V: 1, name: '', added_by: '', revoked: false, caps: { os: 'linux', arch: 'arm64', sandbox: null, container: null, gpu: null, cow: 'none' } };

/** A recorder stands where the sockets would: it keeps what the directory told them, in order. */
function open(sql = memorySql()) {
  const clock = { now: 1_000_000 };
  const heard = [];
  const watchers = {
    broadcast: (v) => heard.push(`v${v}`),
    closeDevice: (d, code) => heard.push(`close ${d} ${code}`),
    closeAll: (code) => heard.push(`closeAll ${code}`),
  };
  return { sql, clock, heard, dir: new Directory(sql, 'id_t', () => clock.now, STAGE1, watchers) };
}

test('a fresh directory is at version 0 and each visible change moves it by exactly one', () => {
  const { dir, heard } = open();
  assert.equal(dir.version, 0);
  dir.create('c1', init(), 'dev_a');
  const { cell } = dir.acquire('c1', 'dev_a');
  dir.publish('c1', 'dev_a', { fence: cell.lease.fence, old_head: 'h0', head: 'h1', size: 1, class: 'work', pending: 0 });
  dir.archive('c1');
  dir.putDevice('dev_a', DEVICE);
  dir.setVault('', 'x');
  assert.deepEqual(heard, ['v1', 'v2', 'v3', 'v4', 'v5', 'v6']);
  assert.equal(dir.version, 6);
});

test('a heartbeat that only moves the expiry is silent, and one that changes pending is not', () => {
  const { dir, clock, heard } = open();
  dir.create('c1', init(), 'dev_a');
  heard.length = 0;
  clock.now += 10_000;
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 0 });
  assert.deepEqual(heard, [], 'the lease ran on, nothing a person sees changed');
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 2 });
  assert.deepEqual(heard, ['v2']);
});

test('a lease that is held, released, then renewed is seen to change each time, a second release is not', () => {
  const { dir, clock, heard } = open();
  dir.create('c1', init(), 'dev_a');
  dir.release('c1', 'dev_a', 1);
  dir.release('c1', 'dev_a', 1);
  assert.deepEqual(heard, ['v1', 'v2'], 'held to free once; free to free changes nothing');
  clock.now += 1;
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 0 });
  assert.deepEqual(heard, ['v1', 'v2', 'v3'], 'free to held is visible');
});

test('an identical device record, a repeated archive and a repeated revoke change nothing', () => {
  const { dir, heard } = open();
  dir.putDevice('dev_a', DEVICE);
  dir.putDevice('dev_b', DEVICE);
  dir.create('c1', init(), 'dev_a');
  heard.length = 0;
  dir.putDevice('dev_a', DEVICE);
  dir.archive('c1');
  dir.archive('c1');
  dir.revoke('dev_b', 'dev_a');
  dir.revoke('dev_b', 'dev_a');
  assert.deepEqual(heard, ['v4', 'v5', 'close dev_b 4401', 'close dev_b 4401']);
});

test('a refused rule writes nothing and counts nothing', () => {
  const { dir, heard } = open();
  dir.create('c1', init(), 'dev_a');
  heard.length = 0;
  assert.throws(() => dir.acquire('c1', 'dev_b'));
  assert.throws(() => dir.publish('c1', 'dev_a', { fence: 9, old_head: 'h0', head: 'h1', size: 1, class: 'work', pending: 0 }));
  assert.deepEqual(heard, []);
  assert.equal(dir.version, 1);
});

test('revoke asks for its device sockets to close with 4401, rotation for all with 4410, thaw for none', () => {
  const { dir, heard } = open();
  dir.putDevice('dev_b', DEVICE);
  heard.length = 0;
  dir.revoke('dev_b', 'dev_a');
  dir.setRotation({ state: 'frozen', by: 'dev_a', at: 1 });
  dir.setRotation(undefined);
  assert.deepEqual(heard, ['v2', `close dev_b ${CLOSE_REVOKED}`, 'v3', `closeAll ${CLOSE_ROTATED}`, 'v4']);
});

test('the version outlives the object: a new Directory over the same storage continues the count', () => {
  const first = open();
  first.dir.create('c1', init(), 'dev_a');
  first.dir.archive('c1');
  const second = open(first.sql);
  assert.equal(second.dir.version, 2);
  second.dir.putDevice('dev_a', DEVICE);
  assert.equal(second.dir.version, 3);
});

test('key order does not make two equal records different', () => {
  const { dir, heard } = open();
  dir.putDevice('dev_a', DEVICE);
  heard.length = 0;
  dir.putDevice('dev_a', { caps: DEVICE.caps, revoked: false, added_by: '', name: '', V: 1 });
  assert.deepEqual(heard, []);
});

test('an idempotent freeze, and a thaw of a live identity, bump nothing; a real freeze bumps once then closes all', () => {
  const { dir, heard } = open();
  dir.setRotation(undefined);
  assert.deepEqual(heard, [], 'a thaw of an identity that never rotated is no change');
  dir.setRotation({ state: 'frozen', by: 'dev_a', at: 1 });
  dir.setRotation({ state: 'frozen', by: 'dev_a', at: 1 });
  assert.equal(dir.version, 1, 'the owner freezing again stores the same record');
  assert.deepEqual(heard.filter((h) => h.startsWith('v')), ['v1']);
  dir.setRotation(undefined);
  assert.equal(dir.version, 2, 'a thaw of a frozen identity is visible');
});
