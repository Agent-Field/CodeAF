import { checkStorageHandoff } from '../storage.js';
import { Flight } from '../flight.js';
import { PersonalForward } from './forward.js';
import { PersonalPeers } from './peers.js';
import { coordinationPolicy,CommittedHolds } from './liveness.js';
import { clusterIngress } from './ingress.js';
import { frame, response, HostCodec } from './abi.js';
import { sharedRuntime } from './actor.js';
import { encodeCanonical, decodeCanonical } from './codec.js';
import { b64, unb64, bytes } from './storage.js';
import { v1Command } from './v1.js';
import { projectRecord } from './directory.js';
import { RaftEffectsFacade } from './effects.js';
import { Wire, empty, json, unwrap } from '../wire.js';
import { prefixOf, signedUri } from '../base.js';
import { fromBase64Url } from '../codec.js';
import { parseBeat } from '../presence.js';
import { parseHolds } from '../vouch.js';
import { rotationView } from '../rotation.js';
import { checkTime } from '../verify.js';
import { checkApprovalDevice } from '../link/decide.js';
import { asGrant } from '../link/code.js';
import { CommittedWatchers, CLOSE_REVOKED, CLOSE_ROTATED } from '../watch.js';

const enc=new TextEncoder(),dec=new TextDecoder('utf-8',{fatal:true});
const segment=s=>Array.from(enc.encode(s),b=>b.toString(16).padStart(2,'0')).join('');
const unsegment=s=>dec.decode(Uint8Array.from(s.match(/../g)??[],h=>parseInt(h,16)));
const data=v=>enc.encode(encodeCanonical(v));
const MIGRATION_PAGE=64,COORDINATOR_QUEUE=64,MIN_MEMBERS=2;
const PRESENCE_INTERVAL=30_000,PRESENCE_BATCH=64,READ_ATTEMPTS=3;
const MEMBER_READS=new Set(['hostView','hostResult','hostSelected','hostPersonal','readEvidence','hostWatchState']);
const unavailable=()=>new Wire('unreachable',503,1);
const interruptedRead=()=>Object.assign(unavailable(),{readInterrupted:true});
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
  constructor(ctx,env,tenant,admission,arm){
    this.armAlarm=arm??(async at=>{if(at===null){await ctx.storage.deleteAlarm();return;}const old=await ctx.storage.getAlarm();if(old===null||at<old)await ctx.storage.setAlarm(at)});
    this.policy=coordinationPolicy(sharedRuntime());this.started=Math.floor(performance.now());this.elapsed=0;this.asleep=false;this.wakeVersion=0;this.alarmTail=Promise.resolve();
    this.ctx=ctx;this.env=env;this.tenant=tenant;this.runtime=sharedRuntime();this.codec=new HostCodec(this.runtime);
    this.source=tenant.migration(ctx.storage,this.runtime);this.tail=Promise.resolve();this.pending=0;this.operationTail=Promise.resolve();this.operations=0;this.waiters=new Set();this.progressVersion=0;this.forwarding=new PersonalForward(this);this.members=new Map();this.states=new Map();this.inactiveMembers=new Set();this.queue=[];this.reads=[];
    this.seen=new Map();this.pendingIO=new Map();
    this.readGeneration=0;this.memberWork=0;this.readLease=null;
  }
  serial(fn){
    if(this.pending>=COORDINATOR_QUEUE)return Promise.reject(unavailable());this.pending++;
    const task=this.tail.then(fn);this.tail=task.catch(()=>{});return task.finally(()=>this.pending--);
  }
  operation(fn){
    if(this.operations>=COORDINATOR_QUEUE)return Promise.reject(unavailable());this.operations++;
    const task=this.operationTail.then(fn).catch(error=>{if(error.isolateCapacity)throw unavailable();throw error;});this.operationTail=task.catch(()=>{});return task.finally(()=>this.operations--);
  }
  signal(){this.progressVersion++;for(const resolve of this.waiters)resolve();this.waiters.clear();}
  async progress(deadline,version=this.progressVersion){
    if(Date.now()>=deadline)throw unavailable();
    if(version!==this.progressVersion)return;
    const flight=new Flight(),release=flight.enter();this.waiters.add(release);
    try{if(!await flight.idle(Math.max(0,deadline-Date.now()))||Date.now()>=deadline)throw unavailable();}
    finally{release();this.waiters.delete(release);}
  }
  replica(){return [...this.states].filter(([id])=>this.members.has(id)).sort(([,a],[,b])=>a.applied===b.applied?Number(b.term-a.term):a.applied>b.applied?-1:1)[0]?.[0];}
  leaderID(){const hints=[...this.states].filter(([id])=>this.members.has(id)).sort(([,a],[,b])=>a.term===b.term?(a.applied>b.applied?-1:a.applied<b.applied?1:0):a.term>b.term?-1:1);const hint=String(hints[0]?.[1].leader??0);return this.leader()??(hint!=='0'?hint:this.routeHint??'0');}
  async forward(node,work){
    const token=await this.serial(()=>this.forwarding.begin(node,work));
    const reply=await token.promise;
    // Cold followers may temporarily lose their leader hint. An authenticated
    // forwarding reply preserves routing, while every operation still requires
    // its ordinary quorum barrier and exact durable result.
    const hint=String(reply?.status?.Leader??'0'),term=BigInt(reply?.status?.Term??0);
    const known=[...(this.states?.values()??[])].reduce((max,state)=>state.term>max?state.term:max,this.routeTerm??0n);
    if(hint!=='0'&&term>=known){this.routeHint=hint;this.routeTerm=term;}
    if(!reply||reply.error)throw Object.assign(unavailable(),{forwardID:token.id});
    return reply;
  }
  async refreshRoute(){
    for(const id of this.members.keys())await this.input(id,0);await this.pump();
  }
  async remote(work){
    work={...work,id:work.id??crypto.randomUUID()};
    const target=this.leaderID();
    try{return await this.forward(target,work);}catch(error){
      await this.refreshRoute();
      const next=this.leaderID();
      if(!this.leader()&&next!=='0'&&next!==target)return this.forward(next,{...work,id:error.forwardID??work.id});
      throw error;
    }
  }
  async appliedReplica(index,deadline=Date.now()+this.peers.limits.ForwardTimeout){
    for(;;){const id=this.replica();if(!id||!index||BigInt(index)===0n)throw unavailable();
      if(this.states.get(id).applied>=BigInt(index))return {node:id,index:BigInt(index)};
      await this.progress(deadline);
    }
  }
  invalidateRead(){this.readGeneration++;this.readLease=null;}
  member(id){
    const stub=this.env.RAFT_MEMBERS.get(this.env.RAFT_MEMBERS.idFromName(this.manifest.config.cluster+'/'+id));
    // Default to mutation: a new member RPC cannot silently bypass the fence.
    return new Proxy(stub,{get:(target,name)=>{
      const call=Reflect.get(target,name,target);if(typeof call!=='function')return call;
      return (...args)=>{
        if(MEMBER_READS.has(name))return Reflect.apply(call,target,args);
        this.invalidateRead();this.memberWork++;
        return Promise.resolve().then(()=>Reflect.apply(call,target,args)).finally(()=>{this.memberWork--;});
      };
    }});
  }
  leaseCurrent(lease=this.readLease){return lease&&lease.generation===this.readGeneration&&!this.memberWork&&!this.asleep&&performance.now()<lease.until;}
  async confirmRead(b){
    if(this.memberWork||this.coordinationBusy())return null;
    const generation=this.readGeneration,started=performance.now();
    const selected=await this.members.get(b.node).readEvidence(),conf=this.configuration(selected.conf);
    const singleton=this.birth&&conf.voters.length===1&&String(conf.voters[0])===this.birth.voters[0];
    if(conf.joint||conf.outgoing?.length||conf.voters.length!==3&&!singleton)return null;
    const voters=conf.voters.map(String).filter(id=>this.members.has(id));
    if(voters.length<(singleton?1:2))return null;
    const evidence=await Promise.all(voters.map(async node=>({node,...await this.members.get(node).readEvidence()})));
    if(generation!==this.readGeneration||this.memberWork||this.coordinationBusy())return null;
    if(!selected.clean||BigInt(selected.index)<BigInt(b.index)||!evidence.every(e=>e.clean&&e.index===selected.index&&e.term===selected.term&&e.conf===selected.conf&&e.leader===selected.leader))return null;
    const leader=selected.leader;if(leader==='0'||!conf.voters.map(String).includes(leader))return null;
    if(this.members.has(leader)&&!evidence.some(e=>e.node===leader&&e.role===2))return null;
    // Every quorum intersects these hosted voters. With clean durable tails,
    // another commit needs new anchor traffic, which invalidates this fence
    // before delivery. The deadline additionally bounds idle reuse.
    return this.readLease={node:b.node,index:BigInt(selected.index),term:selected.term,activation:selected.activation,generation,until:started+this.policy.tick_every*2};
  }
  async readAuthority(fresh=false){
    this.lastReadAt=Math.max(this.elapsed,performance.now()-this.started);
    if(!fresh&&this.leaseCurrent())return this.readLease;
    const b=await this.barrier(),lease=!fresh&&await this.confirmRead(b);
    if(lease)return lease;
    const evidence=await this.members.get(b.node).readEvidence();
    return {...b,activation:evidence.activation};
  }
  checkRead(b){if(b.generation!==undefined&&!this.leaseCurrent(b))throw interruptedRead();}
  async retryRead(read){
    const deadline=Date.now()+(this.peers?.limits.ForwardTimeout??this.policy.tick_every);
    for(let attempt=0;;attempt++){
      try{return await read(attempt>0);}
      catch(error){
        if(!error.readInterrupted||attempt+1>=READ_ATTEMPTS||Date.now()>=deadline)throw error;
        this.invalidateRead();
      }
    }
  }
  async readView(b,namespace,key,after=null){
    this.checkRead(b);
    let row;try{row=await this.members.get(b.node).hostView(b.index,namespace,key,after,b.activation??null);}catch(error){this.invalidateRead();throw unavailable();}
    if(b.generation===undefined){
      b.applied??=String(row.app.applied);
      if(b.applied!==String(row.app.applied))throw interruptedRead();
    }
    this.checkRead(b);return row;
  }
  readNow(committed=0){
    const floor=this.tenant.meta.get('read-time')??0,now=Math.max(Date.now(),Number(committed),floor);
    if(now>floor)this.tenant.meta.set('read-time',now);return now;
  }
  async finishRead(b,reply){
    await this.ctx.storage.sync();
    if(b.generation===undefined)await this.readView(b,'state','identity');
    this.checkRead(b);return reply;
  }
  config(id){
    const c=this.manifest.config;
    return {cluster:c.cluster,id:String(id),seed:String(BigInt(id)+100n),bootstrap:this.source.gate()?.birth?.voters??c.anchors,...(this.source.gate()?.birth?{birth:this.source.gate().birth}:{}),ceiling:Number(this.manifest.ceiling),scope:{identity:c.identity,cluster:c.cluster,epoch:c.generation,protocol:1},anchors:c.anchors.map((node,i)=>({node,device:'',key:c.anchor_keys[i],incarnation:'1',protocol:1,anchor:true,retired:false,ready_index:'0'}))};
  }
  async provision(identityKey){
    let p=await this.ctx.storage.get('raft-provision');
    if(p)return p;
    // A frozen source must never receive a different UUID or replacement keys.
    if(this.source.gate())throw unavailable();
    const anchors=[];for(let i=0;i<3;i++)anchors.push(await keypair());
    const effects=await keypair(),limits=this.tenant.limits;
    p={singleton:true,config:{cluster:crypto.randomUUID(),generation:'1',identity:this.tenant.identity,signing_key:b64(identityKey),anchors:['1','2','3'],anchor_keys:anchors.map(a=>a.public),effects_key:effects.public,now:Date.now(),grace:{min:limits.minGraceMs,max:limits.maxGraceMs,default:limits.defaultGraceMs}},anchors:anchors.map(a=>a.private),effects:effects.seed};
    await this.ctx.storage.put('raft-provision',p);await this.ctx.storage.sync();return p;
  }
  async ensure(identityKey){
    // Recover inactive members without replacing live routing or sockets.
    if((this.ready||this.initialized)&&this.inactiveMembers.size){
      for(const id of [...this.inactiveMembers]){
        try{
          const member=this.member(id);if(!await member.importReceipt(this.manifest,this.birth??this.source.gate()?.birth??null))continue;
          const initialized=await member.initialize(this.config(id));if(initialized.capacity)continue;
          this.members.set(id,member);this.accept(id,initialized);this.accept(id,await member.drainRecovery());
        }catch{this.members.delete(id);this.states.delete(id);}
      }
      if([...this.members.keys()].filter(id=>!this.inactiveMembers.has(id)).length<(this.birth?1:MIN_MEMBERS))throw unavailable();
    }
    if(this.ready)return;
    if(this.initialized)return this.completeEnsure();
    this.provisioning=await this.provision(identityKey);
    if(!this.source.gate())await this.source.freeze(this.provisioning.config,this.provisioning.singleton===true);
    this.manifest=await this.source.inventory();
    if(encodeCanonical(this.manifest.config)!==encodeCanonical(this.provisioning.config))throw unavailable();
    const owned=this.source.gate().stage==='raft-owned';
    this.birth=this.source.gate().birth??null;
    const imports=this.birth?this.birth.voters:this.manifest.config.anchors;
    if(!owned){
      // Each retry advances a bounded page; the cursor is durable only after
      // all three copies accepted identical bytes. A cut merely repeats a row.
      const imported=[];
      for(const id of imports)if(await this.member(id).importReceipt(this.manifest,this.birth))imported.push(id);
      if(imported.length<imports.length){
        let cursor=await this.ctx.storage.get('raft-import-cursor')??{namespace:'',key:''},count=0;
        for(const row of this.ctx.storage.sql.exec(`SELECT namespace,key,data FROM legacy_rows WHERE namespace>? OR (namespace=? AND key>?) ORDER BY namespace,key LIMIT ${MIGRATION_PAGE+1}`,cursor.namespace,cursor.namespace,cursor.key)){
          if(count++===MIGRATION_PAGE)throw unavailable();
          const copied={namespace:row.namespace,key:row.key,data:bytes(row.data)};
          for(const id of imports){
            if(!imported.includes(id)){await this.member(id).legacyImport(this.config(id),this.manifest,copied);}
          }
          cursor={namespace:row.namespace,key:row.key};await this.ctx.storage.put('raft-import-cursor',cursor);await this.ctx.storage.sync();
        }
        for(const id of imports){
          if(!imported.includes(id)){await this.member(id).legacyImport(this.config(id),this.manifest,null,true);}
        }
      }
    }
    // AFTER publication, absent or unclaimed stores are excluded, never
    // imported again under old voter IDs. Two actual survivors are required.
    const receipts=[];
    for(const id of (owned?this.manifest.config.anchors:imports)){
      try{
        const receipt=await this.member(id).importReceipt(this.manifest,this.birth??this.source.gate()?.birth??null);if(!receipt)continue;
        const member=this.member(id);this.members.set(id,member);receipts.push(receipt);
        const initialized=await member.initialize(this.config(id));
        // Durable import receipts cover all anchors; serving needs a live
        // majority even when admission pressure delays the remaining copy.
        if(initialized.capacity){this.inactiveMembers.add(id);this.members.delete(id);this.states.delete(id);continue;}
        this.accept(id,initialized);this.elapsed=Math.max(this.elapsed,initialized.elapsed??0);
        this.accept(id,await member.drainRecovery());
      }catch(error){this.inactiveMembers.add(id);this.members.delete(id);this.states.delete(id);if(!owned)throw error;}
    }
    if(this.members.size<(this.birth?1:MIN_MEMBERS))throw unavailable();
    if(owned){await this.pump();await this.restorePeers();}
    else{await this.pump();await this.elect();await this.barrier();}
    if(!owned){await this.source.own(receipts);if(this.birth){await this.ctx.storage.put('raft-formation-imported',imports);await this.ctx.storage.sync();}}
    this.started=Math.floor(performance.now())-this.elapsed;
    this.source.route(()=>true);
    this.watchers=new CommittedWatchers(this.ctx,this.tenant.limits.maxWatchers,this.tenant.watchers.ttlMs,at=>this.ctx.waitUntil(this.armAlarm(at)));
    this.tenant.watchers=this.watchers;
    this.effects=new RaftEffectsFacade(this,this.manifest.config.cluster);
    this.holds=new CommittedHolds(this,this.policy);
    this.holds.opened(Date.now(),this.watchers.holdEvidence(Date.now()).some(socket=>socket.holds.length));
    // Transport and follower clocks remain available while a cold cluster
    // cannot yet answer authority reads. Publication still needs a quorum.
    this.initialized=true;await this.wakeMaintenance(true);
    return this.completeEnsure();
  }
  async completeEnsure(){
    const b=await this.barrier(),header=await this.members.get(b.node).hostView(b.index,'membership','registry');
    this.forming=!!this.birth&&!header.raw;
    if(this.forming){
      const conf=this.configuration(header.app.conf);
      if(conf.joint||conf.outgoing?.length||conf.voters.length!==3){
        const root=await this.members.get(b.node).hostView(b.index,'state','identity');await this.rememberRetirement(this.project('identity',root.raw));
        if(!this.peers)await this.restorePeers(header.app.conf);
        await this.wakeMaintenance();this.ready=true;return;
      }
    }
    if(!header.raw){
      const anchors=this.config(this.manifest.config.anchors[0]).anchors;
      requireOK((await this.anchorCommand({init_membership:{anchors,limit:this.policy.max_member_records}},Date.now())).value.code);
    }
    const root=await this.barrierRecord({namespace:'state',key:'identity'});
    await this.rememberRetirement(this.project('identity',root));
    if(!this.peers)await this.restorePeers();else await this.peers.recover();
    this.forming=false;
    this.membershipPending=!!(header.raw&&decodeCanonical(dec.decode(header.raw)).pending);
    await this.reconcileMembership();
    await this.wakeMaintenance();this.ready=true;
  }
  async restorePeers(initialConf=null){
    const generation=await this.ctx.storage.get('raft-router-generation')??'0';
    if(typeof generation!=='string'||!/^(0|[1-9][0-9]*)$/.test(generation)||BigInt(generation)>=(1n<<64n)-1n)throw unavailable();
    const saved=await this.ctx.storage.get('raft-router-sleep'),active=await this.ctx.storage.get('raft-maintenance');
    if(Number.isSafeInteger(saved?.elapsed)&&saved.elapsed>=0)this.elapsed=Math.max(this.elapsed,saved.elapsed);
    if(saved?.generation===generation&&!active){
      const peers=new PersonalPeers(this,generation);peers.restoring=true;await peers.refresh();
      const live=Object.fromEntries(Object.entries(peers.view).filter(([,peer])=>!peer.revoked));
      const external=Object.keys(live).filter(node=>live[node].voter&&!this.members.has(node));
      if(external.length&&encodeCanonical(live)===encodeCanonical(saved.view)&&external.every(node=>['peer','forward'].every(kind=>peers.sockets().some(ws=>{const a=ws.deserializeAttachment();return a.node===node&&a.kind===kind&&a.generation===generation;})))){
        for(const [node,cursors]of Object.entries(saved.cursors)){
          const box=peers.boxes.get(node);if(!box)throw unavailable();
          for(const key of ['next','issued','acked']){if(cursors[key].length!==peers.limits.Slots.length)throw unavailable();box[key]=cursors[key].map(BigInt);}
          peers.acks.set(node,box.acked.map(String));
        }
        peers.progress=new Map(saved.progress);peers.restoring=false;this.peers=peers;this.routeHint=saved.leader;return;
      }
    }
    const next=String(BigInt(generation)+1n);
    await this.ctx.storage.put('raft-router-generation',next);await this.ctx.storage.sync();
    this.peers=new PersonalPeers(this,next);
    for(const ws of this.peers.sockets())this.peers.close(ws);
    this.peers.restoring=initialConf!==null;
    try{await this.peers.refresh();}finally{this.peers.restoring=false;}
    await this.peers.recover(initialConf);
  }
  accept(id,e){if(!e)return;if(e.status){this.inactiveMembers?.delete(String(id));this.states.set(String(id),e.status);}
    if(e.applicationVersion!==undefined)this.appliedVersion=this.appliedVersion===undefined?BigInt(e.applicationVersion):this.appliedVersion>BigInt(e.applicationVersion)?this.appliedVersion:BigInt(e.applicationVersion);this.queue.push(...(e.send??[]).map(m=>({...m,from:String(id),to:String(m.to)})));this.reads.push(...(e.reads??[]).map(r=>({...r,node:String(id)})));this.signal();}
  async input(id,tag,value){const result=await this.members.get(String(id)).hostInput(tag,value);this.accept(id,result);return result;}
  // Reserve durable sequence ranges before emission. Concurrent public and
  // peer turns share one allocator, and recovery skips every unused sequence.
  async nextSequence(){
    for(;;){
      if(this.sequence!==undefined&&this.sequence<this.sequenceCeiling)return String(++this.sequence);
      this.sequencePending??=(async()=>{
        const range=await this.ctx.storage.transaction(async tx=>{
          const saved=await tx.get('raft-transport-sequence')??'0',count=BigInt(this.policy.sequence_reservation);
          if(typeof saved!=='string'||!/^(0|[1-9][0-9]*)$/.test(saved)||BigInt(saved)>(1n<<64n)-1n-count)throw unavailable();
          const ceiling=BigInt(saved)+count;await tx.put('raft-transport-sequence',String(ceiling));return {start:saved,ceiling:String(ceiling)};
        });await this.ctx.storage.sync();this.sequence=BigInt(range.start);this.sequenceCeiling=BigInt(range.ceiling);
      })().finally(()=>{this.sequencePending=null;});
      await this.sequencePending;
    }
  }
  async envelope(from,to,payload,chunk=null){
    const next=await this.nextSequence();
    const e={scope:this.config(from).scope,from,to,incarnation:'1',destination:this.peers?.view[to]?.incarnation??'1',sequence:String(next),chunk,digest:Array.from(this.codec.hash(payload).digest,b=>b.toString(16).padStart(2,'0')).join(''),payload:b64(payload),signature:null};
    const rank=this.manifest.config.anchors.indexOf(from);
    const key=await crypto.subtle.importKey('pkcs8',unb64(this.provisioning.anchors[rank]),'Ed25519',false,['sign']);
    e.signature=b64(new Uint8Array(await crypto.subtle.sign('Ed25519',key,enc.encode('codeaf/raft/envelope/1\n'+JSON.stringify(e)))));
    return enc.encode(JSON.stringify(e));
  }
  hash(raw){return Array.from(this.codec.hash(raw).digest,b=>b.toString(16).padStart(2,'0')).join('');}
  async pump(){
    if(this.pumping){await this.pumping;if(this.queue.length)return this.pump();return;}
    if(this.peers)this.peers.blocked=new Set();
    this.pumping=this.pumpQueue();
    try{await this.pumping;}finally{this.pumping=null;}
    await this.notifyApplied();
    if(this.peers)await this.peers.push();
  }
  async notifyApplied(){
    if(!this.watchers||this.appliedVersion===undefined||this.notifiedVersion===this.appliedVersion)return;
    const id=this.replica();if(!id)return;
    const index=this.states.get(id).applied,devices=[...new Set([...this.ctx.getWebSockets().filter(ws=>!ws.deserializeAttachment()?.clusterPeer).map(ws=>this.ctx.getTags(ws)[0]),...this.pendingIO.keys()])];
    for(let offset=0;offset===0||offset<devices.length;offset+=64){
      const state=await this.members.get(id).hostWatchState(index,devices.slice(offset,offset+64)),version=BigInt(state.version);
      await this.rememberRetirement(state.identity);
      if(this.notifiedVersion!==version){this.watchers.broadcast(String(version));this.notifiedVersion=version;}
      if(state.identity.rotation){this.watchers.closeAll(CLOSE_ROTATED,'rotated');for(const device of this.pendingIO.keys())this.stopIO(device,'rotated');}
      for(const device of state.revoked){this.tenant.fabric.revokeClient(device);this.watchers.closeDevice(device,CLOSE_REVOKED,'revoked');this.stopIO(device,'revoked');}
    }
    if(this.retireAt)await this.armAlarm(this.retireAt);
  }
  async pumpQueue(){
    for(let n=0;this.queue.length;n++){
      if(n>=2048){this.queue=[];throw unavailable();}
      const m=this.queue.shift(),to=this.members.get(m.to),snapshot=this.codec.inspect(4,m.bytes).snapshot;
      if(!to){
        if(this.forming&&this.manifest.config.anchors.includes(m.to)){if(snapshot.length)await this.input(m.from,12,{peer:m.to,snapshot:true,success:false});}
        else if(this.peers)await this.peers.send(m);
        continue;
      }
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
        const hash=this.hash(m.bytes),chunk=m.bytes.length>this.policy.max_message_bytes?{generation:hash,offset:'0',hash}:null;
        const result=await to.peer(await this.envelope(m.from,m.to,m.bytes,chunk));this.accept(m.to,result);
        if(snapshot.length)await this.input(m.from,12,{peer:m.to,snapshot:true,success:result.installed===true});
      }catch(error){
        if(snapshot.length)await this.input(m.from,12,{peer:m.to,snapshot:true,success:false});
        this.inactiveMembers?.add(m.to);this.members.delete(m.to);this.states.delete(m.to);
      }
    }
  }
  leader(){const term=[...this.states.values()].reduce((max,s)=>s.term>max?s.term:max,0n);return [...this.states].find(([id,s])=>this.members.has(id)&&s.term===term&&s.role===2)?.[0];}
  externalVoter(){return Object.entries(this.peers?.view??{}).some(([node,peer])=>peer.voter&&!peer.revoked&&!this.manifest.config.anchors.includes(node));}
  async elect(){
    if(this.leader()||(this.leaderID()!=='0'&&!this.members.has(this.leaderID())))return;
    if(this.transferTarget&&this.forwarding.socket(this.transferTarget.node)){const deadline=Date.now()+this.peers.limits.ForwardTimeout;while(!this.leader()&&(this.leaderID()==='0'||this.members.has(this.leaderID())))await this.progress(deadline);return;}
    if(this.externalVoter())throw unavailable();
    for(const id of this.members.keys()){await this.input(id,16);await this.pump();if(this.leader())return;}
    throw unavailable();
  }
  async barrier(id=crypto.randomUUID(),deadline=Date.now()+(this.peers?.limits.ForwardTimeout??this.policy.tick_every),background=false){
    await this.elect();const leader=this.leader();
    if(!leader){try{const reply=await this.remote({action:background?'idle-read':'read',id});return this.appliedReplica(reply.barrier,deadline);}catch(error){if(this.leader())return this.barrier(id,deadline,background);throw error;}}
    try{await this.input(leader,background?15:4,id);await this.pump();}
    catch(error){await this.refreshRoute();if(this.leader()!==leader)return this.barrier(id,deadline,background);throw error;}
    try{for(;;){
      if(this.leader()!==leader){await this.refreshRoute();if(this.leader()!==leader)return this.barrier(id,deadline,background);}
      const at=this.reads.findIndex(r=>r.node===leader&&r.id===id);if(at>=0)return this.reads.splice(at,1)[0];
      if(!this.externalVoter())throw unavailable();await this.progress(deadline);
    }}
    finally{await this.input(leader,10,id);this.reads=this.reads.filter(r=>r.id!==id);}
  }
  async commit(raw,deadline=Date.now()+(this.peers?.limits.ForwardTimeout??this.policy.tick_every)){
    await this.elect();const id=this.leader();
    if(!id){
      let reply;try{reply=await this.remote({action:'submit',command:b64(raw)});}catch(error){if(this.leader())return this.commit(raw,deadline);throw error;}
      if(!reply.result)throw unavailable();const replica=await this.appliedReplica(reply.applied,deadline);
      if(reply.result.view_index){
        if(BigInt(reply.result.view_index)!==BigInt(reply.applied))throw unavailable();
        const result=await this.members.get(replica.node).hostResult(raw,BigInt(reply.applied)-1n);
        if(!result||BigInt(result.index)!==BigInt(reply.applied))throw unavailable();
        const value=this.project('typed_result',result.raw);
        if(value.code!==reply.result.code||value.now!==reply.result.now||value.version!==reply.result.version)throw unavailable();
        await this.peers.refresh();return {raw:result.raw,value,index:BigInt(reply.applied)};
      }
      await this.peers.refresh();return {raw:data(reply.result),value:reply.result,index:BigInt(reply.applied)};
    }
    let proposal;
    try{proposal=await this.input(id,3,raw);await this.pump();}
    catch(error){await this.refreshRoute();if(this.leader()!==id)return this.commit(raw,deadline);throw error;}
    for(;;){
      if(this.leader()!==id){await this.refreshRoute();if(this.leader()!==id)return this.commit(raw,deadline);}
      const version=this.progressVersion,result=await this.members.get(id)?.hostResult(raw,proposal.proposalAfter);
      if(result){if(this.peers)await this.peers.refresh();return {raw:result.raw,value:this.project('typed_result',result.raw),index:result.index};}
      // All hosted persistence and replies have drained. Only an external
      // voter can still make progress without another input to this operation.
      if(!this.externalVoter())throw unavailable();
      await this.progress(deadline,version);
    }
  }
  async commitV1(raw){
    const commands=Array.isArray(raw)?raw:[raw],width=this.peers.limits.Slots[0];
    for(let offset=0;offset<commands.length-1;offset+=width){
      await Promise.all(commands.slice(offset,Math.min(commands.length-1,offset+width)).map(async command=>{
        const result=await this.commit(command);requireOK(result.value.code);return result;
      }));
    }
    const result=await this.commit(commands.at(-1));requireOK(result.value.code);return result;
  }
  async commitEffect(change,now=Date.now()){
    const r=response(this.runtime.call(frame(30).str(this.manifest.config.cluster).u64(now).blob(unb64(this.provisioning.effects)).blob(data(change)).finish()));
    const raw=r.blob();r.end();return (await this.commit(raw)).value;
  }
  async barrierRecord({namespace,key}){const b=await this.barrier();return (await this.members.get(b.node).hostView(b.index,namespace,key)).raw;}
  async barrierOutbox(after,limit){const b=await this.barrier();return (await this.members.get(b.node).hostView(b.index,'outbox','',after)).rows.slice(0,limit);}
  projectJSON(kind,raw){return projectRecord(this.runtime,kind,raw);}
  project(kind,raw){return JSON.parse(this.projectJSON(kind,raw));}
  async authorize(device,certificate,write=false,fresh=false,allowClient=false){
    await this.wakeMaintenance();
    const b=await this.readAuthority(fresh),root=await this.readView(b,'state','identity'),actor=await this.readView(b,'state','device/'+segment(device));
    const actorRec=actor.raw?this.project('device',actor.raw):null;
    if(!allowClient&&actorRec?.kind==='client')throw new Wire('unauthorized',401);
    const r=response(this.runtime.call(frame(32).blob(root.raw).blob(actor.raw??new Uint8Array()).str(device).str(certificate).u32((write?1:0)|(allowClient?2:0)).finish()));requireOK(r.str());r.end();
    if(actorRec?.kind!=='client')this.noteSeen(device);
    return {barrier:b,app:root.app,identity:this.project('identity',root.raw),actor:actorRec};
  }
  async recheckAuthority(previous,device,certificate,write,fresh){
    const current=await this.readAuthority(fresh);
    const applied=current.generation===undefined?(await this.readView(current,'state','identity')).app.applied:current.index;
    const changed=current.node!==previous.barrier.node||BigInt(applied)!==BigInt(previous.app.applied);
    const authority=changed?await this.authorize(device,certificate,write,fresh):null;
    await this.finishRead(authority?.barrier??current,null);
  }
  // Presence is advisory, like unflushed store statistics. It may be lost on
  // eviction, but never delays authority checks or grows with request volume.
  noteSeen(device,now=Date.now()){
    const pending=this.seen.get(device);
    if(pending){pending.at=now;return;}
    if(this.seen.size>=PRESENCE_BATCH)return;
    this.seen.set(device,{at:now,due:now+PRESENCE_INTERVAL});
    this.ctx.waitUntil(this.wakeMaintenance());
  }
  async flushSeen(now){
    for(const [device,pending]of this.seen){
      if(pending.due>now)continue;
      const at=pending.at;
      try{requireOK((await this.commitEffect({device_seen:{device}},at)).code);}catch{return;}
      if(pending.at===at)this.seen.delete(device);
      else pending.due=now+PRESENCE_INTERVAL;
      return;
    }
  }
  async listingJSON(b,app){
    const devices=[],cells=[];let identity='null',after='';
    for(;;){const page=await this.readView(b,'state','',after);if(!page.rows.length)break;
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
  proof(request,body){const base=prefixOf(this.env);return {...(base?{base_path:base}:{}),method:request.method,uri:signedUri(this.env,new URL(request.url)),identity_key:b64(fromBase64Url(request.headers.get('codeaf-identity'))),certificate:request.headers.get('codeaf-cert'),at:request.headers.get('codeaf-time'),signature:b64(fromBase64Url(request.headers.get('codeaf-sig'))),body:b64(body)};}
  configuration(raw){const r=response(this.runtime.call(frame(35).blob(unb64(raw)).finish())),conf=decodeCanonical(dec.decode(r.blob()));r.end();return conf;}
  async anchorCommand(change,now){
    const id=this.manifest.config.anchors[0],actor='anchor/'+id;
    const saved=await this.barrierRecord({namespace:'sessions',key:segment(actor)+'/'+segment('maintenance')});
    const key=await crypto.subtle.importKey('pkcs8',unb64(this.provisioning.anchors[0]),'Ed25519',true,['sign']);
    const pkcs8=new Uint8Array(await crypto.subtle.exportKey('pkcs8',key));
    const r=response(this.runtime.call(frame(34).str(this.manifest.config.cluster).u64(id).u64(now).blob(pkcs8.slice(-32)).blob(saved??new Uint8Array()).blob(data(change)).finish()));
    const command=r.blob();r.end();return this.commit(command);
  }
  async rememberRetirement(identity){
    const at=identity.rotation?.retire_at;
    if(this.retirementKnown&&at===this.retireAt)return;
    // Cache only confirmed storage so an interrupted write remains repairable.
    this.retirementKnown=false;
    if(at)await this.ctx.storage.put('raft-retire-at',at);
    else await this.ctx.storage.delete('raft-retire-at');
    this.retireAt=at;this.retirementKnown=true;
  }
  wakeMaintenance(wake=false){
    this.wakePending=this.wakePending||this.asleep||wake;this.asleep=false;this.wakeVersion++;
    // Peer ingress has its own queue. Serialize only timer transitions so a
    // wake racing the final sleep cannot lose its durable maintenance hint.
    const task=this.alarmTail.then(async()=>{
      if(!this.maintaining){await this.ctx.storage.put('raft-maintenance',true);await this.ctx.storage.sync();this.maintaining=true;}
      await this.armAlarm(Date.now()+this.policy.tick_every);
    });
    this.alarmTail=task.catch(()=>{});return task;
  }
  sleepMaintenance(version){
    const task=this.alarmTail.then(async()=>{
      if(version!==this.wakeVersion){this.asleep=false;return;}
      await this.ctx.storage.delete('raft-maintenance');await this.ctx.storage.sync();this.maintaining=false;
      await this.armAlarm(null);
      this.watchers.liveness.rearm();
      if(this.retireAt)await this.armAlarm(this.retireAt);
      if(this.holds.next!==null)await this.armAlarm(this.holds.next);
    });
    this.alarmTail=task.catch(()=>{});return task;
  }
  coordinationBusy(){
    if(this.forwarding.pending.size||this.queue.length||this.peers.pending)return true;
    for(const [node,peer]of Object.entries(this.peers.view))if(peer.voter&&!peer.revoked){
      if(this.peers.retryTargets.has(node)||this.peers.boxes.get(node)?.queues.some(q=>q.length)||this.peers.snapshots.has(node))return true;
    }
    return false;
  }
  // Replies produced by this turn must drain before sleep without becoming
  // another application activity sample that cancels the new marker.
  async drainCoordination(){
    const deadline=Date.now()+this.peers.limits.ForwardTimeout;
    while(this.coordinationBusy()){
      const version=this.progressVersion;
      if([...this.members.keys()].some(node=>this.peers.boxes.get(node)?.queues.some(q=>q.length)))await this.peers.serial(()=>this.peers.anchors());
      await this.pump();
      if(!this.coordinationBusy())return;
      await this.progress(deadline,version);
    }
  }
  async maintenance(now=Date.now(),elapsed=Math.floor(performance.now())){
    if(this.asleep)return;
    const wakeVersion=this.wakeVersion,busy=this.coordinationBusy()||!!this.wakePending||this.seen.size>0;
    this.wakePending=false;
    this.elapsed=Math.max(this.elapsed,elapsed-this.started);
    // Followers must advance their election clocks before any authority read:
    // an absent leader cannot answer membership or hold-renewal requests.
    const plans=new Map();
    for(const [id,member]of this.members){
      try{
        this.accept(id,await member.drainRecovery());
        const batch=await member.scheduled();
        if(batch){this.accept(id,{send:batch.send.map(m=>({to:m.to,bytes:unb64(m.bytes)}))});await this.pump();await member.acknowledgeScheduled(batch.id);}
        const effects=await member.schedule(this.elapsed,now,busy);this.accept(id,effects);plans.set(id,effects.plan);
      }catch{this.inactiveMembers?.add(id);this.members.delete(id);this.states.delete(id);}
    }
    await this.pump();
    try{
      const leader=this.leader(),remote=leader?null:this.leaderID();
      if(!leader&&(remote==='0'||this.members.has(remote)||!this.forwarding.socket(remote)))return;
      if(this.membershipPending)await this.reconcileMembership();
      // Local durable state is only a work hint. The shared planner obtains
      // its own quorum read before proposing the reverse joint swap.
      if(leader){
        const row=await this.members.get(leader).hostView(this.states.get(leader).applied,'membership','registry');
        if(row.raw&&decodeCanonical(dec.decode(row.raw)).leadership)await this.reconcileLeadership();
      }
      if(this.holds.next!==null)await this.holds.renew(now,this.watchers.holdEvidence(now));
      const replica=this.leader()??this.replica(),member=this.members.get(replica);
      let scan=this.outboxScan??'';
      const app=await member.hostView(this.states.get(replica).applied,'outbox','',scan);
      this.outboxScan=app.rows.at(-1)?.key??'';
      // Only join requests are delivery records. The same namespace also keeps
      // upload receipts and the raw body pages of a large request that was cut
      // short, and those are not canonical records to decode.
      if(app.rows.some(row=>{if(!row.key.startsWith('join/'))return false;const value=decodeCanonical(dec.decode(row.data));return value.decision&&!value.outcome;}))await this.effects.replay(code=>this.env.LINK_REQUEST.get(this.env.LINK_REQUEST.idFromName(code)),scan,now);
      if(this.forming){
        this.formationSoon=false;
        try{this.formationSoon=await this.formNext();}catch{}
        if(this.formationSoon){await this.flushSeen(now);return;}
      }
      let remoteAck,remoteStatus;
      const schedule=async(acks=[])=>{
        if(remote){
          const reply=await this.forward(remote,{action:'schedule',now,schedule:{now:this.elapsed,busy,marker:{index:'0',term:'0',barrier:'0',voters:null},acks}});
          if(!reply.plan)throw unavailable();remoteAck=reply.sleep_ack;remoteStatus=reply.status;return reply.plan;
        }
        const id=this.leader(),effects=await this.members.get(id).schedule(this.elapsed,now,busy,acks);this.accept(id,effects);await this.pump();return effects.plan;
      };
      let plan=remote?await schedule():plans.get(this.leader());
      if(plan?.barrier){
        const id=crypto.randomUUID();
        if(remote){const reply=await this.forward(remote,{action:'idle-read',id});await this.appliedReplica(reply.barrier);}
        else{const leader=this.leader();await this.input(leader,15,id);await this.pump();if(!this.reads.some(r=>r.node===leader&&r.id===id))throw unavailable();this.reads=this.reads.filter(r=>r.id!==id);}
        plan=await schedule();
      }
      if(plan?.prepare){
        await this.drainCoordination();
        const id=this.leader()??this.replica(),s=this.states.get(id),view=await this.members.get(id).hostView(s.applied,'state','identity');
        // IdleRead freshness is consumed at the same applied position. Building
        // this restricted command locally must not replace it with a user read.
        const saved=await this.members.get(id).hostView(s.applied,'sessions',segment('anchor/'+this.manifest.config.anchors[0])+'/'+segment('maintenance'));
        const pkcs8=unb64(this.provisioning.anchors[0]);
        const r=response(this.runtime.call(frame(34).str(this.manifest.config.cluster).u64(this.manifest.config.anchors[0]).u64(now).blob(pkcs8.slice(-32)).blob(saved.raw??new Uint8Array()).blob(data({prepare_quiescence:{barrier:String(s.applied),term:String(remote?remoteStatus.Term:s.term),voters:this.configuration(view.app.conf).voters}})).finish()));
        const raw=r.blob();r.end();requireOK((await this.commit(raw)).value.code);
        if(!remote){await this.input(this.leader(),1);await this.pump();}
      }
      const id=this.leader()??this.replica(),s=this.states.get(id),row=await this.members.get(id).hostView(s.applied,'membership','quiescence');
      const marker=row.raw&&decodeCanonical(dec.decode(row.raw));
      if(marker&&marker.index!=='0'){
        try{
          const acks=[];
          for(const voter of marker.voters)if(this.members.has(voter))acks.push(await this.members.get(voter).sleepAck(marker.index,marker.term));
          await this.drainCoordination();
          if(remote){await schedule();if(remoteAck)acks.push(remoteAck);}
          let sleeping=remote?(await schedule(acks)).sleeping:true;
          for(const [node,member]of this.members){const effects=await member.schedule(this.elapsed,now,busy,acks);this.accept(node,effects);sleeping&&=effects.plan.sleeping;}
          await this.pump();this.asleep=sleeping&&!this.coordinationBusy()&&wakeVersion===this.wakeVersion;
          if(this.asleep){
            // Every sleeping replica exposes the same committed peer view.
            const replica=this.replica();this.peers.viewApplied=replica+'/'+this.states.get(replica).applied;
            await this.peers.quiesce();await this.pump();await this.sleepMaintenance(wakeVersion);
            for(const [node,member]of this.members){
              if(!this.asleep||wakeVersion!==this.wakeVersion)break;
              const state=this.states.get(node),hint=this.leaderID();
              await member.releaseSleeping(this.elapsed,now,acks);
              // Durable progress still authenticates peers, but grants no authority.
              if(!this.states.has(node)||this.states.get(node)===state){
                this.inactiveMembers.add(node);
                if(state)this.states.set(node,{...state,role:0,leader:0n});
              }
              if(state&&(this.routeTerm??0n)<=state.term){this.routeHint=hint;this.routeTerm=state.term;}
            }
          }
        }catch{this.asleep=false;}
      }
      await this.flushSeen(now);
      if(!this.asleep&&this.elapsed-this.lastReadAt<this.policy.idle_after/2){
        try{await this.confirmRead(await this.barrier(undefined,undefined,true));}catch{this.invalidateRead();}
      }
    }finally{
      if(!this.asleep)await this.armAlarm(now+(this.forming&&this.formationSoon?1:this.policy.tick_every));
      if(this.holds.next!==null)await this.armAlarm(this.holds.next);
    }
  }
  async formNext(){
    const leader=this.leader();if(!leader)return;
    const b=await this.barrier(undefined,undefined,true),header=await this.members.get(b.node).hostView(b.index,'membership','registry');
    if(header.raw){this.forming=false;await this.peers.refresh();return false;}
    const imported=await this.ctx.storage.get('raft-formation-imported')??[...this.birth.voters],conf=this.configuration(header.app.conf);
    const configured=new Set([...conf.voters,...(conf.outgoing??[]),...(conf.learners??[])]);
    for(const id of this.manifest.config.anchors){
      if(this.members.has(id)||!configured.has(id)&&!imported.includes(id))continue;
      const member=this.member(id),receipt=await member.importReceipt(this.manifest,this.birth);
      if(!receipt){
        if(imported.includes(id)||!configured.has(id))continue;
        for(const row of this.source.rows())await member.legacyImport(this.config(id),this.manifest,row);
        await member.legacyImport(this.config(id),this.manifest,null,true);
      }
      if(!imported.includes(id)){imported.push(id);await this.ctx.storage.put('raft-formation-imported',imported);await this.ctx.storage.sync();}
      const initialized=await member.initialize(this.config(id));if(initialized.capacity)throw unavailable();
      this.members.set(id,member);this.accept(id,initialized);this.accept(id,await member.drainRecovery());await this.input(leader,1);await this.pump();return true;
    }
    const result=await this.members.get(leader).formation();this.accept(leader,result);await this.pump();
    if(result.settle){
      const anchors=this.config(this.manifest.config.anchors[0]).anchors;
      requireOK((await this.anchorCommand({init_membership:{anchors,limit:this.policy.max_member_records}},Date.now())).value.code);
      this.forming=false;await this.peers.refresh();
    }
    return !!result.progressed;
  }
  async prepareCoordination(){
    if(!this.forming)return;
    try{
      for(let turn=0;turn<16&&this.forming;turn++)await this.formNext();
      if(this.forming)throw unavailable();
    }catch{throw unavailable();}
  }
  async personal(action,{proof,actor='',certificate=''}={}){
    const b=await this.barrier();
    return this.members.get(b.node).hostPersonal(b.index,{action,identity:this.manifest.config.identity,epoch:this.manifest.config.generation,generation:this.peers.generation,leader:this.leaderID(),actor,certificate,
      proof:proof??{method:'',uri:'',identity_key:null,certificate:'',at:'',signature:null,body:null},progress:[...this.peers.progress].map(([node,applied])=>({node,applied}))});
  }

  async reconcileMembership(){
    if(!this.membershipPending)return;
    for(let stage=0;stage<3;stage++){
      const raw=await this.barrierRecord({namespace:'membership',key:'registry'}),h=decodeCanonical(dec.decode(raw));
      if(!h.pending){this.membershipPending=false;await this.peers.refresh();return;}
      const id=this.leader();let plan;
      if(id){plan=await this.members.get(id).membership();this.accept(id,plan);await this.pump();}
      else {const reply=await this.remote({action:'membership'});if(!reply.membership)throw unavailable();plan={change:reply.membership.Change,settle:reply.membership.Settle};if(plan.change)continue;}
      if(plan.settle){requireOK((await this.anchorCommand({settle_membership:{revision:h.revision,node:h.pending.node}},Date.now())).value.code);}
    }
    const h=decodeCanonical(dec.decode(await this.barrierRecord({namespace:'membership',key:'registry'})));this.membershipPending=!!h.pending;
    await this.peers.refresh();
  }
  async enroll(proof){
    await this.prepareCoordination();
    await this.reconcileMembership();
    for(let stage=0;stage<3;stage++){
      const out=await this.personal('admission',{proof});
      if(out.code!=='ok')return this.personalRefusal(out);
      if(!out.plan.change){const grant=await this.personal('grant',{proof});return grant.code==='ok'?this.jsonBytes(unb64(grant.wire)):this.personalRefusal(grant);}
      const result=await this.anchorCommand(out.plan.change,Date.now());
      if(result.value.code!=='ok'){
        return this.personalRefusal({code:result.value.code,message:undefined});
      }
      this.membershipPending=true;
      // Registry admission and configuration are separate durable stages. A
      // subsequent activation or maintenance owns completion after a lost reply.
      if(out.plan.change.enroll_member){const grant=await this.personal('grant',{proof});return this.jsonBytes(unb64(grant.wire));}
      await this.reconcileMembership();
    }
    throw unavailable();
  }
  personalRefusal(out){const r=response(this.runtime.call(frame(38).u32(2).str(out.code).finish())),message=r.str();r.end();return new Response(out.message||message||'this computer is not allowed to join this cluster',{status:out.code==='capacity'?429:out.code==='update_required'?409:403,headers:{'cache-control':'no-store'}});}
  async reconcileLeadership(){
    const raw=await this.barrierRecord({namespace:'membership',key:'registry'}),h=decodeCanonical(dec.decode(raw)),l=h.leadership,id=this.leader();
    if(!l||!id){if(!l)this.transferTarget=null;return false;}
    if(l.restore||l.completed||this.transferTarget?.revision!==h.revision)this.transferTarget=null;
    const empty={node:'0',revision:'0',applied:'0',protocol:0,durable:false};
    try{
      let result;
      try{result=await this.members.get(id).leadership(empty);}
      catch(error){
        if(!/target|transfer/.test(error.message))throw error;
        const target=l.restore?l.standby:l.node;let evidence;
        if(this.members.has(target))evidence=await this.members.get(target).leadershipEvidence(h.revision,l.barrier);
        else {try{evidence=(await this.forward(target,{action:'evidence',revision:h.revision,barrier:l.barrier})).evidence??empty;}catch{const request=crypto.randomUUID();await this.input(id,15,request);await this.input(id,10,request);await this.pump();return false;}}
        if(this.leader()!==id)return true;
        result=await this.members.get(id).leadership(evidence);
      }
      const plan=result.plan;
      if(plan.transfer!=='0')this.transferTarget={node:plan.transfer,revision:h.revision};
      this.accept(id,result);await this.pump();
      if(plan.fallback){this.transferTarget=null;requireOK((await this.anchorCommand({restore_anchors:{revision:h.revision}},Date.now())).value.code);return true;}
      if(plan.configure){await this.peers.refresh();return true;}
      if(plan.settle){requireOK((await this.anchorCommand({settle_leadership:{revision:h.revision}},Date.now())).value.code);return true;}
      return false;
    }catch(error){
      if(/requires the elected leader/.test(error.message)){await this.input(id,0);await this.pump();if(this.leader()!==id)return true;}
      if(/target|transfer|busy|barrier not applied/.test(error.message))return false;throw error;
    }
  }
  async control(method,actor,certificate,proof,names=false){
    await this.prepareCoordination();
    await this.reconcileMembership();const out=await this.personal(method==='POST'?'lead':'status',{actor,certificate,proof});
    if(out.code!=='ok')return new Response('this computer is not allowed to coordinate this cluster',{status:out.code==='bad_request'?400:403});
    if(method!=='POST'){
      // Older clients decode status strictly, so member labels require an explicit opt-in.
      if(names)return this.jsonBytes(unb64(out.wire));
      const status=decodeCanonical(dec.decode(unb64(out.wire)));delete status.members;
      return this.jsonBytes(encodeCanonical(status));
    }
    const command=decodeCanonical(dec.decode(unb64(out.wire))),result=await this.commit(unb64(out.wire));
    const refusal=code=>new Response(encodeCanonical({code,sequence:command.sequence}),{status:409,headers:{'content-type':'application/json'}});
    if(result.value.code!=='ok')return refusal(result.value.code);
    const deadline=Date.now()+this.peers.limits.ForwardTimeout;
    while(Date.now()<deadline){
      const h=decodeCanonical(dec.decode(await this.barrierRecord({namespace:'membership',key:'registry'}))),l=h.leadership;
      if(!l||l.restore||l.node!==command.change.request_leadership.node)return refusal('conflict');
      if(this.leaderID()===l.node&&!this.leader()&&l.settled){
        if(!l.completed){const reply=await this.forward(l.node,{action:'receipt'});if(!reply.receipt)throw unavailable();await this.appliedReplica(reply.barrier,deadline);requireOK((await this.anchorCommand({complete_leadership:{revision:h.revision}},Date.now())).value.code);}
        const reply=await this.forward(l.node,{action:'receipt'});if(!reply.receipt||reply.receipt.node!==l.node||reply.receipt.revision!==h.revision)throw unavailable();
        await this.appliedReplica(reply.barrier,deadline);this.transferTarget=null;return this.jsonBytes(encodeCanonical(reply.receipt));
      }
      const version=this.progressVersion;if(!await this.reconcileLeadership())await this.progress(deadline,version);
    }
    throw unavailable();
  }
  async api(raw,q,actor) {
    await this.ensure(unb64(q.identity_key));
    await this.prepareCoordination();
    await this.wakeMaintenance();
    if(q.cluster!==this.manifest.config.cluster||q.identity_key!==this.manifest.config.signing_key)throw new Wire('unauthorized',401);
    // Native commands carry a caller-signed time. Commit the server's clock
    // witness using the device_seen shape older native evaluators accept so
    // backdated enrollment cannot revive expired scope.
    requireOK((await this.commitEffect({device_seen:{device:actor}},this.readNow())).code);
    const b=await this.barrier(),member=this.members.get(b.node);
    const root=await member.hostView(b.index,'state','identity');
    clusterIngress(this.runtime,raw,root.app.now);
    if(q.action==='submit'){
      const result=await this.commit(data(q.command));
      if(result.value.code==='ok'){
        const b=await this.barrier(),row=await this.members.get(b.node).hostView(b.index,'state','identity');
        const identity=this.project('identity',row.raw);await this.rememberRetirement(identity);
        if(identity.rotation)this.watchers.closeAll(CLOSE_ROTATED,'rotated');
        if(this.retireAt)await this.armAlarm(this.retireAt);
        if(q.command.change.revoke){
          const device=q.command.change.revoke.device;
          this.tenant.fabric.revokeClient(device);
          this.watchers.announce({t:'revoked',device},device);this.watchers.closeDevice(device,CLOSE_REVOKED,'revoked');
        }
      }
      return this.jsonBytes(encodeCanonical({result:result.value}));
    }
    const selected=decodeCanonical(dec.decode(await member.hostSelected(raw,b.index)));
    requireOK(selected.code);
    const out={};for(const key of ['view','grant','assignment'])if(selected[key]!==undefined)out[key]=selected[key];
    // Current-assignment matches the native door, which exposes the selected
    // grant through the cell door alone.
    if(q.action==='assignment')delete out.grant;
    return this.jsonBytes(encodeCanonical(out));
  }
  waitIO(device,write){
    const waits=this.pendingIO.get(device)??new Set(),entry={write,error:null};
    entry.stopped=new Promise((_,reject)=>{entry.reject=reject;});entry.stopped.catch(()=>{});
    waits.add(entry);this.pendingIO.set(device,waits);
    return {entry,stopped:entry.stopped,close:()=>{waits.delete(entry);if(!waits.size)this.pendingIO.delete(device);}};
  }
  stopIO(device,code){
    for(const entry of this.pendingIO.get(device)??[]){
      if(code==='rotated'&&!entry.write)continue;
      entry.error=new Wire(code,code==='revoked'?401:410);entry.reject(entry.error);
    }
  }
  // Handoff uses the same committed rotation view as GET, but its binding and
  // approval belong only to this hosted object's meta, never to Raft commands.
  async handoff(c,fresh){
    await this.ensure(fromBase64Url(c.request.headers.get('codeaf-identity')));
    const authority=await this.authorize(c.device,c.request.headers.get('codeaf-cert'),false,fresh);
    checkTime(c.request.headers.get('codeaf-time'),this.readNow(authority.app.now));
    const rotation=authority.identity.rotation;
    if(!['frozen','retired'].includes(rotation?.state)||rotation.by!==c.device)throw new Wire('not_rotating',409);
    const successor=await checkStorageHandoff(this.tenant.identity,c.body);
    await this.finishRead(authority.barrier,null);
    this.tenant.storage.bindSuccessor(successor);
    await this.ctx.storage.sync();
    const approval=this.tenant.limits.storageOpen?null:this.tenant.storage.approval();
    const storage=unwrap(await this.env.IDENTITY.get(this.env.IDENTITY.idFromName(successor)).inheritStorage(approval?.storage==='on'?approval:null));
    return json({successor,storage});
  }
  async serveFabric(c,route,fresh=false){
    const {request,device,body}=c,cert=request.headers.get('codeaf-cert'),targetDevice=route.args[0];
    await this.ensure(fromBase64Url(request.headers.get('codeaf-identity')));
    await this.wakeMaintenance();
    const authority=await this.authorize(device,cert,route.write,fresh,true),b=authority.barrier;
    const now=this.readNow(authority.app.now);
    checkTime(request.headers.get('codeaf-time'),now);
    const ids=new Set([device,targetDevice]);
    if(request.method==='POST'&&body.length){
      try{const parsed=JSON.parse(dec.decode(body));if(typeof parsed?.target==='string'&&parsed.target&&parsed.target!=='any')ids.add(parsed.target);}catch{}
    }
    const devices={};
    if(authority.actor)devices[device]=authority.actor;
    for(const id of ids){
      if(id===device||!id)continue;
      const row=await this.readView(b,'state','device/'+segment(id));
      if(row.raw)devices[id]=this.project('device',row.raw);
    }
    if(devices[targetDevice]?.revoked)this.tenant.fabric.revokeClient(targetDevice);
    const dir={read:(kind,id)=>(kind==='devices'?devices[id]:undefined)};
    const reply=await this.tenant.fabric.withClockAndDir(now,dir,()=>route.handler(c,route.args));
    return this.finishRead(b,reply);
  }
  async dispatch(c,route){
    const path=new URL(c.request.url).pathname;
    if(path==='/v1/store/handoff')return this.operation(()=>this.retryRead(fresh=>this.handoff(c,fresh)));
    if(route.fabric&&!(c.request.method==='PUT'&&/^\/v1\/clients\/[^/]+$/.test(path)))return this.operation(()=>this.retryRead(fresh=>this.serveFabric(c,route,fresh)));
    if(!path.startsWith('/v1/store/'))return this.operation(()=>c.request.method==='GET'?this.retryRead(fresh=>this.serve(c,route,fresh)):this.serve(c,route));
    const cert=c.request.headers.get('codeaf-cert');
    const ticket=await this.operation(()=>this.retryRead(async fresh=>{
      await this.ensure(fromBase64Url(c.request.headers.get('codeaf-identity')));
      const authority=await this.authorize(c.device,cert,route.write,fresh);
      return {authority,io:this.waitIO(c.device,route.write)};
    }));
    // Blob waits cannot hold the coordination queue: revoke/rotation must be
    // able to commit during I/O, and Has must not block queued frame puts.
    try{
      if(ticket.io.entry.error)throw ticket.io.entry.error;
      const work=Promise.resolve().then(()=>route.handler(c,route.args));
      c.retainPut?.(work);
      const result=await Promise.race([work,ticket.io.stopped]);await this.ctx.storage.sync();
      await this.operation(()=>this.retryRead(fresh=>this.recheckAuthority(ticket.authority,c.device,cert,route.write,fresh)));return result;
    }finally{ticket.io.close();}
  }
  async serve(c,route,fresh=false){
    const {request,device,body}=c,url=new URL(request.url),path=url.pathname,cert=request.headers.get('codeaf-cert');
    await this.ensure(fromBase64Url(request.headers.get('codeaf-identity')));
    await this.wakeMaintenance();
    if(path==='/v1/dir/watch'){
      if(request.headers.get('upgrade')?.toLowerCase()!=='websocket')throw new Wire('upgrade_required',426);
      const holds=parseHolds(url.searchParams.getAll('hold'),this.tenant.limits.maxHolds),beat=parseBeat(url.searchParams.getAll('beat'));
      const a=await this.authorize(device,cert,true,fresh);
      await this.finishRead(a.barrier,null);
      const reply=this.watchers.accept(device,Number(a.app.version),Date.now(),{holds,beat,events:url.searchParams.get('events')==='1',receipts:url.searchParams.get('receipts')==='1',certificate:cert});
      try{
        this.holds.opened(Date.now(),holds.length>0);
        if(holds.length)await this.holds.renew(Date.now(),this.watchers.holdEvidence(Date.now()));
        if(this.holds.next!==null)await this.armAlarm(this.holds.next);
        await this.retryRead(fresh=>this.recheckAuthority(a,device,cert,true,fresh));return reply;
      }catch(error){
        const code=error.code==='revoked'?CLOSE_REVOKED:error.code==='rotated'?CLOSE_ROTATED:1012;
        this.watchers.closeDevice(device,code,error.code??'reconnecting');
        throw error.readInterrupted?unavailable():error;
      }
    }
    if(request.method==='GET'){
      const authority=await this.authorize(device,cert,false,fresh),b=authority.barrier;
      const now=this.readNow(authority.app.now),app={...authority.app,now:String(now)};
      checkTime(request.headers.get('codeaf-time'),now);
      if(path==='/v1/identity/rotation')return this.finishRead(b,json(rotationView(authority.identity.rotation,now,this.tenant.limits)));
      if(path==='/v1/dir/list'||path==='/v1/dir/presence'){
        const raw=await this.listingJSON(b,app),reply=path.endsWith('/presence')?json(presenceOf(JSON.parse(raw).devices,this.watchers.online(),now)):this.jsonBytes(raw);
        reply.headers.set('Codeaf-Dir-Version',app.version);return this.finishRead(b,reply);
      }
      const row=await this.readView(b,'state','cell/'+segment(decodeURIComponent(route.args[0])));if(!row.raw)throw new Wire('not_found',404);
      const reply=this.jsonBytes('{"now":'+now+',"cell":'+this.projectJSON('cell',row.raw)+'}');reply.headers.set('codeaf-conditional-takes','1');return this.finishRead(b,reply);
    }
    const join=/^\/v1\/dir\/requests\/([^/]+)\/(approve|deny)$/.exec(path);
    let joinCode,joinBox;
    if(join){
      // Link decisions need fresh quorum authorization because dispatch does not retry them.
      const authority=await this.authorize(device,cert,true,true);joinCode=decodeURIComponent(join[1]).trim().toLowerCase();joinBox=this.env.LINK_REQUEST.get(this.env.LINK_REQUEST.idFromName(joinCode));
      // Invalid approvals cannot reserve a request for this cluster. The Go
      // command still checks authority and revocation at its committed index.
      const pending=unwrap(await joinBox.read(0));
      if(join[2]==='approve'){
        const approval=JSON.parse(dec.decode(body));await checkApprovalDevice(c,approval,pending);
        const target=await this.readView(authority.barrier,'state','device/'+segment(pending.device));
        if(target.raw&&this.project('device',target.raw).revoked)throw new Wire('bad_request',400);
        asGrant(approval.grant,this.tenant.limits.linkMaxGrant);
      }
    }
    if(request.method==='PUT'&&/^\/v1\/clients\/[^/]+$/.test(path)){
      await this.authorize(device,cert,true,true);
      await this.tenant.fabric.validateRegisterClient({targetDevice:decodeURIComponent(route.args[0]),body,identityHeader:request.headers.get('codeaf-identity')});
    }
    let raw;try{raw=v1Command(this.runtime,this.manifest.config.cluster,this.readNow(),this.proof(request,body));}catch{throw new Wire('bad_request',400);}
    if(join)await this.effects.admit(joinBox,joinCode,Date.now());
    const result=await this.commitV1(raw);
    const b=await this.appliedReplica(result.index),snapshot=await this.members.get(b.node).hostView(b.index,'state','identity'),identity=this.project('identity',snapshot.raw);
    await this.rememberRetirement(identity);
    if(join){const code=decodeURIComponent(join[1]).trim().toLowerCase();await this.effects.deliver(this.env.LINK_REQUEST.get(this.env.LINK_REQUEST.idFromName(code)),code,Date.now());unwrap(await this.env.LINK_GATE.get(this.env.LINK_GATE.idFromName('gate')).decided(code));}
    if(path==='/v1/identity/rotation'){
      this.watchers.closeAll(CLOSE_ROTATED,'rotated');if(identity.rotation?.retire_at)await this.ctx.storage.setAlarm(identity.rotation.retire_at);
      return json(rotationView(identity.rotation,Number(snapshot.app.now),this.tenant.limits));
    }
    if(path.endsWith('/revoke')){
      const device=decodeURIComponent(route.args[0]);
      this.tenant.fabric.revokeClient(device);
      this.watchers.announce({t:'revoked',device},device);this.watchers.closeDevice(device,CLOSE_REVOKED,'revoked');
    }
    if(path.endsWith('/release')||path.endsWith('/archive'))return empty();
    return result.value.view?this.jsonBytes(this.projectJSON('view',result.raw)):empty();
  }
}
