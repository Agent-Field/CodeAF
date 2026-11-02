import test from 'node:test';
import assert from 'node:assert/strict';
import { R2Store, sealFrame } from '../../src/store.js';
import { encode } from '../../src/frame.js';
import { memorySql } from '../sql.js';
import { boundLink, deliveredLink } from '../../src/link/effects.js';
import { RaftBlobTenant, RaftEffectsFacade } from '../../src/raft/effects.js';

const rid='1'.repeat(64), enc=new TextEncoder();
const frame=()=>sealFrame(encode('0'.repeat(32),[{rid,bytes:enc.encode('AGEO\x01sealed')} ]));
function bucket() {
  const data=new Map();
  return {data,async put(key,bytes){data.set(key,bytes.slice());},async get(key,opts){const b=data.get(key);if(!b)return null;const off=opts?.range?.offset??0,n=opts?.range?.length??b.length;return {arrayBuffer:async()=>b.slice(off,off+n).buffer};}};
}

test('R21 an upload receipt waits for blob completion, indexing and the storage durability boundary',async()=>{
  const b=bucket(),store=new R2Store(b,memorySql(),'one'),f=await frame();let release,started;
  const wait=new Promise(r=>{release=r;}),entered=new Promise(r=>{started=r;});const put=b.put;
  b.put=async(...args)=>{started();await wait;return put(...args);};
  const writing=store.put(f);await entered;let ready=false,synced=false;
  const checking=store.checkReferences(rid,[f.id],async()=>{synced=true;}).then(()=>{ready=true;});
  await Promise.resolve();assert.equal(ready,false);assert.equal(synced,false);
  release();await writing;await checking;assert(synced&&ready);
  await assert.rejects(store.checkReferences(rid,[f.id],async()=>{throw Error('sync failed');}),/sync failed/);
  b.data.delete('one/f/'+f.id);await assert.rejects(store.checkReferences(rid,[],async()=>{}));
});

test('R16 R21 a freeze or revoke during external I/O leaves bytes and returns no upload success',async()=>{
  const b=bucket(),store=new R2Store(b,memorySql(),'one'),f=await frame();let calls=0;
  const guarded=new RaftBlobTenant(store,async()=>{if(++calls===2)throw Error('revoked');},async()=>{});
  await assert.rejects(guarded.putFrame(f),/revoked/);
  assert(b.data.has('one/f/'+f.id));assert.deepEqual(await store.get(rid),enc.encode('AGEO\x01sealed'));
});

test('R16 R17 one public request belongs to one namespace and one exact committed delivery',()=>{
  const pending={device:'device',state:'pending',grant:null,expires_at:10000};const r=boundLink(pending,'one');
  assert.equal(boundLink(r,'one'),r);assert.throws(()=>boundLink(r,'two'));
  assert.throws(()=>deliveredLink({...r,device:'replacement'},'one','id','approved','c2VhbGVk',1,'device'));
  assert.throws(()=>deliveredLink(r,'two','id','approved','c2VhbGVk',1,'device'));
  const done=deliveredLink(r,'one','id','approved','c2VhbGVk',1,'device');
  assert.equal(deliveredLink(done,'one','id','approved','c2VhbGVk',2,'device'),done);
  assert.throws(()=>deliveredLink(done,'one','other','approved','c2VhbGVk',2,'device'));
  assert.throws(()=>deliveredLink(done,'one','id','approved','b3RoZXI',2,'device'));
  assert.throws(()=>deliveredLink(done,'one','id','denied',null,2,'device'));
});

test('R17 expired public delivery records a terminal outcome and never invents a new grant',async()=>{
  const {encodeCanonical}=await import('../../src/raft/codec.js');
  const decision={code:'k7m2q9xd',device:'',record:{V:0,name:'',added_by:'',revoked:false,caps:{os:'',arch:'',sandbox:null,container:null,gpu:null,cow:''}},grant:'',cert:'',deny:true};
  const row=enc.encode(encodeCanonical({device:'pending',expires:1,decision}));let outcome;
  const effects=new RaftEffectsFacade({async barrierRecord(){return row;},async commitEffect(change){outcome=change.complete_join;return {code:'ok'};}},'one');
  await effects.deliver({async deliver(){return {err:'gone',status:404};}},decision.code,2);
  assert.equal(outcome.outcome,'gone');assert.match(outcome.decision,/^[0-9a-f]{64}$/);
});

test('R21 an interrupted R2 index has no completion marker and a repeated immutable upload repairs it',async()=>{
  const sql=memorySql(),original=sql.exec.bind(sql);let fail=true;
  sql.exec=(q,...args)=>{if(fail&&q.startsWith('INSERT OR IGNORE INTO objs')){fail=false;throw Error('index failed');}return original(q,...args);};
  const store=new R2Store(bucket(),sql,'one'),f=await frame();
  await assert.rejects(store.put(f),/index failed/);assert.equal(await store.put(f),true);
  await store.checkReferences(rid,[f.id],async()=>{});assert.equal(await store.put(f),false);
});
