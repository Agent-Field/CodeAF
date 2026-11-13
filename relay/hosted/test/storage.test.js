import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { generateKeyPairSync, sign } from 'node:crypto';
import { dispatch } from '../src/front.js';
import { StorageAccess, ADMIN_DOMAIN, ADMIN_WINDOW_MS } from '../src/storage.js';
import { limitsOf } from '../src/limits.js';
import { Meta } from '../src/meta.js';
import { Tenant } from '../src/tenant.js';
import { RaftBlobTenant } from '../src/raft/effects.js';
import { sealFrame } from '../src/store.js';
import { matchRoute } from '../src/routes.js';
import { PROTOCOL, HEADER_NAME } from '../src/engine/carrier.js';
import { Flight } from '../src/flight.js';
import { NOBODY } from '../src/watch.js';
import { policyOf } from '../src/rules.js';
import { respond, guarded } from '../src/wire.js';
import { memorySql } from './sql.js';
import { frame, rid } from './helpers.js';

const account = 'id_' + 'a'.repeat(32);
const refusal = (code, status) => e => e.code === code && e.status === status;
function tenant() {
  const data = new Map(), sql = memorySql(); let puts = 0, reserves = 0;
  const bucket = { async put(key, value) { puts++; data.set(key, value.slice()); }, async get(key, options) {
    const bytes = data.get(key); if (!bytes) return null;
    const range = options?.range, off = range?.offset ?? 0;
    return { size: bytes.length, body: bytes, arrayBuffer: async () => bytes.slice(off, off + (range?.length ?? bytes.length)).buffer };
  }};
  const t = new Tenant({ identity: account, sql, bucket, clock: () => 1, limits: limitsOf({}), policy: policyOf(),
    flight: new Flight(), arm: () => {}, watchers: NOBODY, reserve: async () => { reserves++; } });
  return { t, sql, data, puts: () => puts, reserves: () => reserves };
}
const call = (t, method, path, body) => {
  const route = matchRoute(method, path);
  return respond(() => route.handler({ tenant: t, body, device: 'device', request: new Request('https://relay.test' + path, { method }) }, route.args));
};

test('R1 R2 both frame doors refuse before R2 and every quota or global reservation change', async () => {
  const r = tenant(), bytes = frame([rid(1)]).bytes;
  const before = r.t.quota.snapshot(1);
  const response = await call(r.t, 'POST', '/v1/store/frames', bytes);
  assert.equal(response.status, 403); assert.deepEqual(await response.json(), { err: 'storage_off' });
  const raft = new RaftBlobTenant(r.t.store, async () => {}, async () => {});
  let admits = 0;
  await assert.rejects(raft.putFrame(await sealFrame(bytes), () => { admits++; }), refusal('storage_off', 403));
  assert.equal(r.puts(), 0); assert.equal(r.reserves(), 0); assert.equal(admits, 0);
  assert.deepEqual(r.t.quota.snapshot(1), before);
  assert.deepEqual(r.sql.exec('SELECT * FROM frames').toArray(), []);
});

test('R3 R4 R9 approval and revocation preserve reads, directory writes and stats', async () => {
  const r = tenant(), f = frame([rid(2)]);
  assert.equal((await (await call(r.t, 'GET', '/v1/store/stats')).json()).storage, 'off');
  r.t.storage.set({ storage: 'on', at: 1 });
  assert.equal((await call(r.t, 'POST', '/v1/store/frames', f.bytes)).status, 200);
  assert.equal((await (await call(r.t, 'GET', '/v1/store/stats')).json()).storage, 'on');
  r.t.storage.set({ storage: 'off', at: 2 });
  assert.equal((await call(r.t, 'POST', '/v1/store/frames', f.bytes)).status, 403);
  assert.deepEqual(await r.t.getObject(rid(2)), f.objs[0]);
  assert.deepEqual(await r.t.has([rid(2)]), [true]);
  assert.equal(Object.keys(await r.t.locate([rid(2)])).length, 1);
  assert.equal((await r.t.getFrame((await sealFrame(f.bytes)).id)).size, f.bytes.length);
  r.t.dir.create('chat', { head: 'h', class: 'chat' }, 'device');
  assert.equal(r.t.dir.cell('chat').cell.head, 'h');
  assert.equal((await (await call(r.t, 'GET', '/v1/store/stats')).json()).storage, 'off');
});

