import {Flight} from '../flight.js';
import {frame,response} from './abi.js';
import {encodeCanonical} from './codec.js';
import {unb64} from './storage.js';

const enc=new TextEncoder(),dec=new TextDecoder('utf-8',{fatal:true});
const unavailable=()=>new Error('personal member unavailable');

// Public waits retain only their operation queue. Replies resolve bounded
// tokens independently, allowing the peer stream to finish durable replication.
export class PersonalForward {
 constructor(host){this.host=host;this.pending=new Map();}
 socket(node){return this.host.peers.sockets().find(ws=>{const a=ws.deserializeAttachment();return a.kind==='forward'&&a.node===node&&a.generation===this.host.peers.generation;});}
 async begin(node,work){
  const host=this.host,ws=this.socket(node);
  if(!ws||this.pending.size>=host.peers.limits.Inflight)throw unavailable();
  host.peers.validate(unb64(ws.deserializeAttachment().auth));
  const id=work.id??crypto.randomUUID();
  if(this.pending.has(id))throw unavailable();
  const value={command:null,revision:'0',barrier:'0',schedule:null,now:0,...work,scope:host.peers.scope,generation:host.peers.generation,from:host.manifest.config.anchors[0],to:node,id};
  const r=response(host.runtime.call(frame(39).u32(0).blob(enc.encode(encodeCanonical(value))).blob(unb64(host.provisioning.anchors[0]).slice(-32)).finish())),raw=r.blob();r.end();
  const flight=new Flight(),pending={ws,node,release:flight.enter(),reply:null};
  const promise=flight.idle(host.peers.limits.ForwardTimeout).then(completed=>{if(!completed)this.finish(id,null);return pending.reply;});
  this.pending.set(id,pending);
  try{ws.send(raw);}catch{this.finish(id,null);}
  return {id,promise};
 }
 finish(id,reply){const p=this.pending.get(id);if(!p)return;this.pending.delete(id);p.reply=reply;p.release();this.host.signal();}
 frame(ws,raw){
  const host=this.host,a=ws.deserializeAttachment();
  host.peers.validate(unb64(a.auth));
  if(!(raw instanceof ArrayBuffer))throw unavailable();
  const peer=host.peers.view[a.node],r=response(host.runtime.call(frame(39).u32(1).blob(new Uint8Array(raw)).blob(unb64(peer.key)).finish()));
  if(!r.u32())throw unavailable();
  const f=JSON.parse(dec.decode(r.blob()));r.end();
  const wire=JSON.parse(dec.decode(new Uint8Array(raw)));
  if(wire.scope.identity!==host.peers.scope.identity||wire.scope.cluster!==host.peers.scope.cluster||wire.scope.epoch!==host.peers.scope.epoch||wire.scope.protocol!==host.peers.scope.protocol||wire.generation!==host.peers.generation||wire.from!==a.node||wire.to!==host.manifest.config.anchors[0])throw unavailable();
  const p=this.pending.get(f.id);if(p?.ws===ws&&p.node===wire.from)this.finish(f.id,f);
 }
 departed(ws){for(const [id,p]of this.pending)if(p.ws===ws)this.finish(id,null);}
}
