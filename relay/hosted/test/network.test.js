import { test } from 'node:test';
import assert from 'node:assert/strict';
import { networkOf } from '../src/network.js';
import { ipOf } from '../src/wire.js';

test('an IPv4 address is its own network', () => {
  assert.equal(networkOf('203.0.113.7'), '203.0.113.7');
});

test('an IPv6 address counts by its /64, whether written whole or compressed', () => {
  const want = '2001:db8:1:2::/64';
  for (const a of ['2001:db8:1:2:aaaa:bbbb:cccc:dddd', '2001:0db8:0001:0002:0000:0000:0000:0001', '2001:db8:1:2::1', '2001:DB8:1:2::', '2001:db8:1:2:3:4:5:6'])
    assert.equal(networkOf(a), want, a);
});

test('a compression inside the prefix is read', () => {
  assert.equal(networkOf('2001:db8::5:1'), '2001:db8:0:0::/64');
  assert.equal(networkOf('2001:db8:0:0:9::1'), '2001:db8:0:0::/64');
  assert.equal(networkOf('::1'), '0:0:0:0::/64');
  assert.equal(networkOf('::'), '0:0:0:0::/64');
});

test('a zone id is not part of the network', () => {
  assert.equal(networkOf('fe80::1%eth0'), 'fe80:0:0:0::/64');
  assert.equal(networkOf('fe80::1%en0'), networkOf('fe80::2'));
});

test('an IPv4-mapped IPv6 address is the IPv4 address', () => {
  assert.equal(networkOf('::ffff:203.0.113.7'), '203.0.113.7');
  assert.equal(networkOf('::FFFF:203.0.113.7'), '203.0.113.7');
  assert.equal(networkOf('0:0:0:0:0:ffff:203.0.113.7'), '203.0.113.7');
  assert.equal(networkOf('::ffff:cb00:7107'), '203.0.113.7');
});

test('different /64 blocks are different networks', () => {
  assert.notEqual(networkOf('2001:db8:1:2::1'), networkOf('2001:db8:1:3::1'));
  assert.notEqual(networkOf('2001:db8:1:2::1'), networkOf('2001:db9:1:2::1'));
});

test('text that is no address is its own network', () => {
  for (const a of ['unknown', '', 'not an ip', '1:2:3', '1::2::3', '2001:db8:::1', 'gggg::1', '1:2:3:4:5:6:7:8:9', '::ffff:999.1.1.1', '::ffff:1.2.3', '12345::1', '::ffff:01.2.3.4'])
    assert.equal(networkOf(a), a, JSON.stringify(a));
});

test('every per-caller limit sees the network, with or without a trusted proxy', () => {
  const at = (headers) => new Request('https://x/', { headers });
  assert.equal(ipOf(at({ 'cf-connecting-ip': '2001:db8:1:2::9' }), {}), '2001:db8:1:2::/64');
  assert.equal(ipOf(at({ 'x-forwarded-for': '::ffff:198.51.100.2, 10.0.0.1' }), { TRUST_PROXY: '1' }), '198.51.100.2');
  assert.equal(ipOf(at({}), {}), 'unknown');
});
