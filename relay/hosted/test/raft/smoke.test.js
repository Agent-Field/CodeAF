import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { readFile, writeFile, mkdtemp, open } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { tmpdir } from 'node:os';
import { performance } from 'node:perf_hooks';
import { createHash } from 'node:crypto';
import { openWorkerd } from './workerd.js';
import { Writer, Reader, createFrame, stepFrame, frame, response, effects } from '../../src/raft/abi.js';
import { encodeCanonical, decodeCanonical } from '../../src/raft/codec.js';

const clockTicks=Number(execFileSync('getconf',['CLK_TCK'],{encoding:'utf8'}).trim());
const out = resolve(process.env.CODEAF_RAFT_PROOF_DIR ?? '/tmp/codeaf-p03');
const text = new TextEncoder();
const canonical = v => text.encode(encodeCanonical(v));
const parse = bytes => decodeCanonical(new TextDecoder().decode(bytes));
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
async function processSample() {
  const ids=[process.pid,...(await readFile(`/proc/${process.pid}/task/${process.pid}/children`,'utf8')).trim().split(/\s+/).filter(Boolean).map(Number)];
  const rows=[];
  for(const pid of ids){try{const status=await readFile(`/proc/${pid}/status`,'utf8');const stat=(await readFile(`/proc/${pid}/stat`,'utf8')).split(') ').slice(1).join(') ').split(' ');rows.push({cpuSeconds:(Number(stat[11])+Number(stat[12]))/clockTicks,pid,name:status.match(/^Name:\s+(.*)$/m)?.[1],rssBytes:Number(status.match(/^VmRSS:\s+(\d+)/m)?.[1]??0)*1024,peakRSSBytes:Number(status.match(/^VmHWM:\s+(\d+)/m)?.[1]??0)*1024});}catch{}}
  return {processes:rows,combinedRSSBytes:rows.reduce((sum,r)=>sum+r.rssBytes,0)};
}
const report = { allocator:{arenas:process.env.MALLOC_ARENA_MAX,trimThreshold:process.env.MALLOC_TRIM_THRESHOLD_}, laptopSamples:[], requirements: ['R3', 'R4', 'R14', 'R27', 'R28'], phases: [], importCalls: {}, measurements: [] };

class Native {
  constructor(path) {
    this.child = spawn(path, [], { stdio: ['pipe', 'pipe', 'inherit'] });
    this.buffer = Buffer.alloc(0); this.waiter = undefined;
    this.child.stdout.on('data', b => { this.buffer = Buffer.concat([this.buffer, b]); this.pump(); });
    this.child.on('error', err => this.waiter?.reject(err));
    this.child.on('exit', code => { if (this.waiter) this.waiter.reject(new Error(`native peer exit ${code}`)); });
  }
  pump() {
    if (!this.waiter || this.buffer.length < 4) return;
    const n = this.buffer.readUInt32LE(0); assert(n <= 2*1024*1024);
    if (this.buffer.length < n + 4) return;
    const b = new Uint8Array(this.buffer.subarray(4,n+4)); this.buffer = this.buffer.subarray(n+4);
    const waiter = this.waiter; this.waiter = undefined; clearTimeout(waiter.timer); waiter.resolve(b);
  }
  call(b) {
    assert(!this.waiter, 'one synchronous ABI call per native runtime');
    return new Promise((resolve, reject) => { this.waiter = { resolve, reject, timer:setTimeout(()=>{this.waiter=undefined;reject(new Error('native ABI call exceeded 5 seconds'));},5000) }; const h = Buffer.alloc(4); h.writeUInt32LE(b.length); this.child.stdin.write(h); this.child.stdin.write(b); this.pump(); });
  }
  async close() { this.child.stdin.end();if(this.child.exitCode===null)await new Promise(resolve=>this.child.once('exit',resolve)); }
}

