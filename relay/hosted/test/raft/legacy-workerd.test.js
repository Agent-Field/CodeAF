import test from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { encodeCanonical } from '../../src/raft/codec.js';
import { build } from 'esbuild';
import { writeFile, mkdtemp, copyFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generateKeyPairSync, createHash } from 'node:crypto';
import { openWorkerd } from './workerd.js';

test('R24 R3 R28 actual hosted source and three imports survive process death at every migration stage',{timeout:60000},async()=>{
 const proof=process.env.CODEAF_RAFT_PROOF_DIR??'/tmp/codeaf-p11',out=await mkdtemp(join(tmpdir(),'p11-wasm-')),persist=await mkdtemp(join(tmpdir(),'p11-disk-'));
 const bundled=await build({entryPoints:['test/raft/legacy-worker.js'],bundle:true,format:'esm',target:'es2022',external:['core.wasm','cloudflare:*'],write:false});
 await writeFile(join(out,'worker.js'),bundled.outputFiles[0].contents);await copyFile(join(proof,'core.wasm'),join(out,'core.wasm'));await writeFile(join(out,'storage-worker.js'),'export class ProofStore{}');
 const pub=()=>generateKeyPairSync('ed25519').publicKey.export({format:'der',type:'spki'}).subarray(-32);
 const root=pub(),cfg={cluster:'one-hosted-uuid',generation:'1',identity:'id_'+createHash('sha256').update(root).digest('hex').slice(0,32),signing_key:root.toString('base64'),effects_key:pub().toString('base64'),anchors:['1','2','3'],now:1000};
 const records=[{kind:'identity',id:'',doc:{V:1,identity:cfg.identity,vault:'sealed-vault',rotation:{state:'frozen',by:'a',at:900}}},{kind:'devices',id:'a',doc:{V:1,name:'c2VhbGVk',added_by:cfg.identity,revoked:false,caps:{os:'linux',arch:'amd64',sandbox:null,container:null,gpu:null,cow:'none'}}},{kind:'cells',id:'chat',doc:{V:1,head:'saved',durable_at:1000,class:'host-bound',size:7,keys:{key:{identity:'wrapped'}},lease:{device:'a',fence:1,expires:0,pending:0,released_at:900},frames:['sealed-frame'],title:'c2VhbGVk'}}];
 let native;
 let server;const restart=async()=>{if(server)await server.dispose();server=await openWorkerd(out,persist,true);};
 const call=async(name,op,extra={})=>{const r=await fetch(server.origin+'/'+name,{method:'POST',body:JSON.stringify({op,config:cfg,...extra}),signal:AbortSignal.timeout(5000)});assert.equal(r.status,200);return r.json();};
 const ok=async(...args)=>{const r=await call(...args);assert('ok'in r,JSON.stringify(r));return r.ok;};
 try{
  await restart();await ok('source','seed',{records});const original=await ok('source','original');
  await ok('source','freeze');await restart();assert.equal((await ok('source','gate')).stage,'frozen');assert.equal((await call('source','legacy')).status,503);
  const manifest=await ok('source','inventory');await restart();assert.deepEqual(await ok('source','inventory'),manifest);const rows=await ok('source','rows');
  const snapshots=[];
  for(const node of ['1','2','3']){
   const memberConfig={cluster:cfg.cluster,id:node,seed:String(100+Number(node)),bootstrap:cfg.anchors,ceiling:Number(manifest.ceiling)};
   await ok('anchor-'+node,'stage',{memberConfig,manifest,row:rows[0]});await restart();
   assert('error'in await call('anchor-'+node,'activate',{memberConfig}),'incomplete import activated');
   for(const row of rows)await ok('anchor-'+node,'stage',{memberConfig,manifest,row});
   const snapshot=await ok('anchor-'+node,'finish',{memberConfig,manifest});snapshots.push(snapshot);await restart();
   const result=await ok('anchor-'+node,'member',{node,manifest});assert.equal(result.applied,'1');assert.equal(result.snapshot,snapshot);
   assert.equal(await ok('anchor-'+node,'finish',{memberConfig,manifest}),snapshot);
   const active=await ok('anchor-'+node,'activate',{memberConfig});assert.equal(active.status.applied,'1');assert.equal(active.status.role,0);
   assert.equal((await call('source','legacy')).status,503);
  }
  assert.equal(snapshots[0],snapshots[1]);assert.equal(snapshots[1],snapshots[2]);
  // A real native FULL-synchronous store imports exactly the hosted export.
  // This compares the complete portable genesis bytes, not a JS summary.
  native=spawn(join(proof,'native-peer'),['actors',join(persist,'native')],{stdio:['pipe','pipe','inherit'],env:{...process.env,GOMEMLIMIT:'32MiB'}});
  let buffer=Buffer.alloc(0),waiter;
  native.stdout.on('data',chunk=>{buffer=Buffer.concat([buffer,chunk]);if(waiter&&buffer.length>=4){const n=buffer.readUInt32LE();assert(n<2*1024*1024);if(buffer.length>=n+4){const response=JSON.parse(buffer.subarray(4,n+4));buffer=buffer.subarray(n+4);clearTimeout(waiter.timer);const done=waiter;waiter=null;done.resolve(response);}}});
  const nativeCall=value=>new Promise((resolve,reject)=>{waiter={resolve,reject,timer:setTimeout(()=>reject(Error('native import timeout')),5000)};const body=Buffer.from(JSON.stringify(value)),header=Buffer.alloc(4);header.writeUInt32LE(body.length);native.stdin.write(header);native.stdin.write(body);});
  const nativeImport=await nativeCall({op:'legacy-import',cluster:cfg.cluster,id:'1',value:JSON.parse(encodeCanonical(manifest)),rows});assert.equal(nativeImport.applied,'1',JSON.stringify(nativeImport));assert.equal(nativeImport.snapshot,snapshots[0]);
  assert.deepEqual(await nativeCall({op:'legacy-import',cluster:cfg.cluster,id:'1',value:JSON.parse(encodeCanonical(manifest)),rows:[]}),nativeImport);
  native.stdin.end();await new Promise(resolve=>native.once('exit',resolve));native=null;
  assert('error'in await call('source','own',{receipts:[{node:'1',applied:'1',manifest}]}));
  await ok('source','own',{receipts:[{node:'1',applied:'2',manifest},{node:'2',applied:'2',manifest}]});await restart();const gate=await ok('source','gate');assert.equal(gate.stage,'raft-owned');assert.deepEqual(gate.manifest,manifest);assert.equal(await ok('source','original'),original);
  await ok('erased','erase-marker');assert.equal((await call('erased','freeze')).status,410);assert.equal(await ok('erased','gate'),null);
  const conflict=await call('source','freeze',{config:{...cfg,cluster:'second-uuid'}});assert('error'in conflict);assert.equal((await ok('source','gate')).config.cluster,cfg.cluster);assert.equal((await call('source','legacy')).status,503);
 }finally{if(native?.exitCode===null){native.kill('SIGKILL');await new Promise(resolve=>native.once('exit',resolve));}if(server)await server.dispose();await rm(out,{recursive:true,force:true});await rm(persist,{recursive:true,force:true});}
});
