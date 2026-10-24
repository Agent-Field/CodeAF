import { frame, response, HostCodec } from './abi.js';
import { sharedRuntime } from './actor.js';
import { encodeCanonical, decodeCanonical } from './codec.js';
import { b64, unb64, bytes } from './storage.js';
import { v1Command } from './v1.js';
import { RaftEffectsFacade } from './effects.js';
import { Wire, empty, json, unwrap } from '../wire.js';
import { signedUri } from '../base.js';
import { fromBase64Url } from '../codec.js';
import { presenceOf, parseBeat } from '../presence.js';
import { parseHolds } from '../vouch.js';
import { rotationView } from '../rotation.js';
import { CommittedWatchers, CLOSE_REVOKED, CLOSE_ROTATED } from '../watch.js';

const enc=new TextEncoder(),dec=new TextDecoder('utf-8',{fatal:true});
const segment=s=>Array.from(enc.encode(s),b=>b.toString(16).padStart(2,'0')).join('');
const unsegment=s=>dec.decode(Uint8Array.from(s.match(/../g)??[],h=>parseInt(h,16)));
const data=v=>enc.encode(encodeCanonical(v));
const MIGRATION_PAGE=64;
const unavailable=()=>new Wire('unreachable',503,1);
const status={not_found:404,unauthorized:401,revoked:401,rotated:410,bad_request:400,bad_grace:400,self_revoke:400,capacity:507,full:507};
function requireOK(code){if(code!=='ok')throw new Wire(code,status[code]??409);}
async function keypair(){
  const pair=await crypto.subtle.generateKey('Ed25519',true,['sign','verify']);
  const privateBytes=new Uint8Array(await crypto.subtle.exportKey('pkcs8',pair.privateKey));
  return {public:b64(new Uint8Array(await crypto.subtle.exportKey('raw',pair.publicKey))),private:b64(privateBytes),seed:b64(privateBytes.slice(-32))};
}