// Protobuf inspection is transport/storage framing only. Application meaning
// is evaluated by Go, and the independent consensus checker remains P06.
function fields(bytes) {
  let at = 0; const result = new Map();
  const vint = () => { let n=0n, shift=0n; for(let i=0;i<10;i++){assert(at<bytes.length);const b=bytes[at++];n|=BigInt(b&127)<<shift;if(!(b&128))return n;shift+=7n;}throw new Error('protobuf varint'); };
  while(at<bytes.length) {const key=vint(), tag=Number(key>>3n), kind=Number(key&7n);let value;if(kind===0)value=vint();else if(kind===2){const n=Number(vint());assert(at+n<=bytes.length);value=bytes.slice(at,at+n);at+=n;}else throw new Error('unexpected protobuf wire type');result.set(tag,value);}
  return result;
}
const entryIndex = b => fields(b).get(3) ?? 0n;
function checkpoint(d) {
  const w = new Writer().blob(d.hard).blob(d.conf).blob(d.snapshot).u32(d.entries.length);
  for(const e of d.entries)w.blob(e);
  return w.u64(d.applied).blob(d.row).finish();
}
function recover(bytes) { const r = new Reader(bytes);const d={hard:r.blob(),conf:r.blob(),snapshot:r.blob(),entries:r.entries(),applied:r.u64(),row:r.blob()};r.end();return d; }
const empty = () => ({ hard:new Uint8Array(),conf:new Uint8Array(),snapshot:new Uint8Array(),entries:[],applied:0n,row:new Uint8Array() });
const decodeHandle = b => {const r=response(b),h=r.u32();r.end();return h;};
const decodeBlob = b => {const r=response(b),v=r.blob();r.end();return v;};

async function startWorker(persist) {
  const start = performance.now();
  const mf = await openWorkerd(out,persist);
  report.measurements.push({ workerReadyMs:performance.now()-start });
  const call = async b => {const start=performance.now();const res=await mf.dispatchFetch('http://proof/call',{method:'POST',body:b,signal:AbortSignal.timeout(5000)});if(!res.ok)throw new Error(await res.text());const bytes=new Uint8Array(await res.arrayBuffer());report.measurements.push({callMs:performance.now()-start});return bytes;};
  return {mf,call,
    save:async(id,b)=>{const res=await mf.dispatchFetch(`http://proof/store/${id}/checkpoint`,{method:'PUT',body:b});assert.equal(res.status,204);},
    load:async id=>{const res=await mf.dispatchFetch(`http://proof/store/${id}/checkpoint`);assert.equal(res.status,200);return new Uint8Array(await res.arrayBuffer());},
    metrics:async()=>{const res=await mf.dispatchFetch('http://proof/metrics');return res.json();},
  };
}

async function inspector(mf) {
  const base = await mf.getInspectorURL();
  const listing = await (await fetch(new URL('/json/list',base))).json();
  const worker = listing.find(x=>x.title?.includes('raft-proof')) ?? listing[0];
  assert(worker?.webSocketDebuggerUrl,'workerd inspector is required for isolate memory proof');
  const ws = new WebSocket(worker.webSocketDebuggerUrl);
  await new Promise((resolve,reject)=>{ws.onopen=resolve;ws.onerror=reject;});
  let serial=0;const waiting=new Map();
  ws.onmessage=e=>{const m=JSON.parse(e.data);if(m.id){const p=waiting.get(m.id);waiting.delete(m.id);if(m.error)p.reject(new Error(JSON.stringify(m.error)));else p.resolve(m.result);}};
  return {call:(method,params={})=>new Promise((resolve,reject)=>{const id=++serial;waiting.set(id,{resolve:v=>{clearTimeout(timer);resolve(v);},reject:e=>{clearTimeout(timer);reject(e);}});const timer=setTimeout(()=>{waiting.delete(id);reject(new Error('inspector call exceeded 5 seconds'));},5000);ws.send(JSON.stringify({id,method,params}));}),close:()=>ws.close()};
}

