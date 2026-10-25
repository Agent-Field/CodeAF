// IdentityDO retains the source backup, blob index and socket ingress. Its
// irreversible migration gate routes coordination to three durable members;
// none of its legacy directory methods are a production writer.
import { DurableObject } from 'cloudflare:workers';
import { LegacyAdmission } from './raft/legacy.js';
import { peerPath,peerSocket,peerRefusal,expirePeerSockets } from './raft/peers.js';
import { HostedIdentity } from './raft/hosted.js';
import { clusterIngress,clusterRequestLimit } from './raft/ingress.js';
import { sharedRuntime } from './raft/actor.js';
import { identityOf } from './verify.js';
import { b64,unb64 } from './raft/storage.js';
import { fromBase64Url } from './codec.js';
import { signedUri } from './base.js';
import { readBody } from './body.js';
import { dropTables, erasePage } from './erasure.js';
import { Flight } from './flight.js';
import { Lazy } from './lazy.js';
import { limitsOf, RateLimit } from './limits.js';
import { matchRoute } from './routes.js';
import { policyOf } from './rules.js';
import { Tenant, HAS_WAIT_MS } from './tenant.js';
import { finishClose, Watchers } from './watch.js';
import { checkCert, checkRequest } from './verify.js';
import { gone, ipOf, rateLimited, respond, unwrap, Wire } from './wire.js';

const SIGNED = { identity: 'codeaf-identity', cert: 'codeaf-cert', time: 'codeaf-time', sig: 'codeaf-sig' };
const signedHeaders = (request) => Object.fromEntries(Object.entries(SIGNED).map(([k, h]) => [k, request.headers.get(h)]));

export class IdentityDO extends DurableObject {
  flight = new Flight();
  #tenant = null;
  #arming = Promise.resolve(); // the tail of the alarm settings made so far, see #arm
  #gone = null; // whether the tombstone is written, read once from storage
  #admitted = new Lazy((ip) => this.#admitNewcomer(ip)); // single-flight: a burst of first requests is one admission

