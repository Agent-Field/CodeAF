import test from 'node:test';
import assert from 'node:assert/strict';
import { hostedFixture } from './hosted-fixture.js';

// H2/R24/R28 cut the actual coordinator after every durable migration stage,
// including partial imports at each rank. Every resumed call uses public HTTP.
test('H2 R3 R24 R28 hosted first-use migration resumes every cut with one UUID and unchanged originals',{timeout:120000},async()=>{
 const cuts=[
  {name:'freeze',needle:'if(!this.source.gate())await this.source.freeze(this.provisioning.config);'},
  {name:'inventory',needle:'this.manifest=await this.source.inventory();'},
  ...['1','2','3'].map(id=>({name:'stage-'+id,needle:'await this.member(id).legacyImport(this.config(id),this.manifest,copied);',after:`if(id==='${id}')throw new Error('H2 injected process cut');`})),
  ...['1','2','3'].map(id=>({name:'install-'+id,needle:'await this.member(id).legacyImport(this.config(id),this.manifest,null,true);',after:`if(id==='${id}')throw new Error('H2 injected process cut');`})),
  {name:'readback',needle:'await this.pump();await this.elect();await this.barrier();'},
  {name:'ownership',needle:'if(!owned)await this.source.own(receipts);'},
 ];
 for(const cut of cuts){
  const h=await hostedFixture({fixture:true}),a=h.client();
  try{
   await h.start();
   const cell={V:1,head:'saved-original',durable_at:1000,class:'work',size:17,title:'c2VhbGVk',parent_cell:'parent',keys:{key:{[h.identity]:'wrapped'}},frames:['sealed-frame'],lease:{device:a.device,fence:7,expires:0,pending:0,released_at:900}};
   await h.control({op:'seed',records:[{kind:'identity',id:'',doc:{V:1,identity:h.identity,vault:'saved-vault'}},{kind:'devices',id:a.device,doc:{V:1,name:'c2VhbGVk',added_by:h.identity,revoked:false,caps:{os:'linux',arch:'amd64',sandbox:null,container:null,gpu:null,cow:'none'}}},{kind:'cells',id:'chat',doc:cell}]});
   const before=await h.control({op:'source'});
   await h.start(cut);const interrupted=await h.call(a,'GET','/v1/dir/list');assert.equal(interrupted.status,500,cut.name);
   const frozen=await h.control({op:'source'}),gate=JSON.parse(frozen.gate);assert.equal(gate.config.generation,'1');assert.deepEqual(frozen.originals,before.originals);
   await h.start();const list=await h.call(a,'GET','/v1/dir/list');assert.equal(list.status,200,cut.name+JSON.stringify(list));
   assert.equal(list.body.identity.vault,'saved-vault');assert.equal(list.body.cells.chat.lease.fence,7);assert.equal(list.body.cells.chat.lease.released_at,900);
   const full=await h.call(a,'GET','/v1/dir/cells/chat');assert.deepEqual(full.body.cell,cell);
   const after=await h.control({op:'source'}),owned=JSON.parse(after.gate);assert.equal(owned.stage,'raft-owned');assert.deepEqual(owned.config,gate.config);assert.deepEqual(after.originals,before.originals);
   for(const node of ['1','2','3']){const member=await h.control({op:'member',cluster:owned.config.cluster,node});assert.deepEqual(JSON.parse(member.provenance),owned.manifest);assert(BigInt(member.app.applied)>1n);}
  }finally{await h.close();}
 }
});