class Group {
  constructor(worker,native,dir,wasmIDs=[1],cluster='p03-cluster'){this.worker=worker;this.native=native;this.dir=dir;this.wasmIDs=wasmIDs;this.nodes=new Map();this.queue=[];this.reads=[];this.trace=[];this.block=false;this.cluster=cluster;}
  async create(id,recovered) {
    const endpoint = this.wasmIDs.includes(id)?this.worker:this.native;
    const cfg={cluster:this.cluster,id:BigInt(id),seed:BigInt(100+id),bootstrap:recovered?[]:[1n,2n,3n],...(recovered??{})};
    const handle=decodeHandle(await endpoint.call(createFrame(cfg)));
    const shadow=decodeHandle(await this.native.call(createFrame(cfg)));
    this.nodes.set(id,{id,endpoint,handle,shadow,d:recovered??empty()});
  }
  async call(id,tag,value){
    const n=this.nodes.get(id),bytes=await n.endpoint.call(stepFrame(n.handle,tag,value));
    const expected=await this.native.call(stepFrame(n.shadow,tag,value));
    assert.deepEqual(bytes,expected,`R14 identical effects node ${id} input ${tag}`);
    this.trace.push(sha(bytes));report.totalParityCalls=(report.totalParityCalls??0)+1;return effects(bytes);
  }
  async save(n){
    const bytes=checkpoint(n.d);
    if(this.wasmIDs.includes(n.id))await this.worker.save(`${this.cluster}-${n.id}`,bytes);
    else {const f=await open(join(this.dir,`native-${this.cluster}-${n.id}`),'w');try{await f.writeFile(bytes);await f.sync();}finally{await f.close();}}
  }
  async accept(id,e,depth=0){
    assert(depth<100,'bounded Ready completions');const n=this.nodes.get(id);n.status=e.status;
    this.queue.push(...e.send.map(m=>({...m,from:id})));this.reads.push(...e.reads.map(r=>({...r,node:id})));
    if(e.persist){
      const p=e.persist;if(p.hard.length)n.d.hard=p.hard;
      assert.equal(p.snapshot.length,0,'P05 snapshot lifecycle remains outside P03');
      if(p.entries.length){const first=entryIndex(p.entries[0]);n.d.entries=n.d.entries.filter(b=>entryIndex(b)<first).concat(p.entries);}
      await this.save(n);
      await this.accept(id,await this.call(id,5,p.id),depth+1);
    }
    if(e.apply){
      const a=e.apply;
      for(const entry of a.entries){const f=fields(entry);if((f.get(1)??0n)===0n && (f.get(4)?.length??0)>0){
        const command=f.get(4);
        const req=frame(4).blob(command).blob(n.d.row).finish();
        const got=await n.endpoint.call(req),want=await this.native.call(req);
        assert.deepEqual(got,want,'R14 same Go lease application');n.d.row=decodeBlob(got);
      }}
      n.d.conf=a.conf;n.d.applied=a.index;await this.save(n);
      await this.accept(id,await this.call(id,6,a.id),depth+1);
    }
  }
  async step(id,tag,value){await this.accept(id,await this.call(id,tag,value));}
  async pump(){for(let count=0;this.queue.length;count++){assert(count<2000,'bounded network schedule');const m=this.queue.shift();if(this.block&&(m.from===this.isolated||Number(m.to)===this.isolated))continue;await this.step(Number(m.to),2,m.bytes);}}
  async tick(n=1){for(let i=0;i<n;i++){for(const id of this.nodes.keys())await this.step(id,1);await this.pump();}}
  leader(){return [...this.nodes.values()].find(n=>n.status?.role===2)?.id;}
  async read(id,request){await this.step(id,4,request);await this.pump();return this.reads.find(r=>r.id===request);}
}

function command(change,sequence=1n,actor='device-a',now=1000){
 return canonical({schema:1,cluster:'p03-cluster',actor,session:'proof',sequence:String(sequence),request_digest:'sealed-command-digest',now,change,signature:'AQID'});
}

