// The pinned workerd engine is also Miniflare's runtime. Launching it directly
// avoids retaining the entire development SDK in the bounded mixed-peer proof.
import { spawn } from 'node:child_process';
import { createRequire } from 'node:module';
import { createServer } from 'node:net';
import { mkdir, writeFile } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
const require = createRequire(import.meta.url);
const binary = require('workerd').default;
const schema = join(dirname(require.resolve('workerd/package.json')), 'workerd.capnp');
async function port() {
  const s=createServer();await new Promise((resolve,reject)=>{s.on('error',reject);s.listen(0,'127.0.0.1',resolve);});
  const n=s.address().port;await new Promise(resolve=>s.close(resolve));return n;
}
export async function openWorkerd(out,persist, actors = false) {
  await mkdir(persist,{recursive:true});
  const httpPort=await port(),inspectorPort=await port();
  const quote=JSON.stringify;
  const config=join(persist,'proof.capnp');
  await writeFile(config,`using Workerd = import ${quote(schema)};
const config :Workerd.Config = (
 services = [
  (name = "raft-proof", worker = (
   compatibilityDate = "2026-09-01",
   modules = [(name = "worker.js", esModule = embed ${quote(join(out,'worker.js'))}),
              (name = "core.wasm", wasm = embed ${quote(join(out,'core.wasm'))})],
   ${actors ? `durableObjectNamespaces = [(className = "ActorFixture", uniqueKey = "p08-actors", enableSql = true)], durableObjectStorage = (localDisk = "storage"),` : ""}
   bindings = [${actors ? `(name = "ACTORS", durableObjectNamespace = (className = "ActorFixture")),` : ""}(name = "PROOF_STORE", durableObjectNamespace = (className = "ProofStore", serviceName = "proof-storage"))],
   globalOutbound = "blocked"
  )),
  (name = "proof-storage", worker = (
   compatibilityDate = "2026-09-01",
   modules = [(name = "storage.js", esModule = embed ${quote(join(out,'storage-worker.js'))})],
   durableObjectNamespaces = [(className = "ProofStore", uniqueKey = "p03-local-proof", enableSql = true)],
   durableObjectStorage = (localDisk = "storage"), globalOutbound = "blocked"
  )),
  (name = "storage", disk = (path = ${quote(persist)}, writable = true)),
  (name = "blocked", worker = (compatibilityDate = "2026-09-01", modules = [
   (name = "blocked.js", esModule = "export default {fetch() {throw new Error('proof forbids outbound network')}}")
  ], globalOutbound = "blocked"))
 ],
 sockets = [(name = "http", address = "127.0.0.1:${httpPort}", http = (), service = "raft-proof")]
);
`);
  const child=spawn(binary,['serve','--experimental','-I/','--inspector-addr',`127.0.0.1:${inspectorPort}`,config],{stdio:['ignore','pipe','pipe']});
  let errors='',exited=false;
  child.stdout.on('data',()=>{});child.stderr.on('data',b=>{if(errors.length<4096)errors+=b.toString();});
  const ended=new Promise(resolve=>child.once('exit',code=>{exited=true;resolve(code);}));
  const origin=`http://127.0.0.1:${httpPort}`;
  const server={
    origin,
    dispatchFetch:(url,options)=>{const u=new URL(url);return fetch(origin+u.pathname+u.search,options);},
    getInspectorURL:async()=>new URL(`http://127.0.0.1:${inspectorPort}`),
    dispose:async()=>{if(!exited)child.kill('SIGKILL');await ended;},
  };
  try {
    const deadline=Date.now()+10000;
    for(;;){
      if(exited)throw new Error(`local workerd exited: ${errors}`);
      try{const r=await fetch(origin+'/ready',{signal:AbortSignal.timeout(500)});if(r.status===204)break;}catch{}
      if(Date.now()>deadline)throw new Error(`local workerd startup timed out: ${errors}`);
      await delay(25);
    }
    return server;
  }catch(err){await server.dispose();throw err;}
}
