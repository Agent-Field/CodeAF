import relay from '../../src/raft-worker.js';
export * from '../../src/raft-worker.js';
import { nameFor } from '../../src/engine/names.js';
import { PROTOCOL, HEADER_NAME, HEADER_KEY, OP, frame as carrierFrame, readFrame, registrationProof } from '../../src/engine/carrier.js';
const check=(ok,word)=>{if(!ok)throw Error(word)};
const equal=(a,b)=>check(a===b,`expected ${b}, received ${a}`);
const same=(a,b)=>equal(JSON.stringify([...a]),JSON.stringify([...b]));
async function live(env,ctx){
 let engine,surface;
 const receive=ws=>{const queue=[],waiting=[];ws.binaryType='arraybuffer';ws.addEventListener('message',async event=>{const raw=typeof event.data.arrayBuffer==='function'?await event.data.arrayBuffer():event.data;const value=new Uint8Array(raw);if(waiting.length)waiting.shift()(value);else queue.push(value)});ws.accept();return ()=>queue.length?Promise.resolve(queue.shift()):new Promise(resolve=>waiting.push(resolve))};
 try {
  const pair=await crypto.subtle.generateKey('X25519',true,['deriveBits']);
  const key=new Uint8Array(await crypto.subtle.exportKey('raw',pair.publicKey)),name=await nameFor(key);
  const upgrade={upgrade:'websocket','sec-websocket-protocol':PROTOCOL};
  const registered=await call('http://local/v1/engine',{headers:{...upgrade,[HEADER_NAME]:name,[HEADER_KEY]:btoa(String.fromCharCode(...key))}});
  equal(registered.status,101);engine=registered.webSocket;const fromEngine=receive(engine);
  const challenge=await fromEngine();equal(challenge.length,64);
  const ephemeral=await crypto.subtle.importKey('raw',challenge.subarray(0,32),'X25519',false,[]);
  const shared=new Uint8Array(await crypto.subtle.deriveBits({name:'X25519',public:ephemeral},pair.privateKey,256));
  engine.send(await registrationProof(shared,challenge.subarray(32),name,key));same([...await fromEngine()],[1,0]);
  const dial=await call('http://local/v1/dial/'+name,{headers:upgrade});equal(dial.status,101);surface=dial.webSocket;const fromSurface=receive(surface);
  const opened=readFrame(await fromEngine());equal(opened.op,OP.open);
  const payload=Uint8Array.from([0,255,19,47]);surface.send(payload);
  const sent=readFrame(await fromEngine());equal(sent.op,OP.data);same(sent.payload,payload);
  engine.send(carrierFrame(OP.data,opened.stream,Uint8Array.from([91,0,255])));same(await fromSurface(),Uint8Array.from([91,0,255]));

 return Response.json({registered:true,surface_to_engine:[...sent.payload],engine_to_surface:[91,0,255]});
 }finally{try{surface?.close()}catch{}try{engine?.close()}catch{}}
 function call(url,options){return relay.fetch(new Request(url,options),env,ctx)}
}
export default {fetch(request,env,ctx){return new URL(request.url).pathname==='/fixture/storage-live'?live(env,ctx):relay.fetch(request,env,ctx)}};