// R3/R4/R14/R27: actual workerd, patched production cores, real storage,
// identical seeded effects, a current-term quorum read and process restart.
test('R3 R4 R14 R27 mixed Worker/native command, ReadIndex and durable restart', {timeout:120000}, async()=>{
 const dir=await mkdtemp(join(tmpdir(),'codeaf-p03-'));const native=new Native(join(out,'native-peer'));
 let worker,inspect,sampling=false;const heapSamples=[];
 const monitor=setInterval(async()=>{if(sampling)return;sampling=true;try{report.laptopSamples.push(await processSample());if(inspect)heapSamples.push(await inspect.call('Runtime.getHeapUsage'));}finally{sampling=false;}},200);
 try {
  worker=await startWorker(join(dir,'do'));inspect=await inspector(worker.mf);
  const samples=heapSamples;const sample=async()=>{const h=await inspect.call('Runtime.getHeapUsage');samples.push(h);return h;};
  await inspect.call('Profiler.enable');await inspect.call('Profiler.start');
  let g=new Group(worker,native,dir);
  await g.create(1);
  report.coldFirstCallMs=report.measurements.find(x=>x.callMs!==undefined).callMs;
  assert(report.coldFirstCallMs<1000,'R27 cold instantiate/initialize/call stays within 1 s startup envelope');
  for(let id=2;id<=3;id++)await g.create(id);
  for(let id=1;id<=3;id++)await g.step(id,0);
  await g.pump();await g.tick(14);assert(g.leader());
  let leader=g.leader();if(leader!==1){await g.step(leader,7,1n);await g.pump();await g.tick(2);}
  assert.equal(g.leader(),1,'wasm is the actual leader');
  const create=command({create:{cell:'cell-proof',init:{head:'sealed-head',class:'work',size:'7',title:'sealed-title',keys:{}},receipt:'durable-receipt'}});
  await g.step(1,3,create);await g.pump();
  const read=await g.read(1,'after-create');assert(read&&read.index>0n,'ReadIndex barrier returned');
  for(const n of g.nodes.values()){const row=parse(n.d.row);assert.equal(row.cell.lease.fence,'1');assert.equal(row.cell.head,'sealed-head');assert(n.d.applied>=read.index);}
  await sample();
  // Withhold all native acknowledgments: a remembered Worker leader must
  // not satisfy a fresh read before or after CheckQuorum steps it down.
  g.block=true;g.isolated=1;assert.equal(await g.read(1,'minority'),undefined);await g.tick(14);
  assert.equal(g.reads.find(r=>r.id==='minority'),undefined);assert.notEqual(g.nodes.get(1).status.role,2);
  g.block=false;await g.tick(14);leader=g.leader();assert(leader);
  assert(await g.read(leader,'healed'));
  const before=g.nodes.get(1).d.row;const persisted=recover(await worker.load('p03-cluster-1'));assert.deepEqual(persisted.row,before);
  const saved=[];for(let id=1;id<=3;id++)saved.push(id===1?persisted:recover(new Uint8Array(await readFile(join(dir,`native-p03-cluster-${id}`)))));
  report.traceDigest=sha(text.encode(g.trace.join('\n')));report.parityCalls=g.trace.length;
  report.phases.push('wasm leader command + quorum ReadIndex; minority read refused; native fallback');
  const profile=await inspect.call('Profiler.stop');report.cpuProfileSamples=profile.profile.samples?.length??0;
  const functions=new Map(profile.profile.nodes.map(n=>[n.id,n.callFrame.functionName]));
  report.activeProfileMicros=(profile.profile.samples??[]).reduce((sum,id,i)=>sum+(functions.get(id)==='(idle)'?0:(profile.profile.timeDeltas?.[i]??0)),0);
  report.firstRuntime=await worker.metrics();await sample();inspect.close();inspect=undefined;await worker.mf.dispose();worker=undefined;
  // A completely new workerd process recovers only durable storage bytes.
  worker=await startWorker(join(dir,'do'));inspect=await inspector(worker.mf);
  const reloaded=recover(await worker.load('p03-cluster-1'));assert.deepEqual(checkpoint(reloaded),checkpoint(saved[0]));
  g=new Group(worker,native,dir,[1,2]);
  for(let id=1;id<=3;id++)await g.create(id,saved[id-1]);
  for(let id=1;id<=3;id++)await g.step(id,0);await g.pump();await g.tick(14);
  leader=g.leader();assert(leader);assert(await g.read(leader,'restarted'));
  for(const n of g.nodes.values())assert.deepEqual(n.d.row,before,'committed row survived process restart');
  report.phases.push('new workerd process storage restart; two wasm + one native parity');
  samples.push(await inspect.call('Runtime.getHeapUsage'));
  // Three anchors and a second identity share the same Go runtime. The
  // oversized identity remains in the host store and traverses one row at a
  // time; no whole-identity JSON/string is passed into the reactor.
  const identityCheckpoint=await worker.load('p03-cluster-1');
  await proveCoResident(worker,native,inspect,samples);
  assert.deepEqual(await worker.load('p03-cluster-1'),identityCheckpoint,'co-resident identity cannot overwrite the first checkpoint');
  report.finalRuntime=await worker.metrics();
  report.heapSamples=samples;
  report.observedIsolateBytes=Math.max(...samples.map(h=>h.totalSize+(h.embedderHeapUsedSize??0)+(h.backingStorageSize??0)))+report.finalRuntime.peakMemory;
  assert(report.observedIsolateBytes<=96*1024*1024,'R27 observed isolate envelope ≤96 MiB');
  assert(report.finalRuntime.peakMemory<=64*1024*1024);
  assert.equal(report.finalRuntime.wasi.poll_oneoff??0,0,'no blocking runtime poll');
  assert.equal(report.finalRuntime.wasi.sched_yield??0,0,'no scheduler loop');
  report.phases.push('co-resident handles; oversized rows measured by the sequential native driver');
  report.laptopSamples.push(await processSample());
  report.observedCombinedRSSBytes=Math.max(...report.laptopSamples.map(s=>s.combinedRSSBytes));
  assert(report.observedCombinedRSSBytes<=256*1024*1024,'R27 combined local process RSS ≤256 MiB');
 } finally {
  clearInterval(monitor);report.laptopSamples.push(await processSample());
  inspect?.close();if(worker)await worker.mf.dispose();await native.close();
  report.observedCombinedRSSBytes=Math.max(...report.laptopSamples.map(s=>s.combinedRSSBytes));
  report.nodePeakRSSBytes=process.resourceUsage().maxRSS*1024;
  const cpuByPID=new Map();for(const sample of report.laptopSamples)for(const p of sample.processes)cpuByPID.set(p.pid,Math.max(cpuByPID.get(p.pid)??0,p.cpuSeconds));
  report.sampledTotalCPUSeconds=[...cpuByPID.values()].reduce((a,b)=>a+b,0);
  await writeFile(join(out,'runtime.json'),JSON.stringify(report,null,2)+'\n');
 }
});

async function proveCoResident(worker,native,inspect,samples){
 // Admission preserves an imported ceiling, never truncating or discarding
 // rows. This fixture exercises the representation, not P11 migration policy.
 const group=new Group(worker,native,await mkdtemp(join(tmpdir(),'codeaf-p03-extra-')),[1,2,3],'p03-other-identity');
 for(let id=1;id<=3;id++)await group.create(id);
 for(let id=1;id<=3;id++)await group.step(id,0);
 await group.pump();await group.tick(12);assert(group.leader());assert(await group.read(group.leader(),'other-identity'));
 samples.push(await inspect.call('Runtime.getHeapUsage'));
}