// The source remains the namespace router and blob index, but its irreversible
// gate prevents it ever becoming a second directory writer. All coordination
// replies below depend on the members' actual durable application and quorum.
export class HostedIdentity {
  constructor(ctx,env,tenant,admission){
    this.ctx=ctx;this.env=env;this.tenant=tenant;this.runtime=sharedRuntime();this.codec=new HostCodec(this.runtime);
    this.source=tenant.migration(ctx.storage,this.runtime);this.tail=Promise.resolve();this.pending=0;this.members=new Map();this.states=new Map();this.queue=[];this.reads=[];
  }
  serial(fn){
    if(this.pending>=64)return Promise.reject(unavailable());this.pending++;
    const task=this.tail.then(fn);this.tail=task.catch(()=>{});return task.finally(()=>this.pending--);
  }
  member(id){return this.env.RAFT_MEMBERS.get(this.env.RAFT_MEMBERS.idFromName(this.manifest.config.cluster+'/'+id));}
  config(id){
    const c=this.manifest.config;
    return {cluster:c.cluster,id:String(id),seed:String(BigInt(id)+100n),bootstrap:c.anchors,ceiling:Number(this.manifest.ceiling),scope:{identity:c.identity,cluster:c.cluster,epoch:c.generation,protocol:1},anchors:c.anchors.map((node,i)=>({node,device:'',key:c.anchor_keys[i],incarnation:'1',protocol:1,anchor:true,retired:false,ready_index:'0'}))};
  }
  async provision(identityKey){
    let p=await this.ctx.storage.get('raft-provision');
    if(p)return p;
    // A frozen source must never receive a different UUID or replacement keys.
    if(this.source.gate())throw unavailable();
    const anchors=[];for(let i=0;i<3;i++)anchors.push(await keypair());
    const effects=await keypair(),limits=this.tenant.limits;
    p={config:{cluster:crypto.randomUUID(),generation:'1',identity:this.tenant.identity,signing_key:b64(identityKey),anchors:['1','2','3'],anchor_keys:anchors.map(a=>a.public),effects_key:effects.public,now:Date.now(),grace:{min:limits.minGraceMs,max:limits.maxGraceMs,default:limits.defaultGraceMs}},anchors:anchors.map(a=>a.private),effects:effects.seed};
    await this.ctx.storage.put('raft-provision',p);await this.ctx.storage.sync();return p;
  }
  async ensure(identityKey){
    if(this.ready)return;
    this.provisioning=await this.provision(identityKey);
    if(!this.source.gate())await this.source.freeze(this.provisioning.config);
    this.manifest=await this.source.inventory();
    if(encodeCanonical(this.manifest.config)!==encodeCanonical(this.provisioning.config))throw unavailable();
    const owned=this.source.gate().stage==='raft-owned';
    if(!owned){
      // Each retry advances a bounded page; the cursor is durable only after
      // all three copies accepted identical bytes. A cut merely repeats a row.
      const imported=[];
      for(const id of this.manifest.config.anchors)if(await this.member(id).importReceipt(this.manifest))imported.push(id);
      if(imported.length<3){
        let cursor=await this.ctx.storage.get('raft-import-cursor')??{namespace:'',key:''},count=0;
        for(const row of this.ctx.storage.sql.exec(`SELECT namespace,key,data FROM legacy_rows WHERE namespace>? OR (namespace=? AND key>?) ORDER BY namespace,key LIMIT ${MIGRATION_PAGE+1}`,cursor.namespace,cursor.namespace,cursor.key)){
          if(count++===MIGRATION_PAGE)throw unavailable();
          const copied={namespace:row.namespace,key:row.key,data:bytes(row.data)};
          for(const id of this.manifest.config.anchors){
            if(!imported.includes(id)){await this.member(id).legacyImport(this.config(id),this.manifest,copied);}
          }
          cursor={namespace:row.namespace,key:row.key};await this.ctx.storage.put('raft-import-cursor',cursor);await this.ctx.storage.sync();
        }
        for(const id of this.manifest.config.anchors){
          if(!imported.includes(id)){await this.member(id).legacyImport(this.config(id),this.manifest,null,true);}
        }
      }
    }
    // AFTER publication, absent or unclaimed stores are excluded, never
    // imported again under old voter IDs. Two actual survivors are required.
    const receipts=[];
    for(const id of this.manifest.config.anchors){
      try{
        const receipt=await this.member(id).importReceipt(this.manifest);if(!receipt)continue;
        const member=this.member(id);this.members.set(id,member);receipts.push(receipt);
        this.accept(id,await member.initialize(this.config(id)));
        this.accept(id,await member.drainRecovery());
      }catch(error){this.members.delete(id);this.states.delete(id);if(!owned)throw error;}
    }
    if(this.members.size<2)throw unavailable();
    await this.pump();await this.elect();await this.barrier();
    if(!owned)await this.source.own(receipts);
    this.source.route(()=>true);
    this.watchers=new CommittedWatchers(this.ctx,this.tenant.limits.maxWatchers,this.tenant.watchers.ttlMs,at=>this.ctx.waitUntil(this.ctx.storage.setAlarm(at)));
    this.tenant.watchers=this.watchers;this.ready=true;
    this.effects=new RaftEffectsFacade(this,this.manifest.config.cluster);
  }
  accept(id,e){if(!e)return;if(e.status)this.states.set(String(id),e.status);this.queue.push(...(e.send??[]).map(m=>({...m,from:String(id),to:String(m.to)})));this.reads.push(...(e.reads??[]).map(r=>({...r,node:String(id)})));}
  async input(id,tag,value){const result=await this.members.get(String(id)).hostInput(tag,value);this.accept(id,result);return result;}
  async envelope(from,to,payload,chunk=null){
    const next=BigInt(await this.ctx.storage.get('raft-transport-sequence')??'0')+1n;
    await this.ctx.storage.put('raft-transport-sequence',String(next));await this.ctx.storage.sync();
    const e={scope:this.config(from).scope,from,to,incarnation:'1',destination:'1',sequence:String(next),chunk,digest:Array.from(this.codec.hash(payload).digest,b=>b.toString(16).padStart(2,'0')).join(''),payload:b64(payload),signature:null};
    const rank=this.manifest.config.anchors.indexOf(from);
    const key=await crypto.subtle.importKey('pkcs8',unb64(this.provisioning.anchors[rank]),'Ed25519',false,['sign']);
    e.signature=b64(new Uint8Array(await crypto.subtle.sign('Ed25519',key,enc.encode('codeaf/raft/envelope/1\n'+JSON.stringify(e)))));
    return enc.encode(JSON.stringify(e));
  }
  async pump(){
    for(let n=0;this.queue.length;n++){
      if(n>=2048){this.queue=[];throw unavailable();}
      const m=this.queue.shift(),to=this.members.get(m.to);if(!to)continue;
      const snapshot=this.codec.inspect(4,m.bytes).snapshot;
      try{
        if(snapshot.length){
          const meta=this.codec.inspect(3,snapshot),current=await to.hostInput(0);this.accept(m.to,current);
          // Delayed snapshots below the durable applied index go directly to
          // Raft for its ordinary stale reply; they must not stage over a newer
          // checkpoint or exclude a healthy member from the coordinator.
          if(meta.index>current.status.applied){
            const manifest=JSON.parse(dec.decode(meta.data)),id=await to.hostSnapshot('begin',meta.data);
            for(let offset=0;offset<Number(manifest.chunks);offset++){
              const chunk=await this.members.get(m.from).hostSnapshot('read',id,offset);
              await to.peer(await this.envelope(m.from,m.to,chunk,{generation:id,offset:String(offset),hash:Array.from(this.codec.hash(chunk).digest,b=>b.toString(16).padStart(2,'0')).join('')}));
            }
            await to.hostSnapshot('complete',id);
          }
        }
        const result=await to.peer(await this.envelope(m.from,m.to,m.bytes));this.accept(m.to,result);
        if(snapshot.length)await this.input(m.from,12,{peer:m.to,snapshot:true,success:result.installed===true});
      }catch(error){
        if(snapshot.length)await this.input(m.from,12,{peer:m.to,snapshot:true,success:false});
        this.members.delete(m.to);this.states.delete(m.to);
      }
    }
  }
  leader(){return [...this.states].find(([id,s])=>this.members.has(id)&&s.role===2)?.[0];}
  async elect(){
    if(this.leader())return;
    for(const id of this.members.keys()){await this.input(id,16);await this.pump();if(this.leader())return;}
    throw unavailable();
  }
  async barrier(){
    await this.elect();const leader=this.leader(),id=crypto.randomUUID();this.reads=[];
    await this.input(leader,4,id);await this.pump();
    const read=this.reads.find(r=>r.node===leader&&r.id===id);this.reads=[];
    if(!read) {await this.input(leader,10,id);throw unavailable();}
    return {node:leader,index:read.index};
  }
  async commit(raw){
    await this.elect();const id=this.leader(),proposal=await this.input(id,3,raw);await this.pump();
    const result=await this.members.get(id)?.hostResult(raw,proposal.proposalAfter);if(!result)throw unavailable();
    return {raw:result.raw,value:decodeCanonical(dec.decode(result.raw)),index:result.index};
  }
  async commitEffect(change,now=Date.now()){
    const r=response(this.runtime.call(frame(30).str(this.manifest.config.cluster).u64(now).blob(unb64(this.provisioning.effects)).blob(enc.encode(JSON.stringify(change))).finish()));
    const raw=r.blob();r.end();return (await this.commit(raw)).value;
  }
  async barrierRecord({namespace,key}){const b=await this.barrier();return (await this.members.get(b.node).hostView(b.index,namespace,key)).raw;}
  async barrierOutbox(after,limit){const b=await this.barrier();return (await this.members.get(b.node).hostView(b.index,'outbox','',after)).rows.slice(0,limit);}
  projectJSON(kind,raw){const r=response(this.runtime.call(frame(31).str(kind).blob(raw).finish())),value=dec.decode(r.blob());r.end();return value;}
  project(kind,raw){return JSON.parse(this.projectJSON(kind,raw));}
  async authorize(device,certificate,write=false){
    requireOK((await this.commitEffect({device_seen:{device}},Date.now())).code);
    const b=await this.barrier(),member=this.members.get(b.node),root=await member.hostView(b.index,'state','identity'),actor=await member.hostView(b.index,'state','device/'+segment(device));
    const r=response(this.runtime.call(frame(32).blob(root.raw).blob(actor.raw??new Uint8Array()).str(device).str(certificate).u32(write?1:0).finish()));requireOK(r.str());r.end();return {barrier:b,app:root.app,identity:this.project('identity',root.raw)};
  }
  async listingJSON(b,app){
    const devices=[],cells=[];let identity='null',after='';
    for(;;){const page=await this.members.get(b.node).hostView(b.index,'state','',after);if(!page.rows.length)break;
      // The coordinator holds the only public mutation queue throughout pages.
      for(const row of page.rows){
        after=row.key;
        if(after==='identity')identity=this.projectJSON('identity',row.data);
        else if(after.startsWith('device/'))devices.push(JSON.stringify(unsegment(after.slice(7)))+':'+this.projectJSON('device',row.data));
        else if(after.startsWith('cell/'))cells.push(JSON.stringify(unsegment(after.slice(5)))+':'+this.projectJSON('list_cell',row.data));
      }
    }
    return '{"now":'+app.now+',"identity":'+identity+',"devices":{'+devices.join(',')+'},"cells":{'+cells.join(',')+'}}';
  }
  jsonBytes(raw){return new Response(raw,{headers:{'content-type':'application/json'}});}
  proof(request,body){return {method:request.method,uri:signedUri(this.env,new URL(request.url)),identity_key:b64(fromBase64Url(request.headers.get('codeaf-identity'))),certificate:request.headers.get('codeaf-cert'),at:request.headers.get('codeaf-time'),signature:b64(fromBase64Url(request.headers.get('codeaf-sig'))),body:b64(body)};}
  async dispatch(c,route){
    if(!new URL(c.request.url).pathname.startsWith('/v1/store/'))return this.serial(()=>this.serve(c,route));
    const cert=c.request.headers.get('codeaf-cert');
    await this.serial(async()=>{
      await this.ensure(fromBase64Url(c.request.headers.get('codeaf-identity')));
      await this.authorize(c.device,cert,route.write);
    });
    // Blob waits cannot hold the coordination queue: revoke/rotation must be
    // able to commit during I/O, and Has must not block queued frame puts.
    const result=await route.handler(c,route.args);await this.ctx.storage.sync();
    await this.serial(()=>this.authorize(c.device,cert,route.write));return result;
  }
  async serve(c,route){
    const {request,device,body}=c,url=new URL(request.url),path=url.pathname,cert=request.headers.get('codeaf-cert');
    await this.ensure(fromBase64Url(request.headers.get('codeaf-identity')));
    if(path==='/v1/dir/watch'){
      if(request.headers.get('upgrade')?.toLowerCase()!=='websocket')throw new Wire('upgrade_required',426);
      const holds=parseHolds(url.searchParams.getAll('hold'),this.tenant.limits.maxHolds),beat=parseBeat(url.searchParams.getAll('beat'));
      const a=await this.authorize(device,cert,true);
      return this.watchers.accept(device,Number(a.app.version),Date.now(),{holds,beat,events:url.searchParams.get('events')==='1',receipts:url.searchParams.get('receipts')==='1',certificate:cert});
    }
    const join=/^\/v1\/dir\/requests\/([^/]+)\/(approve|deny)$/.exec(path);
    if(join){await this.authorize(device,cert,true);const code=decodeURIComponent(join[1]).trim().toLowerCase();const box=this.env.LINK_REQUEST.get(this.env.LINK_REQUEST.idFromName(code));await this.effects.admit(box,code,Date.now());}
    let raw;try{raw=v1Command(this.runtime,this.manifest.config.cluster,Date.now(),this.proof(request,body));}catch{throw new Wire('bad_request',400);}
    const result=await this.commit(raw);requireOK(result.value.code);
    const b=await this.barrier(),snapshot=await this.members.get(b.node).hostView(b.index,'state','identity'),identity=this.project('identity',snapshot.raw),version=snapshot.app.version;
    this.watchers.broadcast(version);
    if(join){const code=decodeURIComponent(join[1]).trim().toLowerCase();await this.effects.deliver(this.env.LINK_REQUEST.get(this.env.LINK_REQUEST.idFromName(code)),code,Date.now());unwrap(await this.env.LINK_GATE.get(this.env.LINK_GATE.idFromName('gate')).decided(code));}
    if(request.method==='GET'){
      if(path==='/v1/identity/rotation')return json(rotationView(identity.rotation,Number(snapshot.app.now),this.tenant.limits));
      if(path==='/v1/dir/list'||path==='/v1/dir/presence'){
        const raw=await this.listingJSON(b,snapshot.app),reply=path.endsWith('/presence')?json(presenceOf(JSON.parse(raw).devices,this.watchers.online(),Number(snapshot.app.now))):this.jsonBytes(raw);reply.headers.set('Codeaf-Dir-Version',version);return reply;
      }
      const row=await this.members.get(b.node).hostView(b.index,'state','cell/'+segment(decodeURIComponent(route.args[0])));if(!row.raw)throw new Wire('not_found',404);
      const reply=this.jsonBytes('{"now":'+row.app.now+',"cell":'+this.projectJSON('cell',row.raw)+'}');reply.headers.set('codeaf-conditional-takes','1');return reply;
    }
    if(path==='/v1/identity/rotation'){
      this.watchers.closeAll(CLOSE_ROTATED,'rotated');if(identity.rotation?.retire_at)await this.ctx.storage.setAlarm(identity.rotation.retire_at);
      return json(rotationView(identity.rotation,Number(snapshot.app.now),this.tenant.limits));
    }
    if(path.endsWith('/revoke'))this.watchers.closeDevice(decodeURIComponent(route.args[0]),CLOSE_REVOKED,'revoked');
    if(path.endsWith('/release')||path.endsWith('/archive'))return empty();
    return result.value.view?this.jsonBytes(this.projectJSON('view',result.raw)):empty();
  }
}