test('R5 storageOpen accepts true/1 only and default is closed in every deployment', async () => {
  for (const value of [undefined, false, 0, 'false', '0', 'yes']) assert.equal(limitsOf({ CAF_LIMITS: JSON.stringify({ storageOpen: value }) }).storageOpen, false);
  for (const value of [true, 1, 'true', '1']) {
    const r = tenant(); r.t.storage.limits = limitsOf({ CAF_LIMITS: JSON.stringify({ storageOpen: value }) });
    assert.equal(r.t.storage.state(), 'on');
    await r.t.putFrame(frame([rid(3)]).bytes); assert.equal(r.puts(), 1);
  }
  const config = readFileSync(new URL('../wrangler.toml', import.meta.url), 'utf8');
  assert(config.includes('CAF_ADMIN_KEYS = "aTuSKJXUX8oeetqpxdDIqyB7tHfetea0dmlHJzEfreQ"'));
  assert(!/storageOpen\s*[":= ]+\s*(true|1)/.test(config));
  const generator = readFileSync(new URL('../../../scripts/relay-wrangler-config.sh', import.meta.url), 'utf8');
  assert(generator.includes('print $0'));
  for (const channel of ['stable', 'staging', 'dev']) {
    const generated = execFileSync('bash', [new URL('../../../scripts/relay-wrangler-config.sh', import.meta.url).pathname, channel], { env: { ...process.env, CLOUDFLARE_ACCOUNT_ID: 'test-account' }, encoding: 'utf8' });
    assert(generated.includes('CAF_ADMIN_KEYS = "aTuSKJXUX8oeetqpxdDIqyB7tHfetea0dmlHJzEfreQ"'));
    assert(!/storageOpen/.test(generated));
  }
});

function admin() {
  const keys = generateKeyPairSync('ed25519'), pub = keys.publicKey.export({ type: 'spki', format: 'der' }).subarray(-32).toString('base64url');
  const records = new Map(); let admissions = 0;
  const env = { BASE_PATH: '/fabric', CAF_ADMIN_KEYS: pub, IDENTITY: {
    idFromName(id) { return id; }, get(id) { return { setStorage: body => guarded(() => {
      let state = records.get(id);
      if (!state) { state = new StorageAccess(new Meta(memorySql()), limitsOf({})); records.set(id, state); }
      state.set(body);
    }) }; },
  }, NEWCOMERS: { get() { admissions++; throw Error('approval must not admit'); } } };
  const now = Date.now();
  const body = { v: 1, host: 'relay.test', identity: account, storage: 'on', at: now, nonce: 'random-test-nonce' };
  const send = async (changes = {}, options = {}) => {
    const raw = options.raw ?? JSON.stringify({ ...body, ...changes });
    const sig = options.signature ?? sign(null, Buffer.concat([Buffer.from(ADMIN_DOMAIN), Buffer.from(raw)]), keys.privateKey).toString('base64url');
    return respond(() => dispatch(new Request('https://relay.test/fabric/v1/admin/storage', { method: 'POST', headers: { 'codeaf-admin-signature': sig }, body: raw }), env));
  };
  return { env, records, body, signature: (raw, label=ADMIN_DOMAIN) => sign(null,Buffer.from(label+raw),keys.privateKey).toString('base64url'), send, admissions: () => admissions };
}

test('R6 R7 signed front-door approval works before first device contact; stale and reordered commands fail', async () => {
  const a = admin();
  const response = await a.send(); assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { identity: account, storage: 'on' });
  assert.equal(a.admissions(), 0);
  assert.equal((await a.send()).status, 409);
  assert.equal((await a.send({ at: a.body.at - 1, storage: 'off' })).status, 409);
  assert.equal((await a.send({ at: a.body.at + 1, storage: 'off' })).status, 200);
  assert.equal(a.records.get(account).state(), 'off');
});

test('R6 R7 signature, exact bytes, host, time window, shape and missing keys are enforced', async () => {
  const a = admin();
  for (const changes of [{ host: 'other.test' }, { at: a.body.at - ADMIN_WINDOW_MS - 1000 }, { at: a.body.at + ADMIN_WINDOW_MS + 1000 }]) assert.equal((await a.send(changes)).status, 401);
  for (const changes of [{ v: 2 }, { identity: 'bad' }, { storage: true }, { nonce: '' }, { at: '1' }]) assert.equal((await a.send(changes)).status, 400);
  assert.equal((await a.send({}, { raw: '{' })).status, 400);
  assert.equal((await a.send({}, { signature: 'bad' })).status, 401);
  const unknown = admin(); a.env.CAF_ADMIN_KEYS = unknown.env.CAF_ADMIN_KEYS;
  assert.equal((await a.send()).status, 401);
  a.env.CAF_ADMIN_KEYS = ''; assert.equal((await a.send()).status, 401);
  assert.equal(a.records.size, 0);
});

test('R1 structural guard keeps all production R2 puts inside the gated writer', () => {
  const root = new URL('../src/', import.meta.url), paths = [];
  const visit = dir => { for (const entry of readdirSync(dir, { withFileTypes: true })) { const path = new URL(entry.name, dir); if (entry.isDirectory()) visit(new URL(entry.name + '/', dir)); else if (entry.name.endsWith('.js')) paths.push(path); } };
  visit(root);
  const puts = paths.flatMap(path => [...readFileSync(path, 'utf8').matchAll(/\b(?:bucket|FRAMES)\s*(?:\.\s*put|\[\s*['"]put['"]\s*\])\s*\(/g)].map(() => path.pathname));
  assert.deepEqual(puts, [new URL('../src/store.js', import.meta.url).pathname]);
  const constructors = paths.filter(path => /new R2Store\(/.test(readFileSync(path, 'utf8'))).map(path => path.pathname);
  assert.deepEqual(constructors, [new URL('../src/tenant.js', import.meta.url).pathname]);
  const tenantSource = readFileSync(new URL('../src/tenant.js', import.meta.url), 'utf8');
  assert.match(tenantSource, /new R2Store\(bucket, sql, identity, \(\) => this\.storage\.assert\(\)\)/);
  const store = readFileSync(new URL('../src/store.js', import.meta.url), 'utf8');
  assert.match(store, /this\.assertStorage\(\);\s*await this\.bucket\.put/);
  assert.match(store, /this\.assertStorage\(\);\s*if \(this\.#indexed/);
});


test('R3 pairing and machine tunnel ingress remain available with storage off', async () => {
  let pairs=0, machines=0;
  const gate={create:async()=>{pairs++;return {ok:{nameplate:42}};}};
  const env={CAF_LIMITS:'{"storageOpen":false}',PAIR_GATE:{idFromName:x=>x,get:()=>gate},
    MACHINE:{idFromName:x=>x,get:()=>({fetch:async()=>{machines++;return new Response('live',{status:200});}})},
    MACHINE_DIAL_LIMIT:{limit:async()=>({success:true})},MACHINE_REGISTER_LIMIT:{limit:async()=>({success:true})},
    FRAMES:new Proxy({}, {get(){throw Error('live access touched R2');}}),
    IDENTITY:new Proxy({}, {get(){throw Error('live access consulted storage approval');}})};
  let response=await respond(()=>dispatch(new Request('https://relay.test/v1/pair',{method:'POST',headers:{'codeaf-pair-key':Buffer.alloc(16,1).toString('base64url')}}),env));
  assert.equal(response.status,201);assert.equal(pairs,1);
  for(const path of ['/v1/engine','/v1/dial/amber-forest-42']) {
    response=await respond(()=>dispatch(new Request('https://relay.test'+path,{headers:{upgrade:'websocket','sec-websocket-protocol':PROTOCOL,[HEADER_NAME]:'amber-forest-42'}}),env));
    assert.equal(response.status,200,await response.text());
  }
  assert.equal(machines,2);
});

test('S2 a fixed valid signature refuses changed bytes, whitespace and a missing domain label', async () => {
 const a=admin(), raw=JSON.stringify(a.body), signature=a.signature(raw);
 for(const changed of [raw.replace('random-test-nonce','random-test-noncf'),raw+' ']) assert.equal((await a.send({}, {raw:changed,signature})).status,401);
 assert.equal((await a.send({}, {raw,signature:a.signature(raw,'')})).status,401);
 assert.equal(a.records.size,0);
 delete a.env.CAF_ADMIN_KEYS;
 assert.equal((await a.send()).status,401);
 const other=admin(); a.env.CAF_ADMIN_KEYS=other.env.CAF_ADMIN_KEYS+', '+admin().env.CAF_ADMIN_KEYS;
 assert.equal((await a.send()).status,401);
 a.env.CAF_ADMIN_KEYS+=', '+other.env.CAF_ADMIN_KEYS;
 const b=admin(); b.env.CAF_ADMIN_KEYS=other.env.CAF_ADMIN_KEYS+', '+b.env.CAF_ADMIN_KEYS;
 assert.equal((await b.send()).status,200);
});

test('B1 inheritance copies only on into an empty successor and never replaces its own record', () => {
 for(const state of ['on','off']) {
  const access=new StorageAccess(new Meta(memorySql()), limitsOf({}));
  access.set({storage:state,at:1});
  access.set({storage:'on',at:5},true);
  assert.deepEqual(access.approval(),{storage:state,at:1});
 }
 const empty=new StorageAccess(new Meta(memorySql()),limitsOf({}));
 empty.set({storage:'off',at:10},true); assert.equal(empty.approval(),undefined);
 empty.set({storage:'on',at:11},true); assert.equal(empty.state(),'on');
 const open=new StorageAccess(new Meta(memorySql()),{storageOpen:true});
 open.set({storage:'on',at:12},true); assert.equal(open.approval(),undefined);
});
