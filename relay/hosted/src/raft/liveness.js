import { frame, response } from './abi.js';
import { decodeCanonical } from './codec.js';

// Policy numbers come from the same Go constants as native scheduling/apply.
export function coordinationPolicy(runtime) {
  const r = response(runtime.call(frame(25).finish()));
  const policy = decodeCanonical(new TextDecoder().decode(r.blob())); r.end();
  policy.covered=(raw,device,hold,seen)=>{
    const q=response(runtime.call(frame(26).blob(raw??new Uint8Array()).str(device).str(hold.cell).u64(hold.fence).u64(seen).finish()));
    const covered=q.u32()!==0;q.end();return covered;
  };
  return policy;
}

// The bridge is installed only beside the consensus facade. Watch evidence is
// ingress-authenticated; commitEffect returns durable application results. A
// socket auto-response alone never changes the directory or its visible version.
export class CommittedHolds {
  constructor(host, policy) { this.host = host; this.policy = policy; this.next = null; this.running = null; this.page = null; }
  opened(now, hasHolds) { if (hasHolds && this.next === null) this.next = now; return this.next; }
  renew(now, sockets) {
    if (this.running) return this.running;
    this.running = this.run(now, sockets).finally(() => { this.running = null; }); return this.running;
  }
  async run(now, sockets) {
    if (this.next !== null && now < this.next) return this.next;
    if (!this.page) {
      // A cycle retains at most the admitted sockets' bounded routing tuples.
      // Copy before any await so a replaced socket cannot rewrite old evidence.
      this.page = { sockets:sockets.filter(s=>s.holds.length).map(s=>({...s,holds:structuredClone(s.holds)})), socket:0, offset:0 };
    }
    const page=this.page; let remaining=this.policy.max_renew_holds;
    while(page.socket<page.sockets.length && remaining>0) {
      const socket=page.sockets[page.socket];
      if(socket.seenAt<=0 || socket.seenAt>now || now-socket.seenAt>this.policy.evidence_window) {page.socket++;page.offset=0;continue;}
      const chunk=socket.holds.slice(page.offset,page.offset+remaining);
      page.offset+=chunk.length;remaining-=chunk.length;
      if(page.offset===socket.holds.length){page.socket++;page.offset=0;}
      const items=[];
      try { for(const h of chunk) {
        const key=Array.from(new TextEncoder().encode(h.cell),b=>b.toString(16).padStart(2,'0')).join('');
        const raw=await this.host.barrierRecord({namespace:'state',key:'cell/'+key});
        if(!this.policy.covered(raw,socket.device,h,socket.seenAt))items.push({cell:h.cell,fence:String(h.fence)});
      } } catch {socket.failed?.();continue;}
      if(!items.length)continue;
      let result;
      try { result=await this.host.commitEffect({renew_holds:{device:socket.device,seen_at:socket.seenAt,items,...(socket.certificate?{certificate:socket.certificate}:{})}},now); }
      catch {socket.failed?.();continue;}
      if(result.code!=='ok'||!result.index||result.index==='0'){socket.failed?.();continue;}
      const holds=(result.items??[]).filter(item=>item.code==='ok'&&item.view).map(item=>({cell:item.cell,fence:item.view.cell.lease.fence,index:result.index,at:result.now,until:item.view.cell.lease.expires}));
      if(holds.length)socket.send(JSON.stringify({holds}));
    }
    if(page.socket<page.sockets.length) { this.next=now; }
    else { this.next=page.sockets.length ? now+this.policy.renew_every : null;this.page=null; }
    return this.next;
  }
}

// One local timer handles member activity. Expired deadlines produce one turn,
// never hours of synthetic ticks. The router consumes effects before rearming;
// a rejected dispatch is backpressure, never a successful append acknowledgment.
export class MemberAlarms {
  constructor(storage, actor, dispatch, activity, ready=async()=>true) { this.storage=storage;this.actor=actor;this.dispatch=dispatch;this.activity=activity;this.ready=ready;this.turn=Promise.resolve(); }
  run(elapsed, ingressNow, acks=[]) {
    const task=this.turn.then(()=>this.actor.serial(async()=>{
      if(!await this.ready())return null;
      const busy=await this.activity();
      const effects=await this.actor.scheduleInQueue(elapsed,ingressNow,busy,acks);
      await this.dispatch(effects);
      if(effects.plan.sleeping) await this.storage.deleteAlarm();
      else await this.storage.setAlarm(ingressNow+effects.plan.after);
      return effects;
    }));
    this.turn=task.catch(()=>{});return task;
  }
}

// The router resumes committed membership/leadership/outbox work before idle
// preparation. Each callback uses the existing serialized member doors; this
// driver has no voter-list editor, lease writer or successful read shortcut.
export class QuiescenceCoordinator {
  constructor(host) { this.host=host;this.tail=Promise.resolve(); }
  run(elapsed,now) {
    const task=this.tail.then(async()=>{
      const busy=await this.host.maintain();
      let effects=await this.host.schedule(elapsed,now,busy);
      await this.host.dispatch(effects);
      if(effects.plan.wake?.length)await this.host.wake(effects.plan.wake);
      if(effects.plan.barrier){
        await this.host.idleBarrier();
        effects=await this.host.schedule(elapsed,now,busy);await this.host.dispatch(effects);
      }
      if(!effects.plan.prepare)return effects.plan;
      const marker=await this.host.prepare();
      const acks=await this.host.appliedAcks(marker);
      await this.host.finish(marker,acks,elapsed,now);
      return effects.plan;
    });
    this.tail=task.catch(()=>{});return task;
  }
}
