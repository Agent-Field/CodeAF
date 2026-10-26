// The pinned workerd engine is also Miniflare's runtime. Launching it directly
// avoids retaining the entire development SDK in the bounded mixed-peer proof.
import { spawn } from 'node:child_process';
import { createRequire } from 'node:module';
import { createServer } from 'node:net';
import { Agent, request } from 'node:http';
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
// Local proof RPCs own complete replies and short loopback connections.
// A small client leaves the shared memory budget for the actual member hosts.
function localRequest(url, options, agent) {
  return new Promise((resolve, reject) => {
    const req = request(url, { method: options?.method ?? 'GET', headers: options?.headers, signal: options?.signal, agent }, res => {
      const chunks = [];
      res.on('data', chunk => chunks.push(chunk));
      res.on('error', reject);
      res.on('end', () => {
        const body = Buffer.concat(chunks);
        resolve({ status: res.statusCode, ok: res.statusCode >= 200 && res.statusCode < 300,
          text: async () => body.toString(), json: async () => JSON.parse(body.toString()),
          arrayBuffer: async () => body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength) });
      });
    });
    req.on('error', reject);
    req.end(options?.body);
  });
}
export async function openWorkerd(out,persist, actors = false) {
  const deadline=Date.now()+10000;
  await mkdir(persist,{recursive:true});
  const httpPort=await port(),inspectorPort=await port();
  const agent = new Agent({ keepAlive: false, maxSockets: 1 });
  const quote=JSON.stringify;
  const config=join(persist,'proof.capnp');
  // Synchronous reactors share one isolate. Extra compilation workers retain
  // native allocator caches without supplying member or request concurrency.
  await writeFile(config,`using Workerd = import ${quote(schema)};
const config :Workerd.Config = (
 v8Flags = ["--wasm-num-compilation-tasks=1"],
 services = [
  (name = "raft-proof", worker = (
   compatibilityDate = "2026-09-01",
   modules = [(name = "worker.js", esModule = embed ${quote(join(out,'worker.js'))}),
              (name = "core.wasm", wasm = embed ${quote(join(out,'core.wasm'))})],
   ${actors ? `durableObjectNamespaces = [(className = "ActorFixture", uniqueKey = "p08-actors", enableSql = true)], durableObjectStorage = (localDisk = "storage"),` : ""}
   bindings = [${actors ? `(name = "ACTORS", durableObjectNamespace = (className = "ActorFixture"))` : `(name = "PROOF_STORE", durableObjectNamespace = (className = "ProofStore", serviceName = "proof-storage"))`}],
   globalOutbound = "internet"
  )),
  ${actors ? "" : `(name = "proof-storage", worker = (
   compatibilityDate = "2026-09-01",
   modules = [(name = "storage.js", esModule = embed ${quote(join(out,'storage-worker.js'))})],
   durableObjectNamespaces = [(className = "ProofStore", uniqueKey = "p03-local-proof", enableSql = true)],
   durableObjectStorage = (localDisk = "storage"), globalOutbound = "internet"
  )),`}
  (name = "storage", disk = (path = ${quote(persist)}, writable = true)),
  (name = "internet", network = (allow = []))
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
    dispatchFetch:(url,options)=>{const u=new URL(url);return localRequest(origin+u.pathname+u.search,options,agent);},
    getInspectorURL:async()=>new URL(`http://127.0.0.1:${inspectorPort}`),
    getInspectorTargets:async()=> (await localRequest(`http://127.0.0.1:${inspectorPort}/json/list`, {}, agent)).json(),
    dispose:async()=>{agent.destroy();if(!exited)child.kill('SIGKILL');await ended;},
  };
  try {
    for(;;){
      if(exited)throw new Error(`local workerd exited: ${errors}`);
      try{const r=await localRequest(origin+'/ready',{signal:AbortSignal.timeout(500)},agent);if(r.status===204)break;}catch{}
      if(Date.now()>deadline)throw new Error(`local workerd startup timed out: ${errors}`);
      await delay(25);
    }
    return server;
  }catch(err){await server.dispose();throw err;}
}
