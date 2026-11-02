import { frame, response, Reader } from './abi.js';
import { unb64 } from './storage.js';

// JavaScript supplies immutable dependency bytes and persists the Go write-set.
// It never interprets leases, clocks, revocations, CAS or request sequences.
function applicationPages(runtime,initial,read){
  let r=initial;const id=r.u32(),chunks=[];let size=0;
  try{
    for(;;){
      const kind=r.u32();
      if(kind===0){
        const namespace=r.str(),key=r.str();r.end();
        if(!read)throw new Error('raft application: unexpected record request');
        const data=(read.page??read)(namespace,key);
        r=response(runtime.call(frame(42).u32(id).u32(data==null?0:1).blob(data??new Uint8Array()).finish()));
      }else{
        if(kind!==1)throw new Error('raft application: invalid result page');
        const more=r.u32(),chunk=r.blob();r.end();chunks.push(chunk);size+=chunk.length;
        if(!more)break;
        r=response(runtime.call(frame(43).u32(id).finish()));
      }
    }
    const joined=new Uint8Array(size);let offset=0;
    for(const chunk of chunks){joined.set(chunk,offset);offset+=chunk.length;}
    return new Reader(joined);
  }finally{const dropped=response(runtime.call(frame(44).u32(id).finish()));dropped.end();}
}

export function directoryEvaluator(runtime,cluster,ceiling){
  return (command,read,app)=>{
    const start=response(runtime.call(frame(41).blob(command).str(cluster).u64(app.now).u64(app.version).u64(app.size).u64(ceiling).u64(app.index??0).u64(app.term??0).blob(unb64(app.conf??'')).finish()));
    const r=applicationPages(runtime,start,read),now=r.u64(),version=r.u64(),result=r.blob();
    const writes=Array.from({length:r.u32()},()=>({namespace:r.str(),key:r.str(),delete:r.u32()!==0,data:r.blob()}));r.end();
    return {now,version,result,writes};
  };
}

// Typed projection streams large logical JSON without widening an ABI buffer.
export function projectRecord(runtime,kind,raw){
  const start=response(runtime.call(frame(45).str(kind).blob(raw).finish()));
  const r=applicationPages(runtime,start),result=new TextDecoder('utf-8',{fatal:true}).decode(r.blob());r.end();return result;
}

// The consensus host implements commit and barrier, not a local-success cache.
// Calls return only committed/durably applied bytes; a leader hint is routing.
export class RaftDirectoryFacade {
  constructor(host) { this.host = host; }
  async command(originalSignedBytes) { return this.host.commit(originalSignedBytes); }
  async read(authenticatedRequest) { return this.host.barrierRead(authenticatedRequest); }
}

// The status facade cannot change voters. Its host authorizes a fresh quorum
// read and drives the committed intent, target election and completion barrier.
export class RaftLeadershipFacade {
  constructor(host) { this.host = host; }
  async status(authenticatedRequest) { return this.host.clusterStatus(authenticatedRequest); }
  async lead(originalSignedBytes) { return this.host.leadership(originalSignedBytes); }
}
