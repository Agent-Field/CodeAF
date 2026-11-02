import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { spawn } from 'node:child_process';
import { createHash, generateKeyPairSync, sign } from 'node:crypto';
import { Reactor, HostCodec } from '../../src/raft/abi.js';
import { Directory } from '../../src/directory.js';
import { LegacySource, importLegacy, convertLegacy } from '../../src/raft/legacy.js';
import { MemberStorage, bytes, b64 } from '../../src/raft/storage.js';
import { encodeCanonical, decodeCanonical } from '../../src/raft/codec.js';
import { policyOf } from '../../src/rules.js';
const out=process.env.CODEAF_RAFT_PROOF_DIR??'/tmp/codeaf-p11';
const enc=new TextEncoder(),dec=new TextDecoder();
const sha=b=>createHash('sha256').update(b).digest('hex');
const config=()=>{
 const key=generateKeyPairSync('ed25519'), effects=generateKeyPairSync('ed25519'), device=generateKeyPairSync('ed25519');
 const publicKey=key.publicKey.export({format:'der',type:'spki'}).subarray(-32),pub=device.publicKey.export({format:'der',type:'spki'}).subarray(-32);
 const cert={V:1,device:pub.toString('hex'),created:1000,sig:''};cert.sig=sign(null,Buffer.concat([Buffer.from('codeaf/device-cert/v1\n'),Buffer.from(JSON.stringify(cert))]),key.privateKey).toString('hex');
 return {cluster:'r24-hosted',generation:'1',identity:'id_'+sha(publicKey).slice(0,32),signing_key:publicKey.toString('base64'),effects_key:effects.publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64'),anchors:['1','2','3'],now:200000,certificates:[Buffer.from(JSON.stringify(cert)).toString('base64url')]};
};
class Host{
 constructor(){this.db=new DatabaseSync(':memory:');this.sql={exec:(q,...p)=>{const rows=this.db.prepare(q).all(...p);return {toArray:()=>rows,one:()=>{assert.equal(rows.length,1);return rows[0];},[Symbol.iterator]:()=>rows[Symbol.iterator]()};}};this.gate=null;}
 transactionSync(fn){this.db.exec('BEGIN');try{const v=fn();this.db.exec('COMMIT');return v;}catch(e){this.db.exec('ROLLBACK');throw e;}}
 async sync(){if(this.gate)await this.gate;}
}
class Native{
 constructor(){this.child=spawn(out+'/native-peer',[],{stdio:['pipe','pipe','inherit'],env:{...process.env,GOMEMLIMIT:'32MiB'}});this.buffer=Buffer.alloc(0);this.child.stdout.on('data',b=>{this.buffer=Buffer.concat([this.buffer,b]);this.pump();});}
 pump(){if(!this.waiter||this.buffer.length<4)return;const n=this.buffer.readUInt32LE();assert(n<=2*1024*1024);if(this.buffer.length<n+4)return;const b=this.buffer.subarray(4,n+4);this.buffer=this.buffer.subarray(n+4);const w=this.waiter;this.waiter=null;clearTimeout(w.timer);w.resolve(b);}
 call(b){return new Promise((resolve,reject)=>{assert(!this.waiter);this.waiter={resolve,reject,timer:setTimeout(()=>reject(Error('native migration ABI timeout')),5000)};const h=Buffer.alloc(4);h.writeUInt32LE(b.length);this.child.stdin.write(h);this.child.stdin.write(b);this.pump();});}
 async close(){this.child.stdin.end();if(this.child.exitCode===null)await new Promise(r=>this.child.once('exit',r));}
}

// R24 compares every original SQL row through wasm and native conversion,
// preserves watch evidence, and checks actual portable genesis/checkpoint bytes.
test('R24 R14 R28 hosted source freezes stale handles and imports identical three-voter genesis',{timeout:30000},async()=>{
 const wasm=new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm'))),calls=[],runtime={call(b){const r=wasm.call(b);calls.push([b.slice(),r.slice()]);return r;}},codec=new HostCodec(runtime),host=new Host(),cfg=config();
 let native;
 const watchers={broadcast(){},announce(){},closeDevice(){},closeAll(){},vouchedUntil:()=>290000};
 const directory=new Directory(host.sql,cfg.identity,()=>200000,policyOf(),watchers);
 directory.write('identity','',{V:1,identity:cfg.identity,vault:'opaque-vault',rotation:{state:'retired',by:'a',at:1000,retire_at:900000}});
 directory.write('devices','a',{V:1,name:'c2VhbGVk',added_by:cfg.identity,revoked:true,caps:{os:'linux',arch:'amd64',sandbox:null,container:null,gpu:null,cow:'none'},created:100,last_seen:200});
 const raw='{"V":1,"head":"head","durable_at":1000,"class":"host-bound","size":71,"keys":{"key":{"identity":"wrapped"}},"lease":{"device":"a","fence":9007199254740991,"expires":91000,"pending":3},"title":"c2VhbGVk","frames":["sealed-frame"],"parent_cell":"parent","orphan_turns":2,"archived":true}';
 host.sql.exec('INSERT INTO dir VALUES(?,?,?)','cells','chat',raw);
 const stale=new Directory(host.sql,cfg.identity,()=>200000,policyOf(),watchers),source=new LegacySource(host,directory,runtime),stores=[];
 try{
  native=new Native();
  let release;host.gate=new Promise(r=>release=r);let complete=false;const freezing=source.freeze(cfg).then(()=>complete=true);await new Promise(r=>setImmediate(r));assert.equal(complete,false);assert.throws(()=>stale.release('chat','a',9007199254740991),e=>e.status===503);release();host.gate=null;await freezing;
  const m=await source.inventory(),again=await source.inventory();assert.deepEqual(again,m);assert.equal(m.version,'2');
  await assert.rejects(source.own([{node:'1',applied:'1',manifest:m}]));assert.equal(source.gate().stage,'inventoried');
  const snapshots=[];
  for(const id of ['1','2','3']){
   const h=new Host();stores.push(h);const store=new MemberStorage(h,codec);await store.open({cluster:cfg.cluster,id,protocol:1},Number(m.ceiling));
   const snapshot=await importLegacy(store,m,()=>source.rows());snapshots.push(snapshot);
   assert.deepEqual(await importLegacy(store,m,()=>{throw Error('retry reread source');}),snapshot);
   const recovered=store.recover();assert.equal(recovered.applied,1n);assert.equal(store.application().version,m.version);
   const cell=decodeCanonical(dec.decode(store.read('state','cell/'+Buffer.from('chat').toString('hex'))));assert.equal(cell.lease.fence,'9007199254740991');assert.equal(cell.lease.expires,290000);assert.deepEqual(cell.frames,['sealed-frame']);assert.equal(cell.archived,true);
  }
  assert.deepEqual(snapshots[0],snapshots[1]);assert.deepEqual(snapshots[1],snapshots[2]);
  await source.own([{node:'1',applied:'2',manifest:m},{node:'2',applied:'2',manifest:m}]);assert.equal(source.gate().stage,'raft-owned');assert.equal(source.route(m=>m.config.cluster),cfg.cluster);
  const reopened=new LegacySource(host,stale,runtime);await reopened.freeze(cfg);assert.deepEqual(await reopened.inventory(),m);await assert.rejects(reopened.freeze({...cfg,cluster:'new'}));
  assert.equal(host.sql.exec("SELECT doc FROM dir WHERE kind='cells'").toArray()[0].doc,raw);
  for(const [input,expected]of calls)assert.deepEqual(new Uint8Array(await native.call(input)),expected,'native/wasm migration ABI parity');
 }finally{if(native)await native.close();for(const h of stores)h.db.close();host.db.close();}
});

test('R24 R28 oversized and unknown legacy fields refuse without truncation; old releases stay released',async()=>{
 const runtime=new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm'))),cfg=config();
 const released='{"V":1,"head":"head","size":1,"class":"chat","keys":{},"lease":{"device":"a","fence":7,"expires":0,"pending":0,"released_at":80}}';
 const row=convertLegacy(runtime,cfg,'cells','released',released,900000);const cell=decodeCanonical(dec.decode(row.data));assert.equal(cell.lease.expires,0);assert.equal(cell.lease.released_at,80);
 assert.throws(()=>convertLegacy(runtime,cfg,'cells','new',released.slice(0,-1)+',"future":"keep-me"}'));
 const host=new Host(),dir=new Directory(host.sql,cfg.identity,()=>200000,policyOf()),source=new LegacySource(host,dir,runtime);
 const original=JSON.stringify({V:1,title:'s'.repeat(1024*1024+1)});host.sql.exec('INSERT INTO dir VALUES(?,?,?)','cells','large',original);
 await source.freeze(cfg);await assert.rejects(source.inventory(),/cells record "large".*1048576/);assert.equal(source.gate().stage,'frozen');assert.equal(host.sql.exec("SELECT doc FROM dir WHERE kind='cells'").toArray()[0].doc,original);assert.throws(()=>dir.list(),e=>e.status===503);host.db.close();
});

test('R24 freeze drains admitted legacy effects before inventory and refuses subsequent admissions',async()=>{
 const {LegacyAdmission}=await import('../../src/raft/legacy.js');
 const runtime=new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm'))),cfg=config(),host=new Host(),admission=new LegacyAdmission(host),dir=new Directory(host.sql,cfg.identity,()=>200000,policyOf()),source=new LegacySource(host,dir,runtime,admission);
 let release;const blocked=new Promise(r=>release=r),admitted=admission.run(async()=>{await blocked;dir.setVault('','last-admitted-vault');});
 const freezing=source.freeze(cfg);await assert.rejects(admission.run(()=>dir.setVault('','late')),e=>e.status===503);
 assert.equal(source.gate(),null);release();await admitted;await freezing;await source.inventory();
 const identity=[...source.rows()].find(r=>r.key==='identity');assert.equal(decodeCanonical(dec.decode(identity.data)).vault,'last-admitted-vault');host.db.close();
});

test('R24 R28 staged cross-object rows resume after reopen and preserve a grandfathered identity',async()=>{
 const {stageLegacyImport,finishLegacyImport}=await import('../../src/raft/legacy.js');
 const runtime=new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm'))),codec=new HostCodec(runtime),cfg=config(),host=new Host(),directory=new Directory(host.sql,cfg.identity,()=>200000,policyOf()),source=new LegacySource(host,directory,runtime);
 const title='s'.repeat(300000);for(let i=0;i<30;i++)directory.write('cells',String(i),{V:1,head:'head',class:'host-bound',size:1,keys:{},lease:{device:'a',fence:1,expires:0,pending:0},title});
 await source.freeze(cfg);const m=await source.inventory();assert(BigInt(m.ceiling)>8n*1024n*1024n);
 const target=new Host();let store=new MemberStorage(target,codec);await store.open({cluster:cfg.cluster,id:'1',protocol:1},Number(m.ceiling));
 let n=0;
 for(const row of source.rows()){
  await stageLegacyImport(store,m,row);await stageLegacyImport(store,m,row);
  if(++n===3){store=new MemberStorage(target,codec);assert.equal((await store.open({cluster:cfg.cluster,id:'1',protocol:1})).applied,0n);assert.equal(store.application().count,0);}
 }
 const snapshot=await finishLegacyImport(store,m);assert(snapshot.length>0);assert.equal(store.application().count,Number(m.rows)+1);assert(store.application().size<=Number(m.ceiling));assert.equal(store.one("SELECT count(*) AS n FROM sqlite_master WHERE name LIKE 'legacy_stage%'").n,0);
 // A cold opener uses the persisted imported ceiling even with the default
 // argument, so same-sized or shrinking updates remain possible.
 store=new MemberStorage(target,codec);assert.equal((await store.open({cluster:cfg.cluster,id:'1',protocol:1})).applied,1n);assert.equal(store.identity.ceiling,Number(m.ceiling));assert.equal([...source.rows()].length,Number(m.rows));target.db.close();host.db.close();
});
