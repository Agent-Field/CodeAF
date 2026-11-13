import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {spawn} from 'node:child_process';
import {DatabaseSync} from 'node:sqlite';
import {generateKeyPairSync,sign,createHash} from 'node:crypto';
import {Reactor} from '../../src/raft/abi.js';
import {MemberActor} from '../../src/raft/actor.js';
import {HostedIdentity} from '../../src/raft/hosted.js';
import {directoryEvaluator} from '../../src/raft/directory.js';
import {encodeCanonical,decodeCanonical} from '../../src/raft/codec.js';
import {v1Command} from '../../src/raft/v1.js';

test('r5.4 committed hosted prefix proofs apply identically in the native personal evaluator', {timeout:30000}, async () => {
  const out=process.env.CODEAF_RAFT_PROOF_DIR;
  const runtime=new Reactor(new WebAssembly.Module(readFileSync(out+'/core.wasm')));
  const calls=[],traced={call(input){const result=runtime.call(input);calls.push([input.slice(),result.slice()]);return result;}};
  const enc=new TextEncoder(),dec=new TextDecoder(),bytes=v=>enc.encode(encodeCanonical(v));
  const root=generateKeyPairSync('ed25519'),device=generateKeyPairSync('ed25519');
  const pub=key=>key.export({format:'der',type:'spki'}).subarray(-32);
  const sha=b=>createHash('sha256').update(b).digest('hex');
  const cert={V:1,device:pub(device.publicKey).toString('hex'),created:1000,sig:''};
  cert.sig=sign(null,Buffer.from('codeaf/device-cert/v1\n'+JSON.stringify(cert)),root.privateKey).toString('hex');
  for (const prefix of ['/my-relay','/a/relay']) {
    const body=Buffer.from('{"head":"saved","class":"work","size":0,"keys":{}}');
    const route='/v1/dir/cells/chat',uri=prefix+route,at='1000';
    const request=new Request('http://local'+route,{method:'POST',headers:{
      'Codeaf-Identity':pub(root.publicKey).toString('base64url'),
      'Codeaf-Cert':Buffer.from(JSON.stringify(cert)).toString('base64url'),
      'Codeaf-Time':at,
      'Codeaf-Sig':sign(null,Buffer.from('codeaf-req-v1\nPOST\n'+uri+'\n'+at+'\n'+sha(body)),device.privateKey).toString('base64url'),
    },body});
    const proof=HostedIdentity.prototype.proof.call({env:{BASE_PATH:prefix}},request,body);
    const committed=v1Command(traced,'cluster',1000,proof);
    const command=decodeCanonical(dec.decode(committed));
    assert.equal(command.v1_request.uri,uri);
    assert.equal(command.v1_request.base_path,prefix);
    const identity=bytes({V:1,identity:'id_'+sha(pub(root.publicKey)).slice(0,32),signing_key:pub(root.publicKey).toString('base64')});
    const actors=[],stores=[],queue=[],results=[];
    const accept=effects=>queue.push(...effects.send);
    const pump=async()=>{for(let n=0;queue.length;n++){assert(n<3000);const message=queue.shift();accept(await actors[Number(message.to)-1].input(2,message.bytes));}};
    try {
      for(let n=0;n<3;n++) {
        const db=new DatabaseSync(':memory:');stores.push(db);
        const storage={sql:{exec(sql,...args){const rows=db.prepare(sql).all(...args);return {toArray:()=>rows,[Symbol.iterator]:()=>rows[Symbol.iterator]()};}},transactionSync(fn){db.exec('BEGIN');try{const value=fn();db.exec('COMMIT');return value;}catch(error){db.exec('ROLLBACK');throw error;}},async sync(){}};
        const evaluate=(raw,read,app)=>{
          const initial=read('state','identity')?[]:[{namespace:'state',key:'identity',data:identity}];
          const size=initial.length?12+'state'.length+'identity'.length+identity.length:0;
          const result=directoryEvaluator(traced,'cluster',8*1024*1024)(raw,(ns,key)=>initial.find(row=>row.namespace===ns&&row.key===key)?.data??read(ns,key),{...app,size:app.size+size});
          results.push(decodeCanonical(dec.decode(result.result)));return {...result,writes:[...initial,...result.writes]};
        };
        const actor=new MemberActor(storage,runtime,evaluate);actors.push(actor);
        accept(await actor.init({cluster:'cluster',id:String(n+1),seed:String(n+20),bootstrap:['1','2','3']}));
      }
      await pump();
      for(let n=0;n<30&&!actors.some(actor=>actor.status.role===2);n++){for(const actor of actors)accept(await actor.input(1));await pump();}
      const leader=actors.find(actor=>actor.status.role===2);assert(leader);
      accept(await leader.input(3,committed));await pump();
      assert.equal(results.length,3);assert(results.every(result=>result.code==='ok'));
      const cells=actors.map(actor=>actor.store.read('state','cell/'+Buffer.from('chat').toString('hex')));
      assert.deepEqual(cells[0],cells[1]);assert.deepEqual(cells[0],cells[2]);
      const saved=decodeCanonical(dec.decode(cells[0]));
      assert.equal(saved.head,'saved');assert.equal(saved.lease.fence,'1');
    } finally {for(const db of stores)db.close();}
  }
  const child=spawn(out+'/native-peer',[],{stdio:['pipe','pipe','inherit'],env:{...process.env,GOMEMLIMIT:'32MiB'}});
  let buffer=Buffer.alloc(0),waiter;
  const pump=()=>{if(!waiter||buffer.length<4)return;const n=buffer.readUInt32LE();assert(n<=2*1024*1024);if(buffer.length<n+4)return;const raw=buffer.subarray(4,n+4);buffer=buffer.subarray(n+4);const w=waiter;waiter=null;clearTimeout(w.timer);w.resolve(new Uint8Array(raw));};
  child.stdout.on('data',raw=>{buffer=Buffer.concat([buffer,raw]);pump();});
  try {
    for (const [input,expected] of calls) {
      const actual=await new Promise((resolve,reject)=>{assert(!waiter);waiter={resolve,timer:setTimeout(()=>reject(Error('native ABI timeout')),5000)};const header=Buffer.alloc(4);header.writeUInt32LE(input.length);child.stdin.write(header);child.stdin.write(input);pump();});
      assert.deepEqual(actual,expected);
    }
  } finally {
    if(waiter)clearTimeout(waiter.timer);
    child.stdin.end();if(child.exitCode===null)await new Promise(resolve=>child.once('exit',resolve));
  }
});
