import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { mkdtemp,writeFile,readFile,rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

// Real local Durable Object persistence is the delivery boundary; killing its
// process retains only the on-disk object storage, never a test-side cache.
test('R3 R16 R17 a real LinkRequest durably reserves and replays its committed delivery after restart',{timeout:30000},async()=>{
  const out=process.env.CODEAF_RAFT_PROOF_DIR??'/tmp/codeaf-p10',dir=await mkdtemp(join(tmpdir(),'p10-link-'));
  const [binary,schema]=await Promise.all(['workerd-path','workerd-schema-path'].map(p=>readFile(join(out,p),'utf8')));
  const bundle=await build({entryPoints:['test/raft/link-effects-worker.js'],bundle:true,format:'esm',target:'es2022',external:['cloudflare:*'],write:false});
  await writeFile(join(dir,'worker.js'),bundle.outputFiles[0].contents);
  const server=createServer();await new Promise(r=>server.listen(0,'127.0.0.1',r));const port=server.address().port;await new Promise(r=>server.close(r));
  const q=JSON.stringify,config=join(dir,'config.capnp');
  await writeFile(config,`using Workerd=import ${q(schema.trim())}; const config:Workerd.Config=(services=[(name="proof",worker=(compatibilityDate="2026-09-01",modules=[(name="worker.js",esModule=embed ${q(join(dir,'worker.js'))})],durableObjectNamespaces=[(className="LinkRequest",uniqueKey="p10-link")],durableObjectStorage=(localDisk="storage"),bindings=[(name="LINKS",durableObjectNamespace=(className="LinkRequest"))],globalOutbound="blocked")),(name="storage",disk=(path=${q(dir)},writable=true)),(name="blocked",worker=(compatibilityDate="2026-09-01",modules=[(name="blocked.js",esModule="export default {fetch(){throw Error('outbound forbidden')}}")],globalOutbound="blocked"))],sockets=[(name="http",address="127.0.0.1:${port}",http=(),service="proof")]);`);
  let child,ended,stderr='';const origin=`http://127.0.0.1:${port}`;
  const start=async()=>{child=spawn(binary.trim(),['serve','-I/',config],{stdio:['ignore','ignore','pipe']});child.stderr.on('data',b=>{stderr+=b.toString();});ended=new Promise(r=>child.once('exit',r));for(let n=0;n<100;n++){if(child.exitCode!==null)throw Error(stderr);try{if((await fetch(origin+'/ready',{signal:AbortSignal.timeout(200)})).status===204)return;}catch{}await delay(25);}throw Error('local startup timed out: '+stderr);};
  const stop=async()=>{if(child?.exitCode===null){child.kill('SIGKILL');await ended;}};
  const call=async(code,op,...args)=>{const r=await fetch(origin,{method:'POST',body:JSON.stringify({code,op,args}),signal:AbortSignal.timeout(5000)});assert.equal(r.status,200);return r.json();};
  try {
    await start();const code='k7m2q9xd';
    const pending={code,device:'opaque-device',expires_at:Date.now()+60000};
    assert('ok' in await call(code,'open',pending));assert('ok' in await call(code,'bind','one'));
    assert.equal((await call(code,'bind','two')).err,'already_decided');
    assert.equal((await call(code,'decide','approved','c2VhbGVk','opaque-device')).err,'already_decided');
    assert.equal((await call(code,'read',0)).ok.state,'pending');
    await stop();await start();assert('ok' in await call(code,'bind','one'));
    assert('ok' in await call(code,'deliver','one','decision','approved','c2VhbGVk','opaque-device'));
    await stop();await start();assert('ok' in await call(code,'deliver','one','decision','approved','c2VhbGVk','opaque-device'));
    assert.equal((await call(code,'deliver','one','decision','approved','b3RoZXI','opaque-device')).err,'already_decided');
    const record=(await call(code,'read',0)).ok;assert.equal(record.state,'approved');assert.equal(record.grant,'c2VhbGVk');assert.equal(record.cluster,undefined);assert.equal(record.delivery,undefined);
  } finally {await stop();await rm(dir,{recursive:true,force:true});}
});
