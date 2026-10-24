import { frame, response, HostCodec } from './abi.js';
import { encodeCanonical, decodeCanonical } from './codec.js';
import { ROW_BYTES, DEFAULT_ROWS_BYTES, bytes, b64, unb64, hex } from './storage.js';
import { Wire } from '../wire.js';

const enc = new TextEncoder(), dec = new TextDecoder('utf-8', {fatal:true});
const data = v => enc.encode(encodeCanonical(v));
const check = v => { if (!v) throw new Error('codeaf: directory upgrade is incomplete; retry shortly; saved chats are still here'); };
const size = r => 12 + enc.encode(r.namespace).length + enc.encode(r.key).length + r.data.length;
const xor = (a,b) => { for(let i=0;i<32;i++) a[i]^=b[i]; };
const importKey = 'legacy-import';
const reserve = 4096;

// Original JSON bytes go to the shared Go conversion function, including every
// numeric fence and sealed field. JavaScript never evaluates a lease rule.
export function convertLegacy(runtime, config, kind, id, raw, until=0) {
  const r = response(runtime.call(frame(12).blob(data(config)).str(kind).str(id).blob(enc.encode(raw)).u64(until).finish()));
  const row = decodeCanonical(dec.decode(r.blob())); r.end();
  return { namespace:row.namespace, key:row.key, data:unb64(row.data) };
}

// Admission spans the entire legacy request, including pairing/blob awaits.
// Freeze first stops new admissions, then waits for admitted effects to finish.
// A failed freeze stays unavailable until recovery; it never opens two writers.
export class LegacyAdmission {
  constructor(storage) { this.storage=storage;this.pending=new Set();this.frozen=false; }
  run(fn) {
    if(this.frozen)return Promise.reject(new Wire('unreachable',503));
    const p=Promise.resolve().then(fn);this.pending.add(p);
    return p.finally(()=>this.pending.delete(p));
  }
  async drain(fn) { this.frozen=true;await Promise.allSettled([...this.pending]);return fn(); }
}

