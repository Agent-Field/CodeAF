// Only this local fixture supplies genesis public authorities and driving RPCs.
// All membership evaluation, planning, storage and peer admission are production.
import core from 'core.wasm';
import { RaftMemberDO } from '../../src/raft/member.js';
import { installRaftModule, sharedRuntime } from '../../src/raft/actor.js';
import { directoryEvaluator } from '../../src/raft/directory.js';
import { committedRegistry } from '../../src/raft/membership.js';
import { b64, unb64 } from '../../src/raft/storage.js';
export class ProofStore {}
installRaftModule(core);
const reply = v => new Response(JSON.stringify(v ?? null, (_,x) => typeof x === 'bigint' ? String(x) : x instanceof Uint8Array ? b64(x) : x));
export class ActorFixture extends RaftMemberDO {
 evaluator(config) {
  const evaluate = directoryEvaluator(sharedRuntime(),config.cluster,8*1024*1024);
  return (raw,read,app) => {
   const base = read('state','identity') ? [] : config.genesis.map(r => ({...r,data:unb64(r.data)}));
   const size = base.reduce((n,r) => n+12+r.namespace.length+r.key.length+r.data.length,0);
   const r = evaluate(raw,(ns,k) => base.find(r => r.namespace===ns && r.key===k)?.data ?? read(ns,k),{...app,size:app.size+size});
   return {...r,writes:[...base,...r.writes]};
  };
 }
 async fetch(request) {
  const b = await request.json();
  try {
   if (b.op==='init') return reply(await this.initialize(b.config));
   await this.load();
   if (b.op==='input') return reply(await this.actor.input(b.tag,[2,3,8].includes(b.tag)?unb64(b.value):b.value));
   if (b.op==='peer') return reply(await this.peer(unb64(b.value)));
   if (b.op==='membership') return reply(await this.membership());
   if (b.op==='checkpoint') return reply(await this.actor.checkpoint());
   if (b.op==='registry') return reply(await this.refreshEnrollment());
   if (b.op==='header') return reply(JSON.parse(new TextDecoder().decode(this.actor.store.read('membership','registry'))));
   if (b.op==='row') return reply(this.actor.store.read(b.namespace,b.key));
   if (b.op==='status') return reply(await this.actor.input(0));
   if (b.op==='enrollment') return reply(await this.enrollment(b.view,b.applied));
   throw Error('fixture operation');
  } catch (e) { return reply({error:e.message}); }
 }
}
export default { async fetch(request,env) {
 const u = new URL(request.url); if (u.pathname==='/ready') return new Response(null,{status:204});
 const name = u.pathname.slice(1); return env.ACTORS.get(env.ACTORS.idFromName(name)).fetch(request);
} };
