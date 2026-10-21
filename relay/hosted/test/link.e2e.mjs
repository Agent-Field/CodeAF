// Link pairing end to end (docs/ux-pairing-contract.md): create, read, approve, deny, presence,
// and every limit the contract names. Runs against a relay started with a 3 s request time to live,
// decided requests kept 1.5 s, 40 live requests, 6 creates an hour, 3 misses a minute (see e2e.sh).
// Each case that needs its own allowances comes from its own IP (X-Forwarded-For).
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { newIdentity, newDevice, signed } from './party.js';
import { answerOf, call } from './client.js';

const BASE = process.env.RELAY_LINK ?? 'http://127.0.0.1:18797';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const b64u = (b) => Buffer.from(b).toString('base64url');
let nextIp = 10;
const freshIp = () => `10.8.0.${nextIp++}`;

async function open(method, path, { ip = freshIp(), body } = {}) {
  const res = await fetch(BASE + path, { method, headers: { 'x-forwarded-for': ip }, body: body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body) });
  const text = await res.text();
  return { status: res.status, retry: res.headers.get('retry-after'), json: text.startsWith('{') ? JSON.parse(text) : null };
}

/** member is an identity with one paired device, and a way to make a device of that identity ask to join. */
async function member() {
  const identity = await newIdentity();
  const approver = await newDevice(identity);
  const newcomer = async (platform) => {
    const device = await newDevice(identity);
    const pubkey = Buffer.from(device.cert.device, 'hex');
    return { device, cert: b64u(JSON.stringify(device.cert)), body: { pubkey: b64u(pubkey), x25519: b64u(randomBytes(32)), name_sealed: b64u(randomBytes(24 + 16 + 5)), platform } };
  };
  return { identity, approver, newcomer };
}

const create = (body, ip) => open('POST', '/v1/link/requests', { body, ip });
const read = (code, query = '', ip) => open('GET', `/v1/link/requests/${code}${query}`, { ip });
const approveBody = (n, over = {}) => ({
  device: { V: 1, name: 'bmFtZQ', added_by: 'id_x', revoked: false, caps: { os: 'linux', arch: 'arm64', sandbox: null, container: null, gpu: null, cow: 'none' }, platform: 'linux', ...over.device },
  cert: n.cert,
  grant: b64u(randomBytes(64)),
  ...over.top,
});
const approve = (m, code, body) => call(m.approver, 'POST', `/v1/dir/requests/${code}/approve`, body, { base: BASE });
const deny = (m, code) => call(m.approver, 'POST', `/v1/dir/requests/${code}/deny`, undefined, { base: BASE });
const asked = async (m, platform) => {
  const n = await m.newcomer(platform);
  const made = await create(n.body);
  assert.equal(made.status, 201, JSON.stringify(made.json));
  return { n, ...made.json };
};

const limits = (await open('GET', '/v1/link/limits')).json;