// The source object serializes migration with admitted request handlers. Its
// durable gate is checked again by the legacy writer, including stale handles.
// Awaiting sync precedes bootstrap, ownership publication and every reply.
export class LegacySource {
  constructor(storage, directory, runtime, admission) {
    this.storage=storage;this.admission=admission;this.dir=directory;this.sql=storage.sql;this.runtime=runtime;this.codec=new HostCodec(runtime);
    this.sql.exec('CREATE TABLE IF NOT EXISTS legacy_gate (one INTEGER PRIMARY KEY CHECK(one=1), doc TEXT NOT NULL)');
    this.sql.exec('CREATE TABLE IF NOT EXISTS legacy_rows (namespace TEXT NOT NULL, key TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(namespace,key))');
    this.sql.exec('CREATE TABLE IF NOT EXISTS legacy_holds (id TEXT PRIMARY KEY, until_ms INTEGER NOT NULL)');
  }
  gate() { const r=this.sql.exec('SELECT doc FROM legacy_gate WHERE one=1').toArray()[0];return r?decodeCanonical(r.doc):null; }
  keep(g) { this.sql.exec('INSERT OR REPLACE INTO legacy_gate VALUES(1,?)',encodeCanonical(g)); }
  async freeze(config) {
    if(this.admission)return this.admission.drain(()=>this.freezeSource(config));
    return this.freezeSource(config);
  }
  async freezeSource(config) {
    if(await this.storage.get?.('gone'))throw new Wire('gone',410);
    // Go validates the binding before the source acquires an irreversible gate.
    convertLegacy(this.runtime,config,'identity','',JSON.stringify({V:1,identity:config.identity}));
    this.storage.transactionSync(()=>{
      const old=this.gate();if(old){check(encodeCanonical(old.config)===encodeCanonical(config));return;}
      for(const {id,doc} of this.sql.exec("SELECT id,doc FROM dir WHERE kind='cells' ORDER BY id")) {
        let c;try{c=JSON.parse(doc);}catch{continue;}
        if(!c.lease)continue;
        const until=c.lease.expires===0?0:this.dir.watchers.vouchedUntil(c.lease.device,id,c.lease.fence);
        if(until>c.lease.expires)this.sql.exec('INSERT INTO legacy_holds VALUES(?,?)',id,until);
      }
      this.keep({stage:'frozen',config});
    });
    await this.storage.sync();
    this.dir.watchers.closeAll(1012,'reconnecting');
  }
  async inventory() {
    let manifest;
    this.storage.transactionSync(()=>{
      const g=this.gate();check(g);if(g.manifest){manifest=g.manifest;return;}
      const put=r=>this.sql.exec('INSERT INTO legacy_rows VALUES(?,?,?)',r.namespace,r.key,r.data);
      const authorities=response(this.runtime.call(frame(14).blob(data(g.config)).finish()));
      const countAuthorities=authorities.u32();
      for(let i=0;i<countAuthorities;i++){const r=decodeCanonical(dec.decode(authorities.blob()));put({...r,data:unb64(r.data)});}authorities.end();
      let identity=false;
      for(const {kind,id,doc} of this.sql.exec('SELECT kind,id,doc FROM dir ORDER BY kind,id')) {
        if(kind==='identity')identity=true;
        const until=this.sql.exec('SELECT until_ms FROM legacy_holds WHERE id=?',id).toArray()[0]?.until_ms??0;
        try { put(convertLegacy(this.runtime,g.config,kind,id,doc,until)); }
        catch(error) { throw new Error(`codeaf: cannot import ${kind} record ${JSON.stringify(id)}: unsupported fields or row exceeds ${ROW_BYTES} bytes; original retained`,{cause:error}); }
      }
      if(!identity)put(convertLegacy(this.runtime,g.config,'identity','',JSON.stringify({V:1,identity:g.config.identity})));
      let state=new Uint8Array(), digestRows=this.codec.hash(state).digest, count=0, total=0;
      for(const r of this.rows()){
        const encoded=data({namespace:r.namespace,key:r.key,data:b64(r.data)}), line=new Uint8Array(encoded.length+1);line.set(encoded);line[encoded.length]=10;
        ({state,digest:digestRows}=this.codec.hash(line,state));count++;total+=size(r);
      }
      const version=this.sql.exec('SELECT n FROM dirver').toArray()[0].n;check(Number.isSafeInteger(version));
      manifest={config:g.config,version:String(version),rows:String(count),bytes:String(total),digest:hex(digestRows),ceiling:"0"};
      for(let i=0;i<8;i++){const next=String(Math.max(DEFAULT_ROWS_BYTES,total+size({namespace:'membership',key:importKey,data:data(manifest)})));if(next===manifest.ceiling)break;manifest.ceiling=next;}
      this.codec.genesis(data(manifest));
      g.stage='inventoried';g.manifest=manifest;this.keep(g);
    });await this.storage.sync();return manifest;
  }
  *rows() { for(const r of this.sql.exec('SELECT namespace,key,data FROM legacy_rows ORDER BY namespace,key'))yield {...r,data:bytes(r.data)}; }
  async own(receipts) {
    this.storage.transactionSync(()=>{
      const g=this.gate();check(g?.manifest);const nodes=new Set();
      for(const r of receipts)if(g.config.anchors.includes(String(r.node))&&BigInt(r.applied)>=1n&&encodeCanonical(r.manifest)===encodeCanonical(g.manifest))nodes.add(String(r.node));
      check(nodes.size>=2);g.stage='raft-owned';this.keep(g);
    });await this.storage.sync();
  }
  // A stale routing hint can never resurrect the old writer. The host assembles
  // the committed facade only for this exact durable UUID/generation.
  route(factory) {const g=this.gate();if(g?.stage!=='raft-owned')throw new Wire('unreachable',503);return factory(g.manifest);}
}

