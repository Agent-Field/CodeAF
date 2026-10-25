import {acceptHibernatingSocket} from '../watch.js';
import {frame,response} from './abi.js';
import {encodeCanonical,decodeCanonical} from './codec.js';
import {b64,unb64} from './storage.js';

const enc=new TextEncoder(),dec=new TextDecoder('utf-8',{fatal:true});
const data=v=>enc.encode(encodeCanonical(v));
const refused=code=>Object.assign(new Error('member authorization refused'),{peerCode:code});
export const peerPath=path=>['/v2/cluster/exchange','/v2/cluster/socket','/v2/cluster/forward'].includes(path);
export function peerLimits(runtime){const r=response(runtime.call(frame(38).u32(0).finish()));const limits=JSON.parse(dec.decode(r.blob()));r.end();return limits;}
export const peerSocket=ws=>ws.deserializeAttachment()?.clusterPeer===true;

// Retention belongs to the socket attachment, so an alarm can expire lost
// delivery without activating coordination or replacing an idle generation.
export async function expirePeerSockets(ctx,now,arm){
 for(const ws of ctx.getWebSockets('cluster-peer')){
  const at=ws.deserializeAttachment()?.deadline;
  if(at&&at<=now){try{ws.close(1012,'reconnecting');}catch{}}
  else if(at)await arm(at);
 }
}