const tests = {
  async Limits() {
    assert.deepEqual(Object.keys(limits).sort(), ['concurrent_polls', 'create_per_hour', 'max_grant', 'max_live', 'miss_per_minute', 'pending_per_ip', 'read_per_minute', 'ttl_ms']);
    assert.deepEqual([limits.ttl_ms, limits.max_grant, limits.pending_per_ip, limits.concurrent_polls], [3000, 4096, 3, 4]);
  },

  async CreateAndRead() {
    const m = await member();
    const n = await m.newcomer('darwin');
    const made = await create(n.body);
    assert.equal(made.status, 201);
    assert.match(made.json.code, /^[0-9a-hjkmnp-tv-z]{8}$/);
    assert.match(made.json.check, /^\d{4}$/);
    assert.equal(made.json.expires_at - made.json.requested_at, 3000);
    const got = await read(made.json.code);
    assert.equal(got.status, 200);
    assert.deepEqual(got.json, { ...n.body, code: made.json.code, device: made.json.device, check: made.json.check, requested_at: made.json.requested_at, expires_at: made.json.expires_at, state: 'pending', grant: null });
    assert.equal((await read(made.json.code.toUpperCase())).status, 200, 'a code is read in upper case too');
  },

  async CreateRefusals() {
    const n = await (await member()).newcomer();
    assert.equal(answerOf(await create({ ...n.body, pubkey: b64u(randomBytes(31)) })), 'bad_request');
    assert.equal(answerOf(await create({ ...n.body, device: 'dev_wrong' })), 'bad_request');
    assert.equal(answerOf(await create('[]')), 'bad_request');
    assert.equal(answerOf(await create('not json')), 'bad_request');
    assert.equal(answerOf(await create({ ...n.body, junk: 'x'.repeat(1100) })), 'too_big');
    assert.equal((await create({ ...n.body, platform: 'beos' })).status, 201);
  },

  async ApproveGrantsOnceAndLinksTheKey() {
    const m = await member();
    const { n, code, device } = await asked(m, 'linux');
    const poll = read(code, '?wait=2500');
    await sleep(150);
    const body = approveBody(n);
    assert.equal((await approve(m, code, body)).status, 204);
    const woke = await poll;
    assert.deepEqual([woke.status, woke.json.state, woke.json.grant], [200, 'approved', body.grant]);
    const listed = (await call(m.approver, 'GET', '/v1/dir/list', undefined, { base: BASE })).json.devices[device];
    assert.deepEqual([listed.platform, listed.revoked, typeof listed.created], ['linux', false, 'number'], 'the key is linked to the identity');
    assert.equal((await approve(m, code, body)).status, 204, 'the same approve again is idempotent');
    assert.equal(answerOf(await deny(m, code)), 'already_decided');
  },

  async ClientCannotSetCreatedOrLastSeen() {
    const m = await member();
    const { n, code, device } = await asked(m);
    await approve(m, code, approveBody(n, { device: { device: { created: 5, last_seen: 5 } } }));
    const rec = (await call(m.approver, 'GET', '/v1/dir/list', undefined, { base: BASE })).json.devices[device];
    assert.ok(rec.created > 1_700_000_000_000 && rec.last_seen === undefined, JSON.stringify(rec));
  },

  async DenyIsFinal() {
    const m = await member();
    const { n, code } = await asked(m);
    assert.equal((await deny(m, code)).status, 204);
    assert.equal((await deny(m, code)).status, 204, 'a repeat deny is 204');
    const got = (await read(code)).json;
    assert.deepEqual([got.state, got.grant], ['denied', null]);
    assert.equal(answerOf(await approve(m, code, approveBody(n))), 'already_decided');
  },

  async RaceHasOneWinner() {
    const m = await member();
    const { n, code } = await asked(m);
    const [a1, d1, a2, d2] = await Promise.all([approve(m, code, approveBody(n)), deny(m, code), approve(m, code, approveBody(n)), deny(m, code)]);
    const state = (await read(code)).json.state;
    const [wins, loses] = state === 'approved' ? [[a1, a2], [d1, d2]] : [[d1, d2], [a1, a2]];
    assert.deepEqual([...wins, ...loses].map((a) => a.status), [204, 204, 409, 409], `the first decision (${state}) wins and the other is refused`);
  },

  async ApproveRefusals() {
    const m = await member();
    const { n, code } = await asked(m);
    const other = await member();
    const foreign = await other.newcomer();
    assert.equal(answerOf(await approve(m, code, approveBody(foreign))), 'bad_request', 'a cert for another device');
    assert.equal(answerOf(await approve(m, code, approveBody(n, { top: { cert: foreign.cert } }))), 'bad_request');
    assert.equal(answerOf(await approve(other, code, approveBody(n))), 'bad_request', 'a cert of another identity');
    assert.equal(answerOf(await approve(m, code, approveBody(n, { top: { grant: b64u(randomBytes(4097)) } }))), 'too_big');
    assert.equal(answerOf(await approve(m, code, approveBody(n, { top: { grant: '' } }))), 'bad_request');
    assert.equal(answerOf(await approve(m, code, { cert: n.cert, grant: 'AAAA' })), 'bad_request', 'no device record');
    assert.equal(answerOf(await approve(m, 'zzzzzzzz', approveBody(n))), 'gone');
    assert.equal(answerOf(await approve(m, 'nope', approveBody(n))), 'gone');
    assert.equal(answerOf(await deny(m, 'zzzzzzzz')), 'gone');
    const bare = await fetch(`${BASE}/v1/dir/requests/${code}/approve`, { method: 'POST', body: '{}' });
    assert.equal(bare.status, 401, 'approval needs a signed call');
    assert.equal((await read(code)).json.state, 'pending', 'nothing above decided it');
  },

  async RevokedDeviceRejoinsOnlyAsNewKey() {
    const m = await member();
    const first = await asked(m);
    assert.equal((await approve(m, first.code, approveBody(first.n))).status, 204);
    assert.equal((await call(m.approver, 'POST', `/v1/dir/devices/${first.device}/revoke`, undefined, { base: BASE })).status, 204);
    const again = await create(first.n.body);
    assert.equal(again.status, 201);
    assert.equal(answerOf(await approve(m, again.json.code, approveBody(first.n))), 'bad_request');
  },

  async ExpiryIsGone() {
    const m = await member();
    const { n, code } = await asked(m);
    await sleep(limits.ttl_ms + 300);
    assert.deepEqual([(await read(code)).status, (await read(code)).json.err], [404, 'gone']);
    assert.equal(answerOf(await approve(m, code, approveBody(n))), 'gone');
  },

  async DecidedRequestIsDeletedAfterTheKeepTime() {
    const m = await member();
    const { code } = await asked(m);
    await deny(m, code);
    assert.equal((await read(code)).status, 200);
    await sleep(1800);
    assert.equal((await read(code)).status, 404);
  },

  async PollWithoutDecisionIs204() {
    const m = await member();
    const { code } = await asked(m);
    const got = await read(code, '?wait=300');
    assert.equal(got.status, 204);
  },

  async PendingPerIpIsCapped() {
    const ip = freshIp();
    const m = await member();
    const codes = [];
    for (let i = 0; i < 3; i++) codes.push((await create((await m.newcomer()).body, ip)).json.code);
    const over = await create((await m.newcomer()).body, ip);
    assert.deepEqual([over.status, over.json.err], [429, 'rate_limited']);
    assert.ok(Number(over.retry) >= 1, 'Retry-After is set');
    await deny(m, codes[0]);
    assert.equal((await create((await m.newcomer()).body, ip)).status, 201, 'a decided request frees its place');
  },

  async MissesAreCounted() {
    const ip = freshIp();
    const answers = [];
    for (let i = 0; i < 5; i++) answers.push((await read('zzzzzzz' + i, '', ip)).status);
    assert.deepEqual(answers, [404, 404, 404, 429, 429]);
    const m = await member();
    const { code } = await asked(m);
    assert.equal((await read(code, '', ip)).status, 429, 'a caller over its misses reads nothing');
    assert.equal((await read(code, '', freshIp())).status, 200, 'another caller still can');
  },

  async CreatesAnHourAreCapped() {
    const ip = freshIp();
    const m = await member();
    const out = [];
    for (let i = 0; i < 7; i++) {
      const made = await create((await m.newcomer()).body, ip);
      out.push(made.status);
      if (made.status === 201) await deny(m, made.json.code);
    }
    assert.deepEqual(out, [201, 201, 201, 201, 201, 201, 429]);
  },

  async PollsAreLimited() {
    const m = await member();
    const { code } = await asked(m);
    const ip = freshIp();
    const polls = Array.from({ length: 5 }, () => read(code, '?wait=1200', ip));
    const statuses = (await Promise.all(polls)).map((p) => p.status).sort();
    assert.deepEqual(statuses, [204, 204, 204, 204, 429]);
  },

  async RelayFull() {
    await sleep(limits.ttl_ms + 300);
    const m = await member();
    let ip = freshIp();
    for (let i = 0; i < limits.max_live; i++) {
      if (i % limits.pending_per_ip === 0) ip = freshIp();
      assert.equal((await create((await m.newcomer()).body, ip)).status, 201, `request ${i}`);
    }
    const full = await create((await m.newcomer()).body, freshIp());
    assert.deepEqual([full.status, full.json.err], [503, 'full']);
  },

  async PresenceFollowsSockets() {
    await sleep(limits.ttl_ms + 300);
    const m = await member();
    const read = async () => (await call(m.approver, 'GET', '/v1/dir/presence', undefined, { base: BASE }));
    const { n, code, device } = await asked(m);
    await approve(m, code, approveBody(n));
    const before = await read();
    assert.ok(before.headers.get('codeaf-dir-version'));
    assert.deepEqual(before.json.devices[device], { online: false, last_seen: 0 });
    assert.deepEqual(Object.keys(before.json.devices), [device], 'only devices in the directory');
    const path = '/v1/dir/watch';
    const ws = new WebSocket(BASE.replace(/^http/, 'ws') + path, { headers: await signed(n.device, 'GET', path) });
    await new Promise((up, fail) => (ws.addEventListener('open', up), ws.addEventListener('error', () => fail(new Error('upgrade refused')))));
    const during = (await read()).json;
    assert.equal(during.devices[device].online, true);
    assert.ok(Math.abs(during.devices[device].last_seen - during.now) < 1);
    ws.close();
    for (let i = 0; i < 40 && (await read()).json.devices[device].online; i++) await sleep(50);
    assert.equal((await read()).json.devices[device].online, false);
  },
};

for (const [name, run] of Object.entries(tests)) {
  await run();
  console.log('link ok', name);
}
console.log('link: all pass');
