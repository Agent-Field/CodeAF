import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { mkdtemp, writeFile, copyFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generateKeyPairSync, createHash, sign } from 'node:crypto';
import { encodeCanonical } from '../../src/raft/codec.js';
import { openWorkerd } from './workerd.js';

// R18/R19/R21 use actual persisted DOs and the production Go policy. An old
// authenticated claim crosses cancellation, and a killed isolate retains the
// exact claimed fence and already published result without replaying work.
test('R18 R19 R21 actual workerd assignment claim, delayed decision and restart',{timeout:45000},async()=>{
 const proof=process.env.CODEAF_RAFT_PROOF_DIR??'/tmp/codeaf-p16',out=await mkdtemp(join(tmpdir(),'p16-wasm-')),disk=await mkdtemp(join(tmpdir(),'p16-disk-'));
 const bundle=await build({entryPoints:['test/raft/membership-worker.js'],bundle:true,format:'esm',target:'es2022',external:['core.wasm','cloudflare:*'],write:false});
 await writeFile(join(out,'worker.js'),bundle.outputFiles[0].contents);await copyFile(join(proof,'core.wasm'),join(out,'core.wasm'));await writeFile(join(out,'storage-worker.js'),'export class ProofStore{}');
 const keys=Object.fromEntries(['owner','root','worker','effects','1','2','3'].map(id=>[id,generateKeyPairSync('ed25519')])),pub=id=>keys[id].publicKey.export({format:'der',type:'spki'}).subarray(-32);
 const cluster='p16-workerd',sha=b=>createHash('sha256').update(b).digest('hex'),bytes=v=>Buffer.from(encodeCanonical(v)),segment=s=>Buffer.from(s).toString('hex');
 const worker='dev_'+sha(pub('worker')).slice(0,32),identity='id_'+sha(pub('root')).slice(0,32),scope={identity:cluster,cluster,epoch:'1',protocol:1};
 const anchors=['1','2','3'].map(node=>({node,device:'',key:pub(node).toString('base64'),incarnation:'1',protocol:1,anchor:true,retired:false,ready_index:'0'}));
 const genesis=[{namespace:'state',key:'identity',data:bytes({V:1,identity,signing_key:pub('root').toString('base64')}).toString('base64')},...['owner','effects'].map(id=>({namespace:'enrollment',key:'actor/'+segment(id),data:bytes({cluster,key:pub(id).toString('base64'),personal:id==='owner',...(id==='effects'?{effects:true}:{})}).toString('base64')}))];
 let server,transport=0,serial=0;const status={},queue=[];
 const restart=async()=>{if(server)await server.dispose();server=await openWorkerd(out,disk,true);};
 const call=async(id,op,extra={})=>{const r=await fetch(server.origin+'/'+id,{method:'POST',body:JSON.stringify({op,...extra}),signal:AbortSignal.timeout(5000)});assert.equal(r.status,200);const v=await r.json();if(v?.error)throw Error(v.error);return v;};
 const accept=(id,r)=>{if(r?.status)status[id]=r.status;queue.push(...(r?.send??[]).map(m=>({...m,from:id})));};
 const pump=async()=>{for(let n=0;queue.length;n++){assert(n<2000);const m=queue.shift();const e={scope,from:String(m.from),to:String(m.to),incarnation:'1',destination:'1',sequence:String(++transport),chunk:null,digest:sha(Buffer.from(m.bytes,'base64')),payload:m.bytes,signature:null};e.signature=sign(null,Buffer.from('codeaf/raft/envelope/1\n'+JSON.stringify(e)),keys[e.from].privateKey).toString('base64');accept(Number(m.to),await call(m.to,'peer',{value:Buffer.from(JSON.stringify(e)).toString('base64')}));}};
 const leader=()=>Number(Object.keys(status).find(id=>status[id].role===2));
 const command=(change,signer='owner',inc=0)=>{const actor=signer==='worker'?worker:signer,session='run-'+(++serial),fields={cluster,actor,session,sequence:'1',change,...(inc?{worker_incarnation:String(inc)}:{})};const c={schema:1,...fields,request_digest:sha(bytes(fields)),now:1000,signature:null};c.signature=sign(null,Buffer.concat([Buffer.from('codeaf.directory.command.v1\0'),bytes(c)]),keys[signer].privateKey).toString('base64');return c;};
 const row=async(ns,key)=>{const b=await call(leader(),'row',{namespace:ns,key});return b?JSON.parse(Buffer.from(b,'base64').toString()):null;};
 const submit=async c=>{const i=leader();accept(i,await call(i,'input',{tag:3,value:bytes(c).toString('base64')}));await pump();if(c.change.record_upload){assert(await row('outbox','upload/'+segment(sha(bytes(c.change.record_upload)))));return {code:'ok'};}const saved=await row('sessions',segment(c.actor)+'/'+segment(c.session));assert(saved,'missing committed result for '+Object.keys(c.change));return saved.result;};
 const send=async(change,signer,inc,code='ok')=>{const r=await submit(command(change,signer,inc));assert.equal(r.code,code);return r;};
 const tick=async()=>{for(const id of [1,2,3])accept(id,await call(id,'input',{tag:1}));await pump();};
 try{
  await restart();for(const id of [1,2,3])accept(id,await call(id,'init',{config:{cluster,id:String(id),seed:String(id+20),bootstrap:['1','2','3'],anchors,scope,genesis}}));await pump();for(let n=0;n<30&&!leader();n++)await tick();assert(leader());
  const cert={v:1,identity,cluster,key:pub('worker').toString('base64'),made:1,expires:100000,cells:['child'],learner:false,signature:null};cert.signature=sign(null,Buffer.concat([Buffer.from('codeaf/worker-cert/v1\0'),bytes(cert)]),keys.root.privateKey).toString('base64');
  await send({enroll_worker:{certificate:cert,grants:{child:Buffer.from([0x81]).toString('base64')}}});
  await send({register_worker:{previous:'0',incarnation:'1',caps:{os:'',arch:'',sandbox:null,container:null,gpu:null,cow:''},sealed_labels:null}},'worker');
  const beat={worker,incarnation:'1',sequence:'1',seen_at:1000,work:null,signature:null};beat.signature=sign(null,Buffer.concat([Buffer.from('codeaf/worker-beat/v1\0'),bytes({cluster,beat})]),keys.worker.privateKey).toString('base64');assert.equal((await send({worker_beat_batch:{items:[beat]}})).workers[0].code,'ok');
  for(const cell of ['parent','child'])await send({create:{v1:true,cell,init:{head:'head-'+cell,class:'',size:'0',keys:null,...(cell==='child'?{parent_cell:'parent'}:{})},receipt:''}});
  const spec={id:'run',revision:'1',cell:'child',parent_cell:'parent',expect:{device:'owner',fence:'1'},target:worker,incarnation:'1',expires:90000,sealed_payload:Buffer.from([0x81]).toString('base64')};const ref=v=>({id:v.id,revision:v.revision,cell:v.cell,target:v.target,incarnation:v.incarnation});
  await send({assign:spec});const delayed=command({claim_assignment:{ref:ref(spec),expect:spec.expect}},'worker',1);await send({cancel_assignment:{ref:ref(spec)}});assert.equal((await submit(delayed)).code,'conflict');
  spec.id='second';await send({assign:spec});const claim=command({claim_assignment:{ref:ref(spec),expect:spec.expect}},'worker',1);const claimed=await submit(claim);assert.equal(claimed.code,'ok');assert.equal(claimed.view.cell.lease.fence,'2');
  const publish={cell:'child',publish:{fence:'2',old_head:'head-child',head:'result',size:'0',class:'',pending:0},receipt:''};await send({publish},'worker',1,'bad_request');assert.equal((await row('state','cell/'+segment('child'))).head,'head-child');
  const receipt={device:worker,cell:'child',worker_incarnation:'1',head:'result',head_rid:'rid'};await send({record_upload:receipt},'effects');publish.receipt=sha(bytes(receipt));await send({publish},'worker',1);
  await send({acquire:{cell:'parent',force:true,expect:{device:'owner',fence:'1'}}});await send({finish_assignment:{ref:ref(spec),fence:'2',head:'result'}},'worker',1);
  for(const id of [1,2,3])await call(id,'checkpoint');await restart();for(const id of [1,2,3])accept(id,await call(id,'status'));await pump();for(let n=0;n<30&&!leader();n++)await tick();assert(leader());
  assert.deepEqual(await submit(claim),claimed);assert.equal((await row('state','assignment/'+segment(spec.id))).status,'finished');assert.equal((await row('state','cell/'+segment('parent'))).head,'head-parent');assert.equal((await row('state','cell/'+segment('parent'))).lease.fence,'2');
 }finally{if(server)await server.dispose();await rm(out,{recursive:true,force:true});await rm(disk,{recursive:true,force:true});}
});
