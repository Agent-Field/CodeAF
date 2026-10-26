// Only this loopback fixture exposes migration controls. Production invokes
// the same source/member adapters after authenticating its original ingress.
import core from 'core.wasm';
import { RaftMemberDO } from '../../src/raft/member.js';
import { installRaftModule, sharedRuntime } from '../../src/raft/actor.js';
installRaftModule(core);
import { Reactor, HostCodec } from '../../src/raft/abi.js';
import { Directory } from '../../src/directory.js';
import { policyOf } from '../../src/rules.js';
import { LegacySource, LegacyAdmission, importLegacy } from '../../src/raft/legacy.js';
import { MemberStorage, b64, unb64 } from '../../src/raft/storage.js';
let runtime,codec;
export class ActorFixture extends RaftMemberDO{
 constructor(ctx,env){super(ctx,env);this.tail=Promise.resolve();this.admission=new LegacyAdmission(ctx.storage);}
 fetch(request){const p=this.tail.then(()=>this.control(request));this.tail=p.catch(()=>{});return p;}
 async control(request){
  try{
   runtime??=sharedRuntime();codec??=new HostCodec(runtime);
   const {op,...v}=await request.json();let result;
   if(op==='stage'||op==='finish') {
    const snapshot=await this.legacyImport(v.memberConfig,v.manifest,v.row?{...v.row,data:unb64(v.row.data)}:null,op==='finish');result=snapshot?b64(snapshot):true;
   }else if(op==='activate') {result=await this.initialize(v.memberConfig);
   }else if(op==='import'||op==='member'){
    const store=new MemberStorage(this.ctx.storage,codec);await store.open({cluster:v.manifest.config.cluster,id:v.node,protocol:1},Number(v.manifest.ceiling));
    if(op==='import')await importLegacy(store,v.manifest,function*(){for(const r of v.rows)yield {...r,data:unb64(r.data)};});
    const recovered=store.recover();result={applied:String(recovered.applied),snapshot:b64(recovered.snapshot),provenance:b64(store.read('membership','legacy-import'))};
   }else{
    const dir=new Directory(this.ctx.storage.sql,v.config.identity,()=>v.config.now,policyOf());
    const source=new LegacySource(this.ctx.storage,dir,runtime,this.admission);
    switch(op){
    case 'erase-marker':await this.ctx.storage.put('gone',1000);await this.ctx.storage.sync();result=true;break;
    case 'seed':for(const r of v.records)dir.write(r.kind,r.id,r.doc);await this.ctx.storage.sync();result=true;break;
    case 'freeze':await source.freeze(v.config);result=true;break;
    case 'inventory':result=await source.inventory();break;
    case 'rows':result=[...source.rows()].map(r=>({...r,data:b64(r.data)}));break;
    case 'own':await source.own(v.receipts);result=true;break;
    case 'gate':result=source.gate();break;
    case 'legacy':dir.release('chat','a',1);result=true;break;
    case 'original':result=this.ctx.storage.sql.exec("SELECT doc FROM dir WHERE kind='cells' AND id='chat'").toArray()[0].doc;break;
    default:return new Response(null,{status:404});
    }
   }
   return new Response(JSON.stringify({ok:result},(_,v)=>typeof v==='bigint'?String(v):v),{headers:{'content-type':'application/json'}});
  }catch(e){return Response.json({error:e.message,status:e.status??503});}
 }
}
export default{
 async fetch(request,env){if(new URL(request.url).pathname==='/ready')return new Response(null,{status:204});const name=new URL(request.url).pathname.slice(1);return env.ACTORS.get(env.ACTORS.idFromName(name)).fetch(request);}
};
