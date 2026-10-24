import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createHash, generateKeyPairSync, sign } from 'node:crypto';
import { openWorkerd } from './workerd.js';
import { encodeCanonical } from '../../src/raft/codec.js';

const out = resolve(process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-p08');
const sha = b => createHash('sha256').update(b).digest('hex');
const b64 = b => Buffer.from(b).toString('base64');
const unb64 = s => Buffer.from(s, 'base64');
const clockTicks = Number(execFileSync('getconf', ['CLK_TCK'], { encoding: 'utf8' }).trim());
const report = { calls: [], requirements: ['R3','R4','R6','R7','R8','R10','R12','R13','R14','R16','R27'], samples: [], phases: [] };
async function sample() {
  const ids = [process.pid, ...(await readFile(`/proc/${process.pid}/task/${process.pid}/children`, 'utf8')).trim().split(/\s+/).filter(Boolean).map(Number)];
  const processes = [];
  for (const pid of ids) { try { const s = await readFile(`/proc/${pid}/status`, 'utf8'), stat = (await readFile(`/proc/${pid}/stat`, 'utf8')).split(') ').slice(1).join(') ').split(' '); processes.push({ pid, cpuSeconds: (Number(stat[11]) + Number(stat[12])) / clockTicks, rss: Number(s.match(/^VmRSS:\s+(\d+)/m)?.[1] ?? 0) * 1024 }); } catch {} }
  const value = { processes, combinedRSS: processes.reduce((n, p) => n + p.rss, 0) }; report.samples.push(value); return value;
}
class Native {
  constructor(dir) {
    this.child = spawn(join(out, 'native-peer'), ['actors', dir], { stdio: ['pipe','pipe','inherit'], env: { ...process.env, GOMEMLIMIT: '32MiB' } });
    this.buffer = Buffer.alloc(0); this.waiter = null;
    this.child.stdout.on('data', b => { this.buffer = Buffer.concat([this.buffer, b]); this.pump(); });
    this.child.on('exit', code => this.waiter?.reject(new Error(`native exit ${code}`)));
  }
  pump() { if (!this.waiter || this.buffer.length < 4) return; const n = this.buffer.readUInt32LE(0); assert(n < 2 * 1024 * 1024); if (this.buffer.length < n + 4) return; const value = JSON.parse(this.buffer.subarray(4, n + 4)); this.buffer = this.buffer.subarray(n + 4); const p = this.waiter; this.waiter = null; clearTimeout(p.timer); p.resolve(value); }
  call(value) {
    assert(!this.waiter); return new Promise((resolve, reject) => { this.waiter = { resolve, reject, timer: setTimeout(() => reject(new Error('native actor timeout')), 5000) }; const b = Buffer.from(JSON.stringify(value)), h = Buffer.alloc(4); h.writeUInt32LE(b.length); this.child.stdin.write(h); this.child.stdin.write(b); this.pump(); });
  }
  async close() { this.child.stdin.end(); if (this.child.exitCode === null) await new Promise(resolve => this.child.once('exit', resolve)); }
}
// This parser only identifies snapshot transport in the test scheduler. Both
// production receivers use Go's strict protobuf admission, never this parser.
function fields(b) {
  let at = 0; const map = new Map();
  const uint = () => { let n = 0n, shift = 0n; for (let i = 0; i < 10; i++) { assert(at < b.length); const x = b[at++]; n |= BigInt(x & 127) << shift; if (!(x & 128)) return n; shift += 7n; } throw new Error('varint'); };
  while (at < b.length) { const k = uint(), kind = Number(k & 7n); let v; if (kind === 0) v = uint(); else { assert.equal(kind, 2); const n = Number(uint()); v = b.subarray(at, at + n); at += n; } map.set(Number(k >> 3n), v); } return map;
}
class Group {
  constructor(worker, native, cluster, wasm) {
    this.worker = worker; this.native = native; this.cluster = cluster; this.wasm = wasm; this.nodes = new Map(); this.queue = []; this.reads = []; this.block = new Set(); this.sequence = 0n;
    this.scope = { identity: cluster, cluster, epoch: '1', protocol: 1 }; this.keys = {}; this.view = {};
    for (let i = 1; i <= 4; i++) { const k = generateKeyPairSync('ed25519'); this.keys[i] = k.privateKey; this.view[i] = { key: k.publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64'), incarnation: '1', revoked: false, voter: true }; }
  }
  async call(id, op, value = {}) {
    if(op==='leadership-plan')value={...value,value:JSON.parse(encodeCanonical(value.value))};
    let r; const start = performance.now();
    if (this.wasm.includes(id)) {
      const res = await this.worker.dispatchFetch(`http://proof/actor/${this.cluster}-${id}/${op}`, { method: 'POST', body: JSON.stringify(value), signal: AbortSignal.timeout(5000) }); assert.equal(res.status, 200, await res.clone().text()); r = await res.json();
    } else r = await this.native.call({ op, ...value, id: String(id) });
    report.calls.push({ op, wasm: this.wasm.includes(id), ms: performance.now() - start });
    if (r?.error) throw new Error(`node ${id} ${op}: ${r.error}`);
    return r;
  }
  accept(id, r) { if (!r) return; if (r.status) this.nodes.set(id, r.status); this.queue.push(...(r.send ?? []).map(m => ({ ...m, from: id }))); this.reads.push(...(r.reads ?? []).map(r => ({ ...r, node: id }))); }
  async init() {
    for (let id = 1; id <= 3; id++) this.accept(id, await this.call(id, 'init', { cluster: this.cluster, id: String(id), seed: String(100 + id), bootstrap: ['1','2','3'], scope: this.scope }));
    for (const id of this.wasm.filter(id=>this.nodes.has(id))) await this.call(id, 'enrollment', { view: this.view, applied: this.nodes.get(id).applied });
    await this.pump(); await this.tick(16); assert(this.leader(), JSON.stringify([...this.nodes]));
  }
  envelope(from, to, payload, chunk = null) {
    const e = { scope: this.scope, from: String(from), to: String(to), incarnation: '1', destination: '1', sequence: String(++this.sequence), chunk, digest: sha(payload), payload: b64(payload), signature: null };
    e.signature = sign(null, Buffer.from('codeaf/raft/envelope/1\n' + JSON.stringify(e)), this.keys[from]).toString('base64'); return b64(Buffer.from(JSON.stringify(e)));
  }
  async step(id, tag, value) { this.accept(id, await this.call(id, 'input', { tag, value })); }
  async deliver(m) {
    const to = Number(m.to), payload = unb64(m.bytes), f = fields(payload), snapshot = f.get(9);
    if (snapshot?.length) {
      const data = fields(snapshot).get(1), manifest = JSON.parse(data.toString()), generation = sha(data);
      await this.call(to, 'begin', { data: b64(data) });
      for (let offset = 0; offset < Number(manifest.chunks); offset++) {
        const chunk = await this.call(m.from, 'read-chunk', { generation, id: generation, offset });
        if (this.wasm.includes(to)) await this.call(to, 'peer', { data: this.envelope(m.from, to, unb64(chunk), { generation, offset: String(offset), hash: sha(unb64(chunk)) }) });
        else await this.call(to, 'chunk', { generation, offset, data: chunk, hash: sha(unb64(chunk)) });
      }
      await this.call(to, 'complete', { generation, id: generation });
      report.phases.push(`${this.wasm.includes(m.from) ? 'wasm' : 'native'} snapshot -> ${this.wasm.includes(to) ? 'wasm' : 'native'}`);
    }
    if (this.wasm.includes(to)) this.accept(to, await this.call(to, 'peer', { data: this.envelope(m.from, to, payload) }));
    else await this.step(to, 2, m.bytes);
    if (snapshot?.length) await this.step(m.from, 12, { peer: String(to), snapshot: true, success: true });
  }
  async pump() { for (let events = 0; this.queue.length; events++) { assert(events < 2000); const m = this.queue.shift(); if (this.block.has(m.from) || this.block.has(Number(m.to))) continue; await this.deliver(m); } }
  async tick(rounds = 1) { for (let i = 0; i < rounds; i++) { for (const id of this.nodes.keys()) if (!this.block.has(id)) await this.step(id, 1); await this.pump(); } }
  leader() { return [...this.nodes.entries()].find(([id, s]) => !this.block.has(id) && s.role === 2)?.[0]; }
  async transfer(to) { const leader = this.leader(); if (leader !== to) { await this.step(leader, 7, String(to)); await this.pump(); await this.tick(2); } assert.equal(this.leader(), to); }
  async read(id, name) { await this.step(id, 4, name); await this.pump(); return this.reads.find(r => r.id === name); }
  command(change, sequence = 1) { return b64(Buffer.from(encodeCanonical({ schema: 1, cluster: this.cluster, actor: 'device-a', session: 'actor-probe', sequence: String(sequence), request_digest: 'opaque-digest', now: 1000, change, signature: 'AQID' }))); }
  async create() { await this.step(this.leader(), 3, this.command({ create: { cell: 'cell', init: { head: 'sealed-head', class: 'work', size: '7', title: 'sealed-title', keys: {} }, receipt: 'durable-receipt' } })); await this.pump(); }
  async mutate(sequence) { const change = sequence === 2 ? { release: { cell: 'cell', fence: '1', quiet: false } } : { acquire: { cell: 'cell', force: false, expect: { device: 'device-a', fence: '1' } } }; await this.step(this.leader(), 3, this.command(change, sequence)); await this.pump(); }
  async sameRows() { const rows = []; for (const id of this.nodes.keys()) rows.push(await this.call(id, 'row')); assert(rows[0]); assert.equal(rows[0], rows[1]); assert.equal(rows[0], rows[2]); for (const row of rows) assert.equal(row, rows[0]); return rows[0]; }
}

test('R3 R4 R6 R7 R8 R10 R13 R14 R27 hosted actors commit, ReadIndex, crash and snapshot in both directions', { timeout: 120000 }, async () => {
  const dir = await mkdtemp(join(tmpdir(), 'p08-')); let native = new Native(join(dir, 'native'));
  let worker, inspector, samplingTask, samplingError, samplingPaused = false; const heaps = [];
  // A sample owns its inspector call until completion. Drain it before each
  // intentional process death so a late timeout cannot escape the test.
  const timer = setInterval(() => {
    if (samplingTask || samplingPaused) return;
    samplingTask = (async () => { await sample(); if (inspector) heaps.push(await inspector.call('Runtime.getHeapUsage')); })()
      .catch(error => { samplingError ??= error; }).finally(() => { samplingTask = null; });
  }, 100);
  const closeInspector = async () => { samplingPaused = true; await samplingTask; inspector?.close(); inspector = null; };
  try {
    worker = await openWorkerd(out, join(dir, 'do'), true); inspector = await inspect(worker); samplingPaused = false;
    const g = new Group(worker, native, 'mixed', [1]); await g.init(); await g.transfer(1); await g.create();
    assert(await g.read(1, 'after-create')); const row = await g.sameRows();
    await g.call(1, 'init', { cluster: 'mixed', id: '1', seed: '101', bootstrap: ['1','2','3'], scope: g.scope });
    await assert.rejects(g.call(1, 'init', { cluster: 'wrong', id: '1', seed: '101', bootstrap: ['1','2','3'], scope: g.scope }), /identity cannot change/);
    assert.deepEqual(await g.call(1, 'concurrent'), ['first-start','first-end','second']);
    g.block.add(1); assert.equal(await g.read(1, 'minority'), undefined); await g.tick(16); assert(g.leader() !== 1); assert(await g.read(g.leader(), 'majority'));
    g.block.clear(); await g.tick(14); await g.transfer(1);
    // Node 3 misses a suffix which the hosted leader compacts. Its recovery
    // must install the streamed snapshot before it can acknowledge MsgSnap.
    g.block.add(3); await g.mutate(2); await g.call(1, 'checkpoint'); g.block.clear(); await g.tick(4); await g.sameRows();
    assert(report.phases.includes('wasm snapshot -> native'));
    await g.transfer(2); g.block.add(1); await g.mutate(3); await g.call(2, 'checkpoint'); g.block.clear(); await g.tick(4); const latest = await g.sameRows();
    assert(report.phases.includes('native snapshot -> wasm'));
    await g.read(g.leader(), 'after-snapshots'); await g.transfer(1); await sample(); heaps.push(await inspector.call('Runtime.getHeapUsage'));
    await closeInspector(); await worker.dispose(); worker = null;
    worker = await openWorkerd(out, join(dir, 'do'), true); g.worker = worker; inspector = await inspect(worker); samplingPaused = false;
    // A new workerd process loads the actor and validates durable rows and
    // snapshot chunks. Recovery does not restore a leader or prior read lease.
    assert.equal(await g.call(1, 'row'), latest);
    g.accept(1, await g.call(1, 'input', { tag: 0 })); assert.equal(g.nodes.get(1).role, 0);
    g.accept(2, await g.call(2, 'restart')); g.accept(3, await g.call(3, 'restart')); await g.tick(16);
    assert(await g.read(g.leader(), 'recovered')); await g.sameRows();
    report.phases.push('killed workerd + reopened native SQLite + quorum barrier');
    // A second mixed topology exercises two wasm members, full joint state,
    // learners-next, checkpoint and a process restart before explicit leave.
    const j = new Group(worker, native, 'joint', [1,2]); await j.init(); await j.create();
    j.accept(4, await j.call(4, 'init', { cluster: 'joint', id: '4', seed: '104', bootstrap: ['1','2','3'], scope: j.scope }));
    await j.step(j.leader(), 8, b64(Uint8Array.of(18,4,8,3,16,4))); await j.pump(); await j.tick(2);
    await j.step(j.leader(), 8, b64(Uint8Array.of(8,2,18,4,8,3,16,3,18,4,8,0,16,4))); await j.pump(); await j.tick(2);
    const jointSnapshot = unb64(await j.call(1, 'checkpoint'));
    const jointManifest = JSON.parse(fields(jointSnapshot).get(1).toString());
    const jointConf = fields(unb64(jointManifest.conf)); assert(jointConf.has(3)); assert(jointConf.has(4));
    await closeInspector(); await worker.dispose(); worker = null;
    worker = await openWorkerd(out, join(dir, 'do'), true); inspector = await inspect(worker); samplingPaused = false; j.worker = worker; g.worker = worker;
    for (const id of [1,2]) { await j.call(id, 'row'); j.accept(id, await j.call(id, 'input', { tag: 0 })); }
    for (const id of [3,4]) j.accept(id, await j.call(id, 'restart'));
    await j.tick(16); assert(await j.read(j.leader(), 'joint-restart'));
    const restored = unb64(await j.call(1, 'checkpoint')); assert.equal(JSON.parse(fields(restored).get(1).toString()).conf, jointManifest.conf);
    await j.step(j.leader(), 8, ''); await j.pump(); await j.tick(2); assert(await j.read(j.leader(), 'left-joint')); await j.sameRows();
    const left = unb64(await j.call(1, 'checkpoint')); const leftConf = fields(unb64(JSON.parse(fields(left).get(1).toString()).conf)); assert(!leftConf.has(3)); assert(!leftConf.has(4));
    assert.notEqual(j.leader(), 3, 'removed voter remains a learner after leave/restart');
    // R6/R7/R8/R12 use the production planner, not literal voter edits, for
    // transfer completion and reverse restoration with P absent. The synthetic
    // policy header belongs only to this host-mechanism fixture; signed policy
    // application is covered separately by the actual directory evaluator.
    let header={revision:'3',index:j.nodes.get(j.leader()).applied,next_node:'5',anchors:['1','2','3'],nodes:['1','2','3','4'],live:1,limit:4096,leadership:{node:'4',standby:'3',revision:'3',barrier:j.nodes.get(j.leader()).applied,term:j.nodes.get(j.leader()).term,restore:false,settled:true,completed:false}};
    const plan=async(node,target)=>j.call(node,'leadership-plan',{header:b64(Buffer.from(encodeCanonical(header))),value:{node:String(target),revision:header.revision,applied:j.nodes.get(target).applied,protocol:1,durable:true}});
    const receipt=async node=>j.call(node,'leadership-receipt',{header:b64(Buffer.from(encodeCanonical(header)))});
    await j.tick(1);
    let p=await plan(j.leader(),4);assert.equal(p.transfer,'4');await j.step(j.leader(),7,p.transfer);await j.pump();assert.equal(j.leader(),4);
    await assert.rejects(receipt(4),/caught-up voter/);assert(await j.read(4,'native-personal-leader'));assert.equal((await receipt(4)).node,'4');
    header.leadership.completed=true;
    j.block.add(4);j.block.add(3);native.child.kill('SIGKILL');await new Promise(r=>native.child.once('exit',r));
    j.block.add(2);await j.tick(13);assert.equal(j.leader(),undefined,'R8 personal plus anchor loss must be unavailable');j.block.delete(2);
    for(let n=0;n<24&&!j.leader();n++)await j.tick(1);assert([1,2].includes(j.leader()));
    report.phases.push('R8 personal + anchor loss unavailable; no second writable cluster');
    const beforeFallbackWrite=j.nodes.get(j.leader()).applied;await j.mutate(3);assert(BigInt(j.nodes.get(j.leader()).applied)>BigInt(beforeFallbackWrite),'R7 A+B conditional operation did not commit');
    p=await plan(j.leader(),3);assert.equal(p.fallback,true);assert.equal(p.transfer,'0');
    native=new Native(join(dir,'native'));j.native=native;g.native=native;
    j.accept(3,await j.call(3,'init',{cluster:'joint',seed:'103',bootstrap:['1','2','3']}));j.block.delete(3);await j.pump();
    // C catches up through the committed restore barrier while P remains
    // absent. A+B alone decide both sides of the reverse joint transition.
    header={...header,revision:'4',index:j.nodes.get(j.leader()).applied,leadership:{...header.leadership,revision:'4',barrier:j.nodes.get(j.leader()).applied,restore:true,settled:false,completed:false}};
    await j.tick(1);
    p=await plan(j.leader(),3);assert(p.configure);await j.step(j.leader(),8,p.change);await j.pump();
    p=await plan(j.leader(),3);assert(p.configure);await j.step(j.leader(),8,p.change??'');await j.pump();
    p=await plan(j.leader(),3);assert(p.settle);assert(await j.read(j.leader(),'restored-without-personal'));
    const reverse=JSON.parse(fields(unb64(await j.call(1,'checkpoint'))).get(1).toString());
    assert(!fields(unb64(reverse.conf)).has(3));
    j.accept(4,await j.call(4,'init',{cluster:'joint',seed:'104',bootstrap:['1','2','3']}));j.block.delete(4);await j.tick(2);assert.notEqual(j.leader(),4);
    report.phases.push('R6 native P elected + current-term read; R7 A+B restore C with P absent');
    await j.transfer(3);await j.call(1,'close');await j.call(2,'close');
    // R6 reverses the runtime roles: P is WASI and two anchors are native.
    const k=new Group(worker,native,'wasm-personal',[1,4]);await k.init();
    k.accept(4,await k.call(4,'init',{cluster:k.cluster,id:'4',seed:'104',bootstrap:[],scope:k.scope}));await k.call(4,'enrollment',{view:k.view,applied:k.nodes.get(4).applied});
    await k.step(k.leader(),8,b64(Uint8Array.of(18,4,8,3,16,4)));await k.pump();await k.tick(2);
    let kh={revision:'3',index:k.nodes.get(k.leader()).applied,next_node:'5',anchors:['1','2','3'],nodes:['1','2','3','4'],live:1,limit:4096,leadership:{node:'4',standby:'3',revision:'3',barrier:k.nodes.get(k.leader()).applied,term:k.nodes.get(k.leader()).term,restore:false,settled:false,completed:false}};
    const kp=async()=>k.call(k.leader(),'leadership-plan',{header:b64(Buffer.from(encodeCanonical(kh))),value:{node:'4',revision:'3',applied:k.nodes.get(4).applied,protocol:1,durable:true}});
    p=await kp();assert(p.configure);await k.step(k.leader(),8,p.change);await k.pump();p=await kp();assert(p.configure);await k.step(k.leader(),8,p.change??'');await k.pump();await k.tick(2);for(let n=0;n<24&&!k.leader();n++)await k.tick(1);assert(k.leader());
    p=await kp();assert(p.settle);kh.leadership.settled=true;p=await kp();assert.equal(p.transfer,'4');await k.step(k.leader(),7,'4');await k.pump();assert.equal(k.leader(),4);assert(await k.read(4,'wasm-personal-read'));
    assert.equal((await k.call(4,'leadership-receipt',{header:b64(Buffer.from(encodeCanonical(kh)))})).node,'4');
    report.phases.push('R6 WASI P promoted + elected + current-term read');
    report.phases.push('two wasm + native peers; full joint checkpoint/restart/leave');
    // All three anchors plus two other identities share exactly one runtime.
    const h = new Group(worker, native, 'co-resident', [1,2,3]); await h.init(); await h.create(); assert(await h.read(h.leader(), 'all-hosted')); await h.sameRows();
    assert.equal(await g.call(1, 'row'), latest); assert.notEqual(row, latest);
    await h.transfer(1); const prior = await h.call(3, 'row'); await h.call(3, 'storage-fail');
    await h.step(1, 3, h.command({ release: { cell: 'cell', fence: '1', quiet: false } }, 2));
    await assert.rejects(h.pump(), /injected storage failure/); h.block.add(3); await h.pump();
    assert.equal(await h.call(3, 'row'), prior, 'failed hosted persist retains prior application');
    await assert.rejects(h.call(3, 'input', { tag: 1 }), /stopped/);
    assert(await h.read(1, 'after-store-failure'), 'healthy hosted majority still coordinates');
    report.phases.push('SQL failure stops hosted participation while healthy majority commits');
    const metrics = await (await worker.dispatchFetch('http://proof/metrics')).json(); heaps.push(await inspector.call('Runtime.getHeapUsage'));
    report.metrics = metrics; report.heapSamples = heaps;
    report.isolateBytes = Math.max(...heaps.map(h => h.totalSize + (h.embedderHeapUsedSize ?? 0) + (h.backingStorageSize ?? 0))) + metrics.peakMemory;
    assert(report.isolateBytes <= 96 * 1024 * 1024); assert(metrics.peakMemory <= 64 * 1024 * 1024); assert.equal(metrics.active, 6);
    report.phases.push('three hosted anchors plus mixed identity in same isolate');
  } finally {
    clearInterval(timer); await closeInspector(); await sample(); if (worker) await worker.dispose(); await native.close();
    report.combinedRSS = Math.max(...report.samples.map(s => s.combinedRSS));
    const cpu = new Map(); for (const sample of report.samples) for (const p of sample.processes) cpu.set(p.pid, Math.max(cpu.get(p.pid) ?? 0, p.cpuSeconds)); report.sampledCPUSeconds = [...cpu.values()].reduce((a, b) => a + b, 0);
    const times = report.calls.filter(c => c.wasm).map(c => c.ms).sort((a, b) => a - b); report.callMs = { p50: times[Math.floor(times.length * .5)], p95: times[Math.floor(times.length * .95)], p99: times[Math.floor(times.length * .99)] };
    report.coldFirstInitMs = report.calls.find(c => c.wasm && c.op === 'init')?.ms;
    await writeFile(join(out, 'actors-runtime.json'), JSON.stringify(report, null, 2) + '\n');
  }
  if (samplingError) throw samplingError;
  assert(report.coldFirstInitMs < 1000, 'R27 cold member activation stays within one second');
  assert(report.combinedRSS <= 256 * 1024 * 1024, `R27 combined RSS ${report.combinedRSS}`);
});
async function inspect(worker) {
  const base = await worker.getInspectorURL(), listing = await (await fetch(new URL('/json/list', base))).json();
  const target = listing.find(x => x.title?.includes('raft-proof')) ?? listing[0];
  const ws = new WebSocket(target.webSocketDebuggerUrl); await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject; });
  let serial = 0; const waiting = new Map();
  ws.onmessage = e => { const m = JSON.parse(e.data); if (!m.id) return; const p = waiting.get(m.id); if (!p) return; waiting.delete(m.id); clearTimeout(p.timer); m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result); };
  return { close: () => ws.close(), call: (method) => new Promise((resolve, reject) => { const id = ++serial, timer = setTimeout(() => { waiting.delete(id); reject(new Error('inspector timeout')); }, 5000); waiting.set(id, { resolve, reject, timer }); ws.send(JSON.stringify({ id, method })); }) };
}