// Member storage imports a bounded row stream in one durable genesis. Existing
// imported stores return their provenance without overwriting later cluster work.
export async function importLegacy(store, manifest, source) {
  const m=manifest, encoded=data(m), k={namespace:'membership',key:importKey};
  check(m.config.cluster===store.identity.cluster&&m.config.anchors.includes(store.identity.id)&&encoded.length+128<=reserve);
  check(String(store.identity.ceiling)===m.ceiling);
  await store.transaction(()=>{
    const app=store.application(),old=store.read(k.namespace,k.key);
    if(old){check(hex(old)===hex(encoded)&&BigInt(app.applied)>=1n);return;}
    check(app.applied==='0'&&app.count===0&&store.get('tip')==='0'&&store.get('hard')==='');
    const aggregate=new Uint8Array(32);let count=0,total=0,last='',state=new Uint8Array(),digest=store.codec.hash(state).digest;
    const add=r=>{
      check(r.data.length<=ROW_BYTES&&r.key.length>0);
      const sum=store.checksum('records',r.namespace,r.key,r.data);xor(aggregate,sum);
      store.rows('INSERT INTO records VALUES(?,?,?,?)',r.namespace,r.key,r.data,hex(sum));
    };
    for(const r of source()){
      const order=r.namespace+'\0'+r.key;check(order>last&&!(r.namespace===k.namespace&&r.key===k.key));last=order;
      const encoded=data({namespace:r.namespace,key:r.key,data:b64(r.data)}),line=new Uint8Array(encoded.length+1);line.set(encoded);line[encoded.length]=10;
      ({state,digest}=store.codec.hash(line,state));count++;total+=size(r);check(BigInt(count)<=BigInt(m.rows)&&BigInt(total)<=BigInt(m.bytes));add(r);
    }
    check(String(count)===m.rows&&String(total)===m.bytes&&hex(digest)===m.digest);add({...k,data:encoded});
    // These protobuf messages contain only the common genesis term/index and
    // the source-authorized complete voter set, encoded by the Go ABI below.
    const genesis=store.codec.genesis(data(m));
    store.rows('INSERT INTO journal VALUES(?,?,?)','0000000000000001',genesis.entry,store.hash(genesis.entry));
    store.put('tip','1');store.put('hard',b64(genesis.hard));
    store.put('application',{applied:'1',conf:b64(genesis.conf),version:m.version,now:String(m.config.now),count:count+1,size:total+size({...k,data:encoded}),hash:hex(aggregate)});
  });
  return store.checkpoint();
}

// Cross-object import stages one bounded row per request. Incomplete stages
// never start a core and never become application authority. Retries compare
// exact bytes; final installation rechecks the source's complete stream digest.
export async function stageLegacyImport(store, manifest, row) {
  store.codec.genesis(data(manifest));
  check(manifest.config.cluster===store.identity.cluster&&manifest.config.anchors.includes(store.identity.id)&&String(store.identity.ceiling)===manifest.ceiling);
  await store.transaction(()=>{
    store.rows('CREATE TABLE IF NOT EXISTS legacy_stage (one INTEGER PRIMARY KEY CHECK(one=1), doc TEXT NOT NULL)');
    store.rows('CREATE TABLE IF NOT EXISTS legacy_stage_rows (namespace TEXT NOT NULL, key TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(namespace,key))');
    const old=store.one('SELECT doc FROM legacy_stage WHERE one=1'),encoded=encodeCanonical(manifest);
    if(old)check(old.doc===encoded);else store.rows('INSERT INTO legacy_stage VALUES(1,?)',encoded);
    if(!row)return;
    check(['state','enrollment','outbox','tombstones','sessions','membership'].includes(row.namespace)&&row.key.length>0&&row.data.length<=ROW_BYTES&&size(row)<=ROW_BYTES+28);
    const before=store.one('SELECT data FROM legacy_stage_rows WHERE namespace=? AND key=?',row.namespace,row.key);
    if(before){check(hex(bytes(before.data))===hex(row.data));return;}
    const use=store.one('SELECT count(*) AS n,coalesce(sum(12+length(namespace)+length(key)+length(data)),0) AS bytes FROM legacy_stage_rows');
    check(BigInt(use.n+1)<=BigInt(manifest.rows)&&BigInt(use.bytes+size(row))<=BigInt(manifest.bytes));
    store.rows('INSERT INTO legacy_stage_rows VALUES(?,?,?)',row.namespace,row.key,row.data);
  });
}
export async function finishLegacyImport(store, manifest) {
  const snapshot=await importLegacy(store,manifest,function*(){for(const r of store.rows('SELECT namespace,key,data FROM legacy_stage_rows ORDER BY namespace,key'))yield {...r,data:bytes(r.data)};});
  // These are redundant staging bytes. The immutable legacy ORIGINAL remains
  // at its source; imported provenance and rows remain in every snapshot.
  await store.transaction(()=>{store.rows('DROP TABLE IF EXISTS legacy_stage_rows');store.rows('DROP TABLE IF EXISTS legacy_stage');});
  return snapshot;
}
