import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Fabric } from '../src/fabric.js';
import { Directory } from '../src/directory.js';
import { STAGE1 } from '../src/rules.js';
import { memorySql } from './sql.js';

const bytes = (value) => new TextEncoder().encode(JSON.stringify(value));

test('only the persisted process token recovers a claim after a lost response', () => {
  const sql = memorySql();
  const now = () => 1_000_000;
  const dir = new Directory(sql, 'id_claims', now, STAGE1);
  const device = 'dev_' + 'a'.repeat(32);
  const client = 'dev_' + 'b'.repeat(32);
  dir.putDevice(device, { V: 1 });
  dir.putDevice(client, { V: 1, kind: 'client', methods: ['agents.create'] });
  const fabric = new Fabric(sql, now, dir);
  const body = bytes({ number: 1, at: now(), method: 'agents.create', target: 'any', sealed: 'YQ==' });
  fabric.appendQueue({ caller: client, targetDevice: client, body, proof: {
    signature: 'sig', cert: 'cert', identity: 'identity', requestTime: 'time', requestUri: '/queue', bodyBytes: body,
  } });
  const request = { caller: device, targetDevice: client, numberText: '1', ack: false };
  const token = bytes({ token: 'c'.repeat(64) });
  fabric.claimQueue({ ...request, body: token });
  const reopened = new Fabric(sql, now, dir);
  assert.throws(() => reopened.claimQueue({ ...request, body: bytes({ token: 'd'.repeat(64) }) }), { code: 'claimed' });
  assert.throws(() => reopened.claimQueue({ ...request, body: new Uint8Array() }), { code: 'claimed' });
  const queue = reopened.listQueue({ caller: device, targetDevice: client, body: new Uint8Array() });
  assert.equal(queue.length, 1);
  assert.equal(JSON.stringify(queue).includes('c'.repeat(64)), false);
  reopened.claimQueue({ ...request, body: token });
  reopened.claimQueue({ ...request, ack: true, body: new Uint8Array() });
  assert.deepEqual(reopened.listQueue({ caller: device, targetDevice: client, body: new Uint8Array() }), []);
});