  constructor(ctx, env) {
    super(ctx, env);
    this.legacyAdmission = new LegacyAdmission(ctx.storage);
    this.limits = limitsOf(env);
    this.policy = policyOf(env.LEASE_POLICY);
    this.watchers = new Watchers(
      ctx,
      this.limits.maxWatchers,
      this.policy.ttlMs,
      (at) => this.ctx.waitUntil(this.#arm(at)),
      // Old sockets contribute only their frozen import evidence; every later
      // last-seen update belongs to the committed effects path.
      () => {},
    );
    this.identityRate = new RateLimit(this.limits.requestsPerMinute);
    this.deviceRate = new RateLimit(this.limits.requestsPerMinutePerDevice);
  }

  fetch(request) {
    return respond(() => this.#serve(request));
  }

  /** Peer frames drive replication; the platform answers watch keepalives without waking this object. */
  async webSocketMessage(ws,raw) {
    if(peerSocket(ws)){try{const host=await this.#peerHost();await host.peers.frame(ws,raw);}catch{try{ws.close(1012,'reconnecting');}catch{}}return;}
    console.warn('watch: message');
  }

  /** webSocketClose completes the close handshake the client started, and notes when a device's last socket went. */
  async webSocketClose(ws, code) {
    finishClose(ws, code);
    if(peerSocket(ws))await this.#peerDeparted(ws);else await this.#noteLeft(ws);
  }

  webSocketError(ws) {
    if(peerSocket(ws))return this.#peerDeparted(ws);return this.#noteLeft(ws);
  }

  async #peerDeparted(ws){
    if(this.host?.peers)return this.host.peers.departed(ws);
    // A retained voter list is only a wake hint. Disconnection grants no
    // leadership or membership authority, including after hibernation.
    const voters=await this.ctx.storage.get('raft-router-voters');
    if(voters?.includes(ws.deserializeAttachment()?.node)){
      await this.ctx.storage.put('raft-maintenance',true);await this.ctx.storage.sync();await this.#arm(Date.now());
    }
  }

  /** #noteLeft records last_seen of a device that holds no socket now; the offline frame follows 15 s later, by alarm. */
  async #noteLeft(ws) {
    const device = this.watchers.left(ws);
    const identity = device && (await this.ctx.storage.get('identity'));
    if (identity && !(await this.#isGone()) && this.host?.ready) await this.host.operation(async()=>{await this.host.wakeMaintenance();await this.host.commitEffect({device_seen:{device}},Date.now());});
  }

  async #serve(request) {
    const url = new URL(request.url);
    if(peerPath(url.pathname)){
      try{
        if(url.search)return peerRefusal({peerCode:'invalid'});
        const socket=url.pathname!=='/v2/cluster/exchange';
        if(request.method!==(socket?'GET':'POST'))return new Response(null,{status:405});
        const host=await this.#peerHost();
        const raw=socket?fromBase64Url(request.headers.get('Codeaf-Cluster-Auth')):await readBody(request,host.peers.limits.Exchange,'too_large');
        if(!raw)return peerRefusal({peerCode:'invalid'});
        if(socket)return await host.peers.accept(request,raw,url.pathname.endsWith('/forward')?'forward':'peer');
        const result=await host.peers.httpExchange(raw);if(result.rejected)return peerRefusal({peerCode:'busy'});
        return new Response(result.raw,{headers:{'content-type':'application/json','cache-control':'no-store'}});
      }catch(error){return peerRefusal(error);}
    }
    if(['/v2/cluster/control','/v2/cluster/enroll'].includes(url.pathname)){
      const enrolling=url.pathname.endsWith('/enroll');
      if(url.search||!(enrolling?request.method==='POST':['GET','POST'].includes(request.method)))return new Response(null,{status:404});
      let who,body;
      try{const caller=await checkCert(signedHeaders(request));body=await readBody(request,clusterRequestLimit(sharedRuntime()),'too_large');who=await checkRequest(caller,{method:request.method,uri:signedUri(this.env,url)},body,Date.now());}
      catch{return new Response(enrolling?'this computer is not allowed to join this cluster':'this computer is not allowed to coordinate this cluster',{status:403});}
      await this.#refuseGone();await this.#admitted.get(ipOf(request,this.env));
      if(request.method==='GET'&&body.length)return new Response('unexpected request body',{status:400});
      const tenant=this.#tenantOf(who.identity);this.#admit(tenant,who.device);
      this.host??=new HostedIdentity(this.ctx,this.env,tenant,this.legacyAdmission,at=>this.#arm(at));
      const reply=await this.host.operation(async()=>{
        await this.host.ensure(fromBase64Url(request.headers.get('codeaf-identity')));
        await this.host.wakeMaintenance();
        return enrolling?this.host.enroll(this.host.proof(request,body)):this.host.control(request.method,who.device,request.headers.get('codeaf-cert'),this.host.proof(request,body));
      });this.watchers=this.host.watchers;reply.headers.set('cache-control','no-store');return reply;
    }
    if(url.pathname==='/v2/cluster/api'){
      if(request.method!=='POST'||url.search)throw new Wire('bad_request',400);
      const raw=await readBody(request,clusterRequestLimit(sharedRuntime()),'too_large');
      let verified;try{verified=clusterIngress(sharedRuntime(),raw,Date.now());}catch{throw new Wire("unauthorized",401);}
      const {actor,request:proof}=verified;
      const root=fromBase64Url(request.headers.get('codeaf-identity'));
      if(!root||b64(root)!==proof.identity_key)throw new Wire('unauthorized',401);
      await this.#refuseGone();
      await this.#admitted.get(ipOf(request,this.env));
      const identity=await identityOf(request.headers.get('codeaf-identity'));
      const tenant=this.#tenantOf(identity);this.#admit(tenant,actor);
      this.host??=new HostedIdentity(this.ctx,this.env,tenant,this.legacyAdmission,at=>this.#arm(at));
      const reply=await this.host.operation(()=>this.host.api(raw,proof,actor));
      this.watchers=this.host.watchers;return reply;
    }
    const route = matchRoute(request.method, url.pathname);
    const caller = await checkCert(signedHeaders(request));
    const leave = route.frames ? this.#enterPut() : () => {};
    try {
      const body = await readBody(request, route.limit, route.over);
      const who = await checkRequest(caller, { method: request.method, uri: signedUri(this.env, url) }, body, Date.now());
      await this.#refuseGone();
      await this.#admitted.get(ipOf(request, this.env));
      const tenant = this.#tenantOf(who.identity);
      this.#admit(tenant, who.device);
      this.host ??= new HostedIdentity(this.ctx,this.env,tenant,this.legacyAdmission,at=>this.#arm(at));
      const reply=await this.host.dispatch({tenant,device:who.device,body,request,env:this.env},route);
      this.watchers=this.host.watchers;return reply;
    } finally {
      leave();
    }
  }

  // Peer ingress can restore an existing namespace, but cannot provision one.
  // Check durable provenance before constructing a Tenant, which creates tables.
  async #peerHost(){
    await this.#refuseGone();
    const provision=await this.ctx.storage.get('raft-provision'),identity=await this.ctx.storage.get('identity');
    if(!provision||!identity)throw Object.assign(new Error('member authorization refused'),{peerCode:'unauthorized'});
    if(!this.host?.ready&&!this.host?.peers){
      const tenant=this.#tenantOf(identity);this.host??=new HostedIdentity(this.ctx,this.env,tenant,this.legacyAdmission,at=>this.#arm(at));
      await this.host.operation(()=>this.host.ensure(unb64(provision.config.signing_key)));this.watchers=this.host.watchers;
    }
    return this.host;
  }

  /** #enterPut counts a put from its arrival, and turns away one more than the isolate can hold in memory. */
  #enterPut() {
    if (this.flight.count >= this.limits.concurrentPuts) throw rateLimited(1);
    return this.flight.enter();
  }

  // The object serves one identity for its whole life, and the front Worker names it from the same
  // header the cert was just verified under, so the first verified identity is the only one.
  #tenantOf(identity) {
    if (!this.#tenant) this.ctx.storage.put('identity', identity); // the alarm wakes with no request to name it
    this.#tenant ??= new Tenant({
      identity,
      legacyAdmission: this.legacyAdmission,
      sql: this.ctx.storage.sql,
      bucket: this.env.FRAMES,
      clock: Date.now,
      policy: this.policy,
      limits: this.limits,
      flight: this.flight,
      arm: (at) => this.ctx.waitUntil(this.#arm(at)),
      watchers: this.watchers,
      reserve: async (owner, frame, bytes) => unwrap(await this.#global().reserve(owner, frame, bytes)),
    });
    return this.#tenant;
  }

  /**
   * alarm does the things an object is woken for: tell who went silent or is owed an offline frame, write the counts that changed since the last
   * write (stats.js), and delete the identity once its retirement is due. It always ends by arming
   * the retirement deadline again, because #arm only ever moves the alarm earlier and a stats alarm
   * may have stood in front of it.
   */
  async alarm() {
    const identity=await this.ctx.storage.get('identity');if(!identity)return;
    if(await this.#isGone())return this.#erase(identity);
    const provision=await this.ctx.storage.get('raft-provision');
    if(!provision){
      // An old source may know only the root hash. Its next authenticated
      // request supplies the public root; an alarm cannot invent that key.
      this.watchers.closeAll(1012,'reconnecting');return;
    }
    // Presence is advisory and survives hibernation in socket attachments. A
    // sweep cannot acquire authority or reopen a sleeping replication group.
    this.watchers.expire();
    const now=Date.now();
    await expirePeerSockets(this.ctx,now,at=>this.#arm(at));
    const maintenance=await this.ctx.storage.get('raft-maintenance');
    const retirement=await this.ctx.storage.get('raft-retire-at');
    const retireDue=retirement&&retirement<=now;
    if(retirement&&!retireDue)await this.#arm(retirement);
    if(!maintenance&&!retireDue)return;
    const tenant=this.#tenantOf(identity);this.host ??= new HostedIdentity(this.ctx,this.env,tenant,this.legacyAdmission,at=>this.#arm(at));
    const erase=await this.host.operation(async()=>{
      try{await this.host.ensure(Uint8Array.from(atob(provision.config.signing_key),c=>c.charCodeAt(0)));}
      catch(error){if(!this.host.initialized)throw error;await this.host.maintenance(now);return false;}
      this.watchers=this.host.watchers;tenant.stats.flush();
      if(retireDue||!this.host.retirementKnown)await this.host.wakeMaintenance();
      await this.host.maintenance(now);
      // A retained deadline is only a wake hint. Retirement still needs the
      // current committed identity before any irreversible erasure starts.
      if(retireDue||!this.host.retirementKnown){
        const root=await this.host.barrierRecord({namespace:'state',key:'identity'});
        await this.host.rememberRetirement(this.host.project('identity',root));
      }
      const at=this.host.retireAt;
      if(at && at<=now)return true;
      if(at)await this.#arm(at);return false;
    });
    // Uploads must finish their post-I/O authority check before leaving
    // Flight. Waiting for them while owning that queue would deadlock erasure.
    if(erase)return this.#erase(identity);
  }

  /**
   * #arm wakes the object at `at` unless it is already set to wake sooner. Final
   * all-voter sleep passes null before rearming independent presence deadlines.
   * Calls run one after another: two that overlapped would both read the same alarm and the later write would win.
   */
  #arm(at) {
    const run = this.#arming.then(() => this.#armSooner(at));
    this.#arming = run.catch(() => {}); // one failed setting must not stop the later ones
    return run;
  }

  async #armSooner(at) {
    if(at===null){await this.ctx.storage.deleteAlarm();return;}
    const set = await this.ctx.storage.getAlarm();
    if (set === null || at < set) await this.ctx.storage.setAlarm(at);
  }

  async #isGone() {
    this.#gone ??= Boolean(await this.ctx.storage.get('gone'));
    return this.#gone;
  }

  async #refuseGone() {
    if (await this.#isGone()) throw gone();
  }

  /**
   * #erase deletes a retired identity. The tombstone goes first, so a crash half way leaves an
   * identity that already answers "gone" and that the next alarm finishes erasing; frames then go
   * a page per turn, and only when none are left do the tables, so the tombstone is all that stays.
   */
  async #erase(identity) {
    await this.ctx.storage.put('gone', Date.now());
    this.#gone = true;
    if(!await this.flight.idle(HAS_WAIT_MS))return this.#arm(Date.now()+this.limits.statsFlushMs);
    this.#tenant = null;
    if (await erasePage(this.env.FRAMES, `${identity}/`, this.limits.sweepPageSize)) return this.#arm(Date.now());
    unwrap(await this.#global().forget(identity));
    const sourceGate=this.ctx.storage.sql.exec("SELECT name FROM sqlite_master WHERE type='table' AND name='legacy_gate'").toArray().length && this.ctx.storage.sql.exec('SELECT doc FROM legacy_gate WHERE one=1').toArray()[0];
    let erasing=await this.ctx.storage.get('raft-erasure');
    if(!erasing&&sourceGate){erasing=JSON.parse(sourceGate.doc).manifest;await this.ctx.storage.put('raft-erasure',erasing);await this.ctx.storage.sync();}
    if(erasing){
      try{for(const id of erasing.config.anchors)await this.env.RAFT_MEMBERS.get(this.env.RAFT_MEMBERS.idFromName(erasing.config.cluster+'/'+id)).eraseRetired(erasing);}
      catch(error){await this.#arm(Date.now()+this.limits.statsFlushMs);throw error;}
    }
    dropTables(this.ctx.storage.sql);
    await this.ctx.storage.delete(['identity', 'admitted','raft-provision','raft-router-sleep']);
    await this.ctx.storage.sync();this.host=null;
  }

  /**
   * #admit charges the authenticated request to the device's rate and the
   * identity's. Committed authorization follows inside the coordinator. The device goes first,
   * so one noisy device that is refused spends none of the
   * identity's share, which its other devices still need.
   */
  #admit(tenant, device) {
    const now = Date.now();
    this.deviceRate.admit(device, now);
    this.identityRate.admit('', now);
  }

  // An identity is new until its first admitted request, and admission happens before the tenant
  // exists, so a refused identity has created no table and written nothing: only an admitted one
  // is ever stored. Admitted identities are remembered, so the count never touches an existing
  // identity, whatever address it comes from.
  async #admitNewcomer(ip) {
    if (await this.ctx.storage.get('admitted')) return;
    unwrap(await this.#global().admit(ip));
    await this.ctx.storage.put('admitted', true);
  }

  // Every identity reaches the same durable budget; the namespace is already deployed.
  #global() {
    const gate = this.env.NEWCOMERS;
    return gate.get(gate.idFromName('gate'));
  }
}
