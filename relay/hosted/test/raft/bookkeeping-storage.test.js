import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {createHash,generateKeyPairSync,sign} from 'node:crypto';
import {Reactor,HostCodec} from '../../src/raft/abi.js';
import {MemberStorage} from '../../src/raft/storage.js';
import {directoryEvaluator} from '../../src/raft/directory.js';
import {encodeCanonical} from '../../src/raft/codec.js';
const enc=new TextEncoder(),empty=new Uint8Array(),sha=b=>createHash('sha256').update(b).digest('hex');
const data=v=>enc.encode(encodeCanonical(v));
const publicBytes=key=>key.publicKey.export({format:'der',type:'spki'}).subarray(-32);
const varint=n=>{const b=[];do{b.push((n&127)|(n>127?128:0));n=Math.floor(n/128);}while(n);return b;};
const entry=(index,raw)=>Uint8Array.from([8,0,16,1,24,...varint(index),34,...varint(raw.length),...raw]);
const hard=index=>Uint8Array.from([8,1,24,...varint(index)]),conf=Uint8Array.of(8,1,8,2,8,3);
class Storage {
 constructor(){this.db=new DatabaseSync(':memory:');this.sql={exec:(q,...p)=>{const rows=this.db.prepare(q).all(...p);return {toArray:()=>rows,[Symbol.iterator]:()=>rows[Symbol.iterator]()};}};}
 transactionSync(fn){this.db.exec('BEGIN');try{const result=fn();this.db.exec('COMMIT');return result;}catch(error){this.db.exec('ROLLBACK');throw error;}}
 async sync(){}
}

// r1.5
 test('r1.5 bookkeeping survives a full-content snapshot without granting content growth',{timeout:60000},async()=>{
 const runtime=new Reactor(new WebAssembly.Module(readFileSync(process.env.CODEAF_RAFT_PROOF_DIR+'/core.wasm'))),codec=new HostCodec(runtime);
 const host=new Storage(),target=new Storage(),identity=id=>({cluster:'bounded-directory',id,protocol:1});
 const store=new MemberStorage(host,codec),root=generateKeyPairSync('ed25519'),actor=generateKeyPairSync('ed25519');let index=0;
 const apply=async(raw,evaluate)=>{const entries=[entry(++index,raw)];await store.persist({hard:hard(index),entries,snapshot:empty});await store.apply({index:BigInt(index),entries,conf,snapshot:empty},evaluate);};
 try{
  await store.open(identity('1'));
  await apply(Uint8Array.of(1),()=>({writes:[
   {namespace:'state',key:'identity',data:data({V:1,identity:'id_'+sha(publicBytes(root)).slice(0,32),signing_key:publicBytes(root).toString('base64')})},
   {namespace:'enrollment',key:'actor/61',data:data({cluster:identity('1').cluster,key:publicBytes(actor).toString('base64'),personal:true})}
  ]}));
  while(store.application().size<store.identity.ceiling){
   const key='saved-'+index,cost=12+enc.encode('state'+key).length,length=Math.min(900000,store.identity.ceiling-store.application().size-cost);assert(length>0);
   await apply(Uint8Array.of(1),()=>({writes:[{namespace:'state',key,data:new Uint8Array(length).fill(19)}]}));
  }
  const before=store.application().size;
  const command={schema:1,cluster:identity('1').cluster,actor:'a',session:'bounded-retry',sequence:'1',request_digest:'',now:1000,change:{advance_time:{}},signature:null};
  command.request_digest=sha(data({cluster:command.cluster,actor:command.actor,session:command.session,sequence:command.sequence,change:command.change}));
  command.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),data(command)]),actor.privateKey).toString('base64');
  const coreEvaluate=directoryEvaluator(runtime,command.cluster,store.identity.ceiling);let result;
  const evaluate=(...args)=>{const change=coreEvaluate(...args);result=JSON.parse(new TextDecoder().decode(change.result));return change;};
  await apply(data(command),evaluate);assert.equal(result.code,'ok');const firstResult=result;
  const after=store.application().size,policy=codec.bookkeeping(store.read('sessions','counts'));assert(after>before);assert.equal(after-policy.bytes,before);assert(after<=before+policy.max);
  await apply(data(command),evaluate);assert.deepEqual(result,firstResult);assert.equal(store.application().size,after);
  const snapshot=await store.checkpoint(),info=codec.inspect(3,snapshot),manifest=JSON.parse(new TextDecoder().decode(info.data)),id=sha(info.data);
  const dest=new MemberStorage(target,codec);await dest.open(identity('2'));await dest.beginSnapshot(info.data);
  for(let n=0;n<Number(manifest.chunks);n++){const chunk=store.chunk(id,n);await dest.putChunk(id,n,chunk,sha(chunk));}
  assert.deepEqual(await dest.completeSnapshot(id),snapshot);await dest.persist({hard:hard(index),entries:[],snapshot});
  assert.equal(dest.application().size,after);assert.deepEqual(dest.read('sessions','counts'),store.read('sessions','counts'));
  for(const row of store.recordRows())assert.deepEqual(dest.read(row.namespace,row.key),row.data);
  const applied=dest.application().applied,entries=[entry(index+1,Uint8Array.of(1))];await dest.persist({hard:hard(index+1),entries,snapshot:empty});
  await assert.rejects(dest.apply({index:BigInt(index+1),entries,conf,snapshot:empty},()=>({writes:[{namespace:'state',key:'growth',data:Uint8Array.of(1)}]})),/application capacity/);
  assert.equal(dest.application().applied,applied);assert.equal(dest.application().size,after);assert.equal(dest.read('state','growth'),null);
 }finally{host.db.close();target.db.close();}
});