// Routing memory is expendable. A durable generation fences all old cursors
// before a recovered coordinator can serve another exchange or socket frame.
export class PersonalPeers {
 constructor(host,generation){
  this.host=host;this.generation=generation;this.scope=host.config('1').scope;this.view={};this.boxes=new Map();this.used=[0,0,0];this.progress=new Map();this.snapshots=new Map();this.acks=new Map();this.tail=Promise.resolve();this.pending=0;this.blocked=new Set();this.checkpoints=new Map();
  this.limits=peerLimits(host.runtime);
 }
 serial(fn){if(this.pending>=64)return Promise.reject(refused('busy'));this.pending++;const p=this.tail.then(fn);this.tail=p.catch(()=>{});return p.finally(()=>this.pending--);}
 members(){return Object.entries(this.view).map(([node,p])=>({node,...p}));}
 async refresh(){
  const id=this.host.leader();if(!id)return;
  const applied=String(this.host.states.get(id).applied);if(this.viewApplied===id+'/'+applied)return;
  const view=await this.host.members.get(id).refreshEnrollment();
  this.viewApplied=id+'/'+applied;
  if(JSON.stringify(view)===JSON.stringify(this.view))return;
  this.view=view;this.used=[0,0,0];
  for(const [node,p]of Object.entries(view)){
   const old=this.boxes.get(node);
   this.boxes.set(node,{queues:[[],[],[]],next:old?.next??[0n,0n,0n],issued:old?.issued??[0n,0n,0n],acked:old?.acked??[0n,0n,0n],credits:old?.credits??new Map()});
   if(p.revoked)this.progress.delete(node);
  }
  for(const [to,s]of this.snapshots){this.snapshots.delete(to);await this.host.input(s.from,12,{peer:to,snapshot:true,success:false});}
  for(const ws of this.sockets())if(this.view[ws.deserializeAttachment().node]?.revoked)this.close(ws);
 }
 trusted(){const s=this.host.states.get(this.host.leader());return {scope:this.scope,generation:this.generation,members:this.members(),applied:String(s?.applied??0),term:String(s?.term??0),sleeping:this.host.asleep};}
 validate(raw){
  const r=response(this.host.runtime.call(frame(37).blob(raw).blob(data(this.trusted())).finish()));const result=JSON.parse(dec.decode(r.blob()));r.end();
  if(result.code!=='ok')throw refused(result.code);
  return {request:JSON.parse(dec.decode(raw)),obsolete:new Set(result.obsolete)};
 }
 sockets(){return this.host.ctx.getWebSockets('cluster-peer');}
 close(ws){try{ws.close(1012,'reconnecting');}catch{}}
 expire(){const now=Date.now();for(const box of this.boxes.values())for(let lane=0;lane<3;lane++)box.queues[lane]=box.queues[lane].filter(q=>{if(q.expires>now)return true;this.used[lane]-=q.size;return false;});}
 ack(req){
  const box=this.boxes.get(req.node);if(!box)throw refused('unauthorized');
  for(let lane=0;lane<3;lane++)if(BigInt(req.ack[lane])>box.issued[lane])throw refused('cursor');
  for(let lane=0;lane<3;lane++){
   const ack=BigInt(req.ack[lane]);if(ack<=box.acked[lane])continue;
   box.queues[lane]=box.queues[lane].filter(q=>{
    if(BigInt(q.cursor)>ack)return true;this.used[lane]-=q.size;
    if(q.envelope.chunk){const source=this.boxes.get(q.envelope.from);if(source)source.credits.set(req.node,q.envelope.sequence);}
    return false;
   });box.acked[lane]=ack;
  }
 }
 lane(e){return e.chunk?2:this.view[e.from]?.voter&&this.view[e.to]?.voter?0:1;}
 enqueue(envelopes){
  this.expire();const staged=[],added=[0,0,0],counts=new Map();
  for(const e of envelopes){
   const box=this.boxes.get(e.to);if(!box||this.view[e.to]?.revoked)throw refused('unauthorized');
   const duplicate=[...box.queues.flat(),...staged].find(q=>q.envelope.to===e.to&&q.envelope.from===e.from&&q.envelope.sequence===e.sequence);
   if(duplicate){if(JSON.stringify(duplicate.envelope)!==JSON.stringify(e))throw refused('unauthorized');continue;}
   const lane=this.lane(e),size=enc.encode(JSON.stringify(e)).length,k=e.to+'/'+lane;
   counts.set(k,(counts.get(k)??0)+1);added[lane]+=size;
   if(box.queues[lane].length+counts.get(k)>this.limits.Slots[lane]||this.used[lane]+added[lane]>this.limits.Budget[lane]||box.next[lane]+BigInt(counts.get(k))>=(1n<<64n))throw refused('busy');
   staged.push({envelope:e,lane,size,expires:Date.now()+this.limits.Retention});
  }
  for(const q of staged){const box=this.boxes.get(q.envelope.to);q.cursor=String(++box.next[q.lane]);box.queues[q.lane].push(q);this.used[q.lane]+=q.size;}
 }
 deliveries(node){
  this.expire();const box=this.boxes.get(node),messages=[];let normal=0,snapshot=false,size=0;
  if(!box)return messages;
  for(let lane=0;lane<3;lane++)for(const q of box.queues[lane]){
   const bytes=unb64(q.envelope.payload).length;
   if(q.envelope.chunk){if(snapshot||normal)break;snapshot=true;}else{if(snapshot||normal+bytes>this.limits.Batch)break;normal+=bytes;}
   const d={lane,cursor:q.cursor,envelope:q.envelope},cost=enc.encode(JSON.stringify(d)).length;
   if(size+cost>this.limits.Exchange-1024)break;size+=cost;messages.push(d);box.issued[lane]=BigInt(q.cursor)>box.issued[lane]?BigInt(q.cursor):box.issued[lane];
  }
  return messages;
 }
 wire(node,extra={}){
  const box=this.boxes.get(node),chunks=[...(box?.credits??[])].sort(([a],[b])=>BigInt(a)<BigInt(b)?-1:1).map(([node,sequence])=>({node,sequence}));
  const r=response(this.host.runtime.call(frame(38).u32(1).blob(enc.encode(JSON.stringify({...extra,chunks,generation:this.generation,messages:this.deliveries(node)}))).finish()));const raw=r.blob();r.end();return raw;
 }
 async recover(){
  const from=this.host.leader();
  for(const [to,member]of Object.entries(this.view))if(!member.revoked&&!this.host.members.has(to)){
   await this.host.input(from,12,{peer:to,snapshot:true,success:false});
   await this.host.input(from,12,{peer:to,snapshot:false,success:false});
  }
  await this.host.pump();
 }
 async send(m){
  await this.refresh();
  if(this.host.asleep||this.blocked.has(m.to)||!this.view[m.to]||this.view[m.to].revoked)return;
  const inspected=this.host.codec.inspect(4,m.bytes),snapshot=inspected.snapshot;
  if(m.bytes.length>this.limits.Batch){
   this.blocked.add(m.to);const applied=String(this.host.states.get(m.from).applied);
   if(this.checkpoints.get(m.from)!==applied){await this.host.members.get(m.from).hostCheckpoint();this.checkpoints.set(m.from,applied);}
   await this.host.input(m.from,12,{peer:m.to,snapshot:false,success:false});return;
  }
  const lane=this.view[m.from]?.voter&&this.view[m.to]?.voter?0:1;
  if(this.boxes.get(m.to).queues[lane].length>=this.limits.Slots[lane]){this.blocked.add(m.to);await this.host.input(m.from,12,{peer:m.to,snapshot:snapshot.length>0,success:false});return;}
  try{
   this.enqueue([JSON.parse(dec.decode(await this.host.envelope(m.from,m.to,m.bytes)))]);
   if(snapshot.length){const meta=this.host.codec.inspect(3,snapshot),manifest=JSON.parse(dec.decode(meta.data));this.snapshots.set(m.to,{from:m.from,manifest,generation:this.host.hash(meta.data),offset:0,waiting:null,started:Date.now()});}
  }catch(e){if(e.peerCode!=='busy')throw e;this.blocked.add(m.to);await this.host.input(m.from,12,{peer:m.to,snapshot:snapshot.length>0,success:false});}
 }
 async chunks(){
  if(this.host.asleep)return;
  for(const [to,s]of this.snapshots){
   if(BigInt(this.progress.get(to)??0)>=BigInt(s.manifest.index)){
    this.snapshots.delete(to);await this.host.input(s.from,12,{peer:to,snapshot:true,success:true});continue;
   }
   if(Date.now()-s.started>=this.limits.Retention){this.snapshots.delete(to);await this.host.input(s.from,12,{peer:to,snapshot:true,success:false});continue;}
   if(s.waiting&&this.boxes.get(s.from)?.credits.get(to)!==s.waiting)continue;
   s.waiting=null;if(s.offset>=Number(s.manifest.chunks))continue;
   const chunk=await this.host.members.get(s.from).hostSnapshot('read',s.generation,s.offset);
   const e=JSON.parse(dec.decode(await this.host.envelope(s.from,to,chunk,{generation:s.generation,offset:String(s.offset),hash:this.host.hash(chunk)})));
   try{this.enqueue([e]);s.offset++;s.waiting=e.sequence;}catch(err){if(err.peerCode!=='busy')throw err;}
  }
 }
 async anchors(){
  for(let round=0;round<this.limits.Inflight;round++){
   let work=false;
   for(const [id,member]of this.host.members){
    for(const d of this.deliveries(id)){
     const e=d.envelope,raw=enc.encode(JSON.stringify(e));
     const result=await member.peer(raw);this.host.accept(id,result);
     const ack=this.acks.get(id)??['0','0','0'];ack[d.lane]=d.cursor;this.acks.set(id,ack);work=true;
    }
    this.ack({node:id,ack:this.acks.get(id)??['0','0','0']});
   }
   await this.host.pump();if(!work)break;
  }
 }
 async exchange(raw){return this.serial(async()=>{
  await this.refresh();let {request:req,obsolete}=this.validate(raw);this.ack(req);
  let rejected=false;
  try{this.enqueue((req.messages??[]).filter(e=>!obsolete.has(e.sequence)));}catch(err){if(err.peerCode!=='busy')throw err;rejected=true;}
  if(req.applied)this.progress.set(req.node,String(BigInt(req.applied)>BigInt(this.progress.get(req.node)??0)?req.applied:this.progress.get(req.node)));
  for(const e of req.messages??[])if(!obsolete.has(e.sequence))await this.host.wakeMaintenance();
  await this.anchors();await this.chunks();await this.host.pump();
  await this.push(req.node);
  return {request:req,raw:this.wire(req.node,{[rejected?'rejected':'accepted']:(req.messages??[]).map(e=>e.sequence)}),rejected};
 });}
 async push(except){
  await this.refresh();
  for(const ws of this.sockets()){
   const a=ws.deserializeAttachment();
   if(a.kind!=='peer'||a.node===except)continue;
   try{
    this.validate(unb64(a.auth));const raw=this.wire(a.node),text=this.host.hash(raw);
    if(text===a.last)continue;
    const body=JSON.parse(dec.decode(raw));if(!body.messages.length&&!body.chunks?.length)continue;
    ws.send(raw);const deadline=a.deadline??(body.messages.length?Date.now()+this.limits.Retention:null);ws.serializeAttachment({...a,last:text,deadline});if(deadline)await this.host.armAlarm(deadline);
   }catch{this.close(ws);}
  }
 }
 async accept(request,raw,kind){
  await this.refresh();const checked=this.validate(raw);if(checked.request.messages?.length)throw refused('unauthorized');
  const {request:req}=await this.exchange(raw);
  if(this.sockets().filter(ws=>ws.deserializeAttachment().node===req.node).length>=this.limits.Inflight)throw refused('busy');
  if(request.headers.get('upgrade')?.toLowerCase()!=='websocket')return new Response('upgrade required',{status:426});
  const initial=kind==='peer'?this.wire(req.node):null;
  const deadline=initial&&JSON.parse(dec.decode(initial)).messages.length?Date.now()+this.limits.Retention:null;
  const {client,server}=acceptHibernatingSocket(this.host.ctx,['cluster-peer'],{clusterPeer:true,kind,node:req.node,generation:this.generation,auth:b64(raw),last:initial?this.host.hash(initial):null,deadline});
  if(deadline)await this.host.armAlarm(deadline);
  if(initial)server.send(initial);
  return new Response(null,{status:101,webSocket:client});
 }
 async frame(ws,raw){
  const att=ws.deserializeAttachment();
  try{
   this.validate(unb64(att.auth));if(att.kind!=='peer'||!(raw instanceof ArrayBuffer))throw refused('invalid');
   const bytes=new Uint8Array(raw),next=JSON.parse(dec.decode(bytes));if(next.node!==att.node||next.generation!==att.generation)throw refused('unauthorized');
   const result=await this.exchange(bytes);ws.send(result.raw);const deadline=JSON.parse(dec.decode(result.raw)).messages.length?Date.now()+this.limits.Retention:null;ws.serializeAttachment({...att,last:this.host.hash(result.raw),deadline});if(deadline)await this.host.armAlarm(deadline);
  }catch{this.close(ws);}
 }
 async quiesce(){
  // Final voter acknowledgments discard learner delivery. Its old retention
  // deadline must not wake a cold router merely to reconnect idle sockets.
  for(const ws of this.sockets()){const a=ws.deserializeAttachment();if(a.deadline)ws.serializeAttachment({...a,deadline:null});}
  for(const [node,box]of this.boxes)if(!this.view[node]?.voter)for(let lane=0;lane<3;lane++){for(const q of box.queues[lane])this.used[lane]-=q.size;box.queues[lane]=[];}
  for(const [to,s]of this.snapshots){this.snapshots.delete(to);await this.host.input(s.from,12,{peer:to,snapshot:true,success:false});}
 }
}
export function peerRefusal(e){const code=e.peerCode??'invalid',status={unauthorized:403,generation:409,cursor:409,busy:429}[code]??400;return new Response(JSON.stringify({error:code}),{status,headers:{'content-type':'application/json','cache-control':'no-store',...(code==='busy'?{'retry-after':'1'}:{})}});}
